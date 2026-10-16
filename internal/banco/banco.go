// Package banco e a memoria do cliente: o que o usuario decidiu e o que ele
// viu acontecer, guardado de um jeito que sobrevive a queda de energia.
//
// Ate a v5.7 os aceites moravam no config.json, gravado com troca atomica de
// arquivo - mas sem fsync. Numa queda de energia logo depois de salvar, o
// sistema de arquivos pode acordar com o arquivo vazio ou na versao anterior:
// some o aceite e, junto, os tokens de todos os hosts. E a data do aceite
// era a unica data que existia; quando um alerta tinha comecado, ninguem
// guardava.
//
// O SQLite em modo WAL com synchronous=FULL resolve as duas coisas: cada
// transacao confirmada esta no disco antes de a chamada voltar, e o banco
// tem espaco para o que falta - desde quando cada alerta existe, o que mudou
// nele, e quando cada host reiniciou.
//
// Mora so no cliente, que e quem consome as metricas. O agente continua sem
// estado alem do historico SMART.
//
// E um driver em Go puro (modernc.org/sqlite): o cliente do Windows e
// compilado cruzado com CGO desligado, e um SQLite em C quebraria isso.
package banco

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	_ "modernc.org/sqlite" // registra o driver "sqlite"

	"sysmon/internal/nucleo"
)

// NomeArquivo fica ao lado do config.json, como o sysmon-janela.json: tudo o
// que o programa guarda numa pasta so, que e a pasta que o usuario conhece.
const NomeArquivo = "sysmon.db"

// CaminhoPadrao devolve o banco que acompanha um config.
func CaminhoPadrao(config string) string {
	return filepath.Join(filepath.Dir(config), NomeArquivo)
}

// Banco e a conexao com o arquivo. Seguro para uso concorrente.
type Banco struct {
	db *sql.DB

	// Estado em memoria do diario - ver diario.go. Fica aqui, e nao em
	// variaveis do pacote, porque os testes abrem varios bancos.
	mu       sync.Mutex
	vistoEm  map[string]float64 // ultimo visto_em gravado, por host
	bootEm   map[string]float64 // ultimo boot conhecido, por host
	ausentes map[string]float64 // episodio aberto que sumiu da leitura: desde quando
}

