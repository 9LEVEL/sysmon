package banco

// O diario: desde quando cada alerta existe, o que mudou nele, e quando cada
// host reiniciou.
//
// E o que responde "isso e antigo ou e novo?". Um "89 de 267 desligamentos
// sujos" que ja estava la quando o sysmon comecou a olhar e um fato do
// passado do disco; um 90 que apareceu esta madrugada, logo depois de um
// reinicio, e uma queda de energia que acabou de acontecer.
//
// So grava o que MUDA. Uma leitura a cada 5 segundos gravada inteira seria
// escrever no SSD sem parar para guardar, quase sempre, a mesma coisa - e o
// banco viraria a serie temporal que o agente ja mantem melhor.

import (
	"database/sql"
	"math"
	"strings"

	"sysmon/internal/nucleo"
)

const (
	// Carencia antes de dar um alerta por encerrado. Temperatura e RAM
	// oscilam em torno do limiar; sem isto, cada oscilacao viraria um
	// episodio novo, e "desde quando" passaria a significar "desde a ultima
	// oscilacao". O fechamento e datado de quando ele sumiu, nao de quando a
	// carencia acabou.
	carenciaFechar = 120.0

	// Um boot que se moveu menos que isto e o mesmo boot: o uptime e o
	// relogio do agente nao andam exatamente juntos, e o ajuste de NTP mexe
	// no relogio. Reinicio de verdade move o boot em minutos, no minimo.
	toleranciaBoot = 120.0

	// O visto_em de um host e regravado no maximo a este intervalo. Ele so
	// serve para dizer quando o host foi visto vivo antes de um reinicio, e
	// para isso um minuto de precisao basta.
	intervaloVisto = 60.0
)