// Abrir abre (ou cria) o banco e aplica as migracoes pendentes.
func Abrir(caminho string) (*Banco, error) {
	if err := os.MkdirAll(filepath.Dir(caminho), 0o700); err != nil {
		return nil, err
	}
	// WAL: leitura nao bloqueia escrita, e a escrita e um append - o modo
	// mais tolerante a corte de energia que o SQLite tem.
	// synchronous=FULL: o commit so volta depois do fsync do WAL. O NORMAL,
	// padrao em WAL, pode perder a ultima transacao numa queda, que e
	// justamente o aceite que o usuario acabou de clicar.
	q := url.Values{}
	for _, p := range []string{"journal_mode(WAL)", "synchronous(FULL)",
		"busy_timeout(5000)", "foreign_keys(ON)"} {
		q.Add("_pragma", p)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(caminho)+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	// Uma conexao so: o cliente e um processo, as escritas sao raras, e com
	// uma conexao o SQLite nunca precisa disputar a trava consigo mesmo.
	db.SetMaxOpenConns(1)
	b := &Banco{db: db, vistoEm: map[string]float64{},
		bootEm: map[string]float64{}, ausentes: map[string]float64{}}
	if err := b.migrar(); err != nil {
		db.Close()
		return nil, fmt.Errorf("%s: %w", filepath.Base(caminho), err)
	}
	if runtime.GOOS != "windows" {
		// O arquivo pode guardar os textos dos alertas, que citam discos e
		// caminhos; nao ha por que outro usuario da maquina le-lo.
		_ = os.Chmod(caminho, 0o600)
	}
	return b, nil
}

// Fechar encerra a conexao. Um Banco nil fecha sem erro.
func (b *Banco) Fechar() error {
	if b == nil {
		return nil
	}
	return b.db.Close()
}

// migracoes sao aplicadas em ordem, uma vez cada. O numero da ultima aplicada
// fica no user_version do proprio arquivo, que e o lugar que o SQLite
// reserva para isso.
//
// Nunca se edita uma migracao que ja saiu num release: o banco de quem ja a
// aplicou nao roda de novo, e os dois arquivos passariam a divergir. Mudou o
// esquema, e uma migracao nova no fim da lista.
var migracoes = []string{
	// 1: aceites, diario de alertas e reinicios.
	`
	-- O que esta aceito agora. Uma linha por alerta de host.
	CREATE TABLE aceite (
		chave     TEXT PRIMARY KEY,  -- Alerta.ChaveAceite: "pve/smart:sda:..."
		valor     TEXT NOT NULL,     -- o valor aceito; outro valor volta a alertar
		texto     TEXT NOT NULL DEFAULT '',
		aceito_em REAL NOT NULL      -- unix, segundos
	);

	-- Tudo o que ja foi feito com os aceites. Nao e apagado: "quem aceitou
	-- isso, e quando?" e pergunta que aparece meses depois.
	CREATE TABLE aceite_historico (
		id    INTEGER PRIMARY KEY,
		chave TEXT NOT NULL,
		acao  TEXT NOT NULL CHECK (acao IN ('aceito', 'revogado', 'importado')),
		valor TEXT NOT NULL DEFAULT '',
		texto TEXT NOT NULL DEFAULT '',
		em    REAL NOT NULL
	);
	CREATE INDEX aceite_historico_chave ON aceite_historico (chave, em);

	-- Um episodio de alerta: de quando apareceu ate quando sumiu.
	CREATE TABLE alerta (
		id             INTEGER PRIMARY KEY,
		host           TEXT NOT NULL,
		chave          TEXT NOT NULL,
		aberto_em      REAL NOT NULL,
		fechado_em     REAL,             -- NULL enquanto alerta
		herdado        INTEGER NOT NULL DEFAULT 0, -- ja estava assim na 1a leitura do host
		nivel          INTEGER NOT NULL,
		valor_abertura TEXT NOT NULL DEFAULT '',
		texto_abertura TEXT NOT NULL DEFAULT '',
		valor_atual    TEXT NOT NULL DEFAULT '',
		texto_atual    TEXT NOT NULL DEFAULT ''
	);
	-- No maximo um episodio aberto por condicao de host.
	CREATE UNIQUE INDEX alerta_aberto ON alerta (host, chave) WHERE fechado_em IS NULL;
	CREATE INDEX alerta_host ON alerta (host, chave, aberto_em);

	-- Cada mudanca de valor dentro de um episodio: 89 -> 90 desligamentos.
	CREATE TABLE alerta_valor (
		alerta_id INTEGER NOT NULL REFERENCES alerta (id) ON DELETE CASCADE,
		em        REAL NOT NULL,
		de        TEXT NOT NULL,
		para      TEXT NOT NULL,
		texto     TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX alerta_valor_id ON alerta_valor (alerta_id, em);

	-- Ultimo estado conhecido de cada host, para detectar reinicio.
	CREATE TABLE host_visto (
		host     TEXT PRIMARY KEY,
		boot_em  REAL NOT NULL,  -- quando o host ligou, pelo uptime
		visto_em REAL NOT NULL   -- ultima leitura com dados
	);

	-- Cada reinicio detectado. visto_antes e a ultima leitura antes dele: o
	-- host esteve fora entre visto_antes e boot_em. Se o cliente estava
	-- fechado, a janela e maior - mas o boot_em continua exato.
	CREATE TABLE reinicio (
		id          INTEGER PRIMARY KEY,
		host        TEXT NOT NULL,
		boot_em     REAL NOT NULL,
		visto_antes REAL,
		detectado_em REAL NOT NULL,
		UNIQUE (host, boot_em)
	);
	`,
}

func (b *Banco) migrar() error {
	var versao int
	if err := b.db.QueryRow(`PRAGMA user_version`).Scan(&versao); err != nil {
		return err
	}
	if versao > len(migracoes) {
		// Um sysmon mais novo ja mexeu neste arquivo. Ler um esquema que nao
		// conhecemos seria adivinhar; recusar e dizer por que e melhor.
		return fmt.Errorf("criado por uma versao mais nova do sysmon "+
			"(esquema %d, esta entende ate %d)", versao, len(migracoes))
	}
	for i := versao; i < len(migracoes); i++ {
		tx, err := b.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migracoes[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migracao %d: %w", i+1, err)
		}
		// PRAGMA nao aceita parametro; o numero vem do proprio codigo.
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- aceites

// Reconhecidos le os aceites vigentes.
func (b *Banco) Reconhecidos() (nucleo.Reconhecimentos, error) {
	rows, err := b.db.Query(`SELECT chave, valor, texto, aceito_em FROM aceite`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := nucleo.Reconhecimentos{}
	for rows.Next() {
		var chave string
		var r nucleo.Reconhecido
		if err := rows.Scan(&chave, &r.Valor, &r.Texto, &r.Quando); err != nil {
			return nil, err
		}
		out[chave] = r
	}
	return out, rows.Err()
}

// Gravar troca os aceites vigentes pelo conjunto dado, numa transacao, e
// registra no historico cada um que entrou, mudou ou saiu.
//
// Recebe o conjunto inteiro, e nao "aceite este", porque e assim que a tela
// trabalha: monta o mapa novo e o aplica. Calcular a diferenca aqui mantem o
// historico correto sem a tela precisar saber que ele existe.
func (b *Banco) Gravar(novos nucleo.Reconhecimentos, agora float64) error {
	return b.aplicar(novos, agora, "aceito")
}

// Importar traz os aceites de um config.json antigo. So age com o banco sem
// nenhum aceite: rodar de novo, ou depois de o usuario ja ter aceitado algo
// pela tela, nao pode ressuscitar o que ele revogou.
func (b *Banco) Importar(antigos nucleo.Reconhecimentos, agora float64) (bool, error) {
	if len(antigos) == 0 {
		return false, nil
	}
	var n int
	if err := b.db.QueryRow(`SELECT count(*) FROM aceite_historico`).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	return true, b.aplicar(antigos, agora, "importado")
}

func (b *Banco) aplicar(novos nucleo.Reconhecimentos, agora float64, acao string) error {
	atuais, err := b.Reconhecidos()
	if err != nil {
		return err
	}
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	hist := func(chave, acao string, r nucleo.Reconhecido) error {
		_, err := tx.Exec(`INSERT INTO aceite_historico (chave, acao, valor, texto, em)
			VALUES (?, ?, ?, ?, ?)`, chave, acao, r.Valor, r.Texto, agora)
		return err
	}
	for chave, r := range atuais {
		if _, fica := novos[chave]; fica {
			continue
		}
		if _, err := tx.Exec(`DELETE FROM aceite WHERE chave = ?`, chave); err != nil {
			return err
		}
		if err := hist(chave, "revogado", r); err != nil {
			return err
		}
	}
	for chave, r := range novos {
		if r.Valor == "" {
			continue // sem valor nao ha o que comparar - ver Alerta.Reconhecivel
		}
		if velho, ok := atuais[chave]; ok && velho == r {
			continue
		}
		quando := r.Quando
		if quando == 0 {
			quando = agora
		}
		if _, err := tx.Exec(`INSERT INTO aceite (chave, valor, texto, aceito_em)
			VALUES (?, ?, ?, ?)
			ON CONFLICT (chave) DO UPDATE SET valor = excluded.valor,
				texto = excluded.texto, aceito_em = excluded.aceito_em`,
			chave, r.Valor, r.Texto, quando); err != nil {
			return err
		}
		if err := hist(chave, acao, r); err != nil {
			return err
		}
	}
	return tx.Commit()
}