// Registrar anota uma rodada de leituras.
//
// agora e o relogio do cliente; os horarios de boot e de "visto" saem do
// relogio do proprio host (ts e uptime do agente), para a janela de queda
// ficar inteira num relogio so.
//
// Os alertas sao os BRUTOS - com os aceitos incluidos: aceitar um alerta nao
// o faz deixar de existir, e o episodio dele nao pode fechar por isso.
func (b *Banco) Registrar(agora float64, leituras []nucleo.LeituraHost,
	brutos []nucleo.Alerta) error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	porHost := map[string][]nucleo.Alerta{}
	for _, a := range brutos {
		porHost[a.Host] = append(porHost[a.Host], a)
	}

	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, l := range leituras {
		nome := l.Host.Nome
		novo, err := b.anotarHost(tx, nome, l.Estado)
		if err != nil {
			return err
		}
		if err := b.anotarAlertas(tx, agora, nome, l.Estado.Dados != nil,
			novo, porHost[nome]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// anotarHost atualiza o ultimo estado do host e registra reinicio. Devolve
// true na primeira leitura com dados que este banco ve desse host.
func (b *Banco) anotarHost(tx *sql.Tx, host string, e nucleo.Estado) (bool, error) {
	d := e.Dados
	if d == nil {
		// Fora do ar. Se o banco nunca viu este host com dados, o que ele
		// mostra agora - "conexao recusada" - tambem e a primeira leitura:
		// chamar de novo seria dizer que caiu agora, quando pode estar fora
		// ha dias.
		if _, conhecido := b.bootEm[host]; conhecido {
			return false, nil
		}
		var n int
		err := tx.QueryRow(`SELECT count(*) FROM host_visto WHERE host = ?`,
			host).Scan(&n)
		return n == 0, err
	}
	if d.TS <= 0 {
		return false, nil
	}
	boot := d.TS - float64(d.UptimeS)

	antBoot, conhecido := b.bootEm[host]
	antVisto := b.vistoEm[host]
	if !conhecido {
		err := tx.QueryRow(`SELECT boot_em, visto_em FROM host_visto WHERE host = ?`,
			host).Scan(&antBoot, &antVisto)
		switch {
		case err == sql.ErrNoRows:
			_, err := tx.Exec(`INSERT INTO host_visto (host, boot_em, visto_em)
				VALUES (?, ?, ?)`, host, boot, d.TS)
			if err != nil {
				return false, err
			}
			b.bootEm[host], b.vistoEm[host] = boot, d.TS
			return true, nil
		case err != nil:
			return false, err
		}
		b.bootEm[host], b.vistoEm[host] = antBoot, antVisto
	}

	if math.Abs(boot-antBoot) > toleranciaBoot {
		_, err := tx.Exec(`INSERT OR IGNORE INTO reinicio
			(host, boot_em, visto_antes, detectado_em) VALUES (?, ?, ?, ?)`,
			host, boot, antVisto, d.TS)
		if err != nil {
			return false, err
		}
		if _, err := tx.Exec(`UPDATE host_visto SET boot_em = ?, visto_em = ?
			WHERE host = ?`, boot, d.TS, host); err != nil {
			return false, err
		}
		b.bootEm[host], b.vistoEm[host] = boot, d.TS
		return false, nil
	}
	if d.TS-antVisto >= intervaloVisto {
		if _, err := tx.Exec(`UPDATE host_visto SET visto_em = ? WHERE host = ?`,
			d.TS, host); err != nil {
			return false, err
		}
		b.vistoEm[host] = d.TS
	}
	return false, nil
}

type aberto struct {
	id    int64
	valor string
}

func (b *Banco) anotarAlertas(tx *sql.Tx, agora float64, host string,
	comDados, herdado bool, alertas []nucleo.Alerta) error {
	abertos := map[string]aberto{}
	rows, err := tx.Query(`SELECT id, chave, valor_atual FROM alerta
		WHERE host = ? AND fechado_em IS NULL`, host)
	if err != nil {
		return err
	}
	for rows.Next() {
		var a aberto
		var chave string
		if err := rows.Scan(&a.id, &chave, &a.valor); err != nil {
			rows.Close()
			return err
		}
		abertos[chave] = a
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	presentes := map[string]bool{}
	for _, a := range alertas {
		presentes[a.Chave] = true
		delete(b.ausentes, host+"/"+a.Chave)
		ab, ok := abertos[a.Chave]
		if !ok {
			if _, err := tx.Exec(`INSERT INTO alerta (host, chave, aberto_em,
				herdado, nivel, valor_abertura, texto_abertura, valor_atual,
				texto_atual) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				host, a.Chave, agora, herdado, a.Nivel, a.Valor, a.Texto,
				a.Valor, a.Texto); err != nil {
				return err
			}
			continue
		}
		// So o VALOR conta como mudanca. O texto de alertas sem valor - "CPU
		// em 82C" - muda a cada leitura, e grava-lo seria a serie temporal
		// que este diario existe para nao ser.
		if a.Valor == "" || a.Valor == ab.valor {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO alerta_valor (alerta_id, em, de, para, texto)
			VALUES (?, ?, ?, ?, ?)`, ab.id, agora, ab.valor, a.Valor, a.Texto); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE alerta SET valor_atual = ?, texto_atual = ?,
			nivel = ? WHERE id = ?`, a.Valor, a.Texto, a.Nivel, ab.id); err != nil {
			return err
		}
	}

	// Host sem dados nao diz nada sobre os alertas dele: o disco nao sarou
	// porque a rede caiu. Fechar agora faria o "89 desligamentos" voltar como
	// novo a cada vez que o host ficasse fora do ar.
	if !comDados {
		return nil
	}
	for chave, ab := range abertos {
		if presentes[chave] {
			continue
		}
		k := host + "/" + chave
		desde, ok := b.ausentes[k]
		if !ok {
			b.ausentes[k] = agora
			continue
		}
		if agora-desde < carenciaFechar {
			continue
		}
		if _, err := tx.Exec(`UPDATE alerta SET fechado_em = ? WHERE id = ?`,
			desde, ab.id); err != nil {
			return err
		}
		delete(b.ausentes, k)
	}
	return nil
}

// ------------------------------------------------------------- consultas

// Episodio e um alerta aberto, com o que o diario sabe dele.
type Episodio struct {
	Host, Chave   string
	AbertoEm      float64
	Herdado       bool // ja estava assim na primeira leitura do host
	ValorAbertura string
	Mudancas      int
	UltimaMudanca *Mudanca
}

// Mudanca e uma troca de valor dentro de um episodio.
type Mudanca struct {
	Em       float64
	De, Para string
}

// Abertos devolve os episodios em aberto, indexados como os aceites: pela
// Alerta.ChaveAceite, "host/chave".
func (b *Banco) Abertos() (map[string]Episodio, error) {
	if b == nil {
		return nil, nil
	}
	rows, err := b.db.Query(`
		SELECT a.host, a.chave, a.aberto_em, a.herdado, a.valor_abertura,
		       (SELECT count(*) FROM alerta_valor v WHERE v.alerta_id = a.id),
		       u.em, u.de, u.para
		FROM alerta a
		LEFT JOIN alerta_valor u ON u.rowid = (
			SELECT v.rowid FROM alerta_valor v WHERE v.alerta_id = a.id
			ORDER BY v.em DESC, v.rowid DESC LIMIT 1)
		WHERE a.fechado_em IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Episodio{}
	for rows.Next() {
		var e Episodio
		var em sql.NullFloat64
		var de, para sql.NullString
		if err := rows.Scan(&e.Host, &e.Chave, &e.AbertoEm, &e.Herdado,
			&e.ValorAbertura, &e.Mudancas, &em, &de, &para); err != nil {
			return nil, err
		}
		if em.Valid {
			e.UltimaMudanca = &Mudanca{Em: em.Float64, De: de.String, Para: para.String}
		}
		out[e.Host+"/"+e.Chave] = e
	}
	return out, rows.Err()
}

// Reinicio e um boot detectado.
type Reinicio struct {
	BootEm     float64
	VistoAntes float64 // 0 quando nao havia leitura anterior
}

// UltimosReinicios devolve o boot mais recente detectado de cada host.
func (b *Banco) UltimosReinicios() (map[string]Reinicio, error) {
	if b == nil {
		return nil, nil
	}
	rows, err := b.db.Query(`SELECT host, boot_em, coalesce(visto_antes, 0)
		FROM reinicio r WHERE boot_em = (
			SELECT max(boot_em) FROM reinicio WHERE host = r.host)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Reinicio{}
	for rows.Next() {
		var host string
		var r Reinicio
		if err := rows.Scan(&host, &r.BootEm, &r.VistoAntes); err != nil {
			return nil, err
		}
		out[host] = r
	}
	return out, rows.Err()
}

// EhDeEnergia diz se a chave e de um alerta que uma queda de energia muda -
// os desligamentos sujos. E nesses que o ultimo reinicio do host vale ser
// mostrado ao lado: e ele que diz quando a queda aconteceu.
func EhDeEnergia(chave string) bool {
	return strings.Contains(chave, "desligamento_sujo")
}
