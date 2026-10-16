package banco

import (
	"path/filepath"
	"strings"
	"testing"

	"sysmon/internal/metricas"
	"sysmon/internal/nucleo"
)

func abrir(t *testing.T) (*Banco, string) {
	t.Helper()
	caminho := filepath.Join(t.TempDir(), NomeArquivo)
	b, err := Abrir(caminho)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Fechar() })
	return b, caminho
}

func TestAceitesSobrevivemAReabrir(t *testing.T) {
	b, caminho := abrir(t)
	rec := nucleo.Reconhecimentos{
		"pve/smart:sda:host:desligamento_sujo": {Valor: "89/267/33", Quando: 100, Texto: "pve: sda"},
		"dev/smart:sda:host:desligamento_sujo": {Valor: "90/213/42", Quando: 200},
	}
	if err := b.Gravar(rec, 300); err != nil {
		t.Fatal(err)
	}
	b.Fechar()

	b2, err := Abrir(caminho)
	if err != nil {
		t.Fatal(err)
	}
	defer b2.Fechar()
	lido, err := b2.Reconhecidos()
	if err != nil {
		t.Fatal(err)
	}
	if len(lido) != 2 || lido["pve/smart:sda:host:desligamento_sujo"] != rec["pve/smart:sda:host:desligamento_sujo"] {
		t.Fatalf("leu %+v", lido)
	}
	// A data do aceite e a do clique, e nao a da gravacao.
	if q := lido["dev/smart:sda:host:desligamento_sujo"].Quando; q != 200 {
		t.Fatalf("quando = %v, queria 200", q)
	}
}

func TestHistoricoDosAceitesGuardaQuemSaiu(t *testing.T) {
	b, _ := abrir(t)
	if err := b.Gravar(nucleo.Reconhecimentos{"pve/raid:md0": {Valor: "U_", Quando: 1}}, 1); err != nil {
		t.Fatal(err)
	}
	if err := b.Gravar(nucleo.Reconhecimentos{}, 2); err != nil {
		t.Fatal(err)
	}
	if rec, _ := b.Reconhecidos(); len(rec) != 0 {
		t.Fatalf("o aceite revogado continua valendo: %+v", rec)
	}
	rows, err := b.db.Query(`SELECT acao, em FROM aceite_historico ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var acoes []string
	for rows.Next() {
		var a string
		var em float64
		rows.Scan(&a, &em)
		acoes = append(acoes, a)
	}
	if strings.Join(acoes, ",") != "aceito,revogado" {
		t.Fatalf("historico = %v", acoes)
	}
}

func TestImportarSoNaPrimeiraVez(t *testing.T) {
	// Importar de novo depois de o usuario ter revogado ressuscitaria o que
	// ele desfez.
	b, _ := abrir(t)
	antigos := nucleo.Reconhecimentos{"raid:md0": {Valor: "U_", Quando: 5}}
	if ok, err := b.Importar(antigos, 10); err != nil || !ok {
		t.Fatalf("primeira importacao: ok=%v err=%v", ok, err)
	}
	if err := b.Gravar(nucleo.Reconhecimentos{}, 11); err != nil {
		t.Fatal(err)
	}
	if ok, err := b.Importar(antigos, 12); err != nil || ok {
		t.Fatalf("importou de novo: ok=%v err=%v", ok, err)
	}
	if rec, _ := b.Reconhecidos(); len(rec) != 0 {
		t.Fatalf("ressuscitou: %+v", rec)
	}
}

func TestBancoDeVersaoMaisNovaERecusado(t *testing.T) {
	b, caminho := abrir(t)
	if _, err := b.db.Exec(`PRAGMA user_version = 999`); err != nil {
		t.Fatal(err)
	}
	b.Fechar()
	if _, err := Abrir(caminho); err == nil || !strings.Contains(err.Error(), "mais nova") {
		t.Fatalf("err = %v", err)
	}
}

func TestOModoEDuravel(t *testing.T) {
	b, _ := abrir(t)
	var modo string
	var sinc int
	b.db.QueryRow(`PRAGMA journal_mode`).Scan(&modo)
	b.db.QueryRow(`PRAGMA synchronous`).Scan(&sinc)
	if modo != "wal" || sinc != 2 { // 2 = FULL
		t.Fatalf("journal_mode=%s synchronous=%d", modo, sinc)
	}
}

// ------------------------------------------------------------------ diario

func leitura(host string, ts float64, uptime int64) nucleo.LeituraHost {
	return nucleo.LeituraHost{Host: nucleo.Host{Nome: host},
		Estado: nucleo.Estado{Host: host,
			Dados: &metricas.Snapshot{TS: ts, UptimeS: uptime}}}
}

func offline(host string) nucleo.LeituraHost {
	return nucleo.LeituraHost{Host: nucleo.Host{Nome: host},
		Estado: nucleo.Estado{Host: host, Erro: "conexao recusada"}}
}

func sujo(host, valor string) nucleo.Alerta {
	return nucleo.Alerta{Host: host, Chave: "smart:sda:host:desligamento_sujo",
		Valor: valor, Texto: host + ": sda " + valor, Nivel: nucleo.Critico}
}

func TestDiarioDizDesdeQuandoEOQueMudou(t *testing.T) {
	b, _ := abrir(t)
	reg := func(agora float64, l nucleo.LeituraHost, a ...nucleo.Alerta) {
		t.Helper()
		if err := b.Registrar(agora, []nucleo.LeituraHost{l}, a); err != nil {
			t.Fatal(err)
		}
	}

	// Primeira leitura do host: o alerta ja existia - herdado, sem "desde".
	reg(1000, leitura("pve", 1000, 500), sujo("pve", "89/267/33"))
	ep := b.abertos(t)["pve/smart:sda:host:desligamento_sujo"]
	if !ep.Herdado || ep.AbertoEm != 1000 {
		t.Fatalf("episodio = %+v", ep)
	}

	// O mesmo valor de novo nao grava nada.
	reg(1005, leitura("pve", 1005, 505), sujo("pve", "89/267/33"))
	if ep := b.abertos(t)["pve/smart:sda:host:desligamento_sujo"]; ep.Mudancas != 0 {
		t.Fatalf("mudanca fantasma: %+v", ep)
	}

	// Caiu a energia: o host some, volta com uptime curto e o contador +1.
	reg(1100, offline("pve"), nucleo.Alerta{Host: "pve", Chave: "offline", Nivel: nucleo.Offline})
	if _, ok := b.abertos(t)["pve/smart:sda:host:desligamento_sujo"]; !ok {
		t.Fatal("host fora do ar fechou o alerta do disco")
	}
	reg(5000, leitura("pve", 5000, 60), sujo("pve", "90/268/34"))

	ep = b.abertos(t)["pve/smart:sda:host:desligamento_sujo"]
	m := ep.UltimaMudanca
	if m == nil || m.De != "89/267/33" || m.Para != "90/268/34" || m.Em != 5000 {
		t.Fatalf("mudanca = %+v", m)
	}
	if ep.AbertoEm != 1000 {
		t.Fatalf("a mudanca de valor abriu outro episodio: %+v", ep)
	}

	re, err := b.UltimosReinicios()
	if err != nil {
		t.Fatal(err)
	}
	// boot = ts - uptime = 4940; a ultima leitura viva antes foi a de 1000
	// (a de 1005 ficou dentro do intervalo de regravacao).
	if r := re["pve"]; r.BootEm != 4940 || r.VistoAntes != 1000 {
		t.Fatalf("reinicio = %+v", r)
	}
}

func TestAlertaSoFechaDepoisDaCarencia(t *testing.T) {
	// Temperatura oscilando no limiar nao pode virar um episodio novo a cada
	// oscilacao - senao "desde" passa a dizer "desde a ultima oscilacao".
	b, _ := abrir(t)
	quente := nucleo.Alerta{Host: "pve", Chave: "cpu:temp", Texto: "CPU em 90C", Nivel: nucleo.Aviso}
	reg := func(agora float64, a ...nucleo.Alerta) {
		t.Helper()
		if err := b.Registrar(agora, []nucleo.LeituraHost{leitura("pve", agora, 9999)}, a); err != nil {
			t.Fatal(err)
		}
	}
	reg(1000) // primeira leitura, sem alerta: o proximo nao e herdado
	reg(1010, quente)
	reg(1020)         // sumiu
	reg(1030, quente) // voltou dentro da carencia
	reg(1040)
	reg(1040 + carenciaFechar)
	if _, ok := b.abertos(t)["pve/cpu:temp"]; ok {
		t.Fatal("nao fechou depois da carencia")
	}
	var n int
	var fechado float64
	b.db.QueryRow(`SELECT count(*), max(fechado_em) FROM alerta`).Scan(&n, &fechado)
	if n != 1 || fechado != 1040 {
		t.Fatalf("episodios=%d fechado_em=%v; queria 1 episodio fechado em 1040", n, fechado)
	}

	reg(2000, quente)
	ep := b.abertos(t)["pve/cpu:temp"]
	if ep.Herdado || ep.AbertoEm != 2000 {
		t.Fatalf("episodio novo = %+v", ep)
	}
}

func (b *Banco) abertos(t *testing.T) map[string]Episodio {
	t.Helper()
	m, err := b.Abertos()
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestBancoNilNaoFazNada(t *testing.T) {
	// A janela chama o diario sem perguntar se o banco abriu.
	var b *Banco
	if err := b.Registrar(1, nil, nil); err != nil {
		t.Fatal(err)
	}
	if m, err := b.Abertos(); m != nil || err != nil {
		t.Fatal(m, err)
	}
	if err := b.Fechar(); err != nil {
		t.Fatal(err)
	}
}

func TestHostQueJaComecaForaDoArNaoENovo(t *testing.T) {
	b, _ := abrir(t)
	fora := nucleo.Alerta{Host: "nas", Chave: "offline", Nivel: nucleo.Offline}
	if err := b.Registrar(1000, []nucleo.LeituraHost{offline("nas")},
		[]nucleo.Alerta{fora}); err != nil {
		t.Fatal(err)
	}
	if ep := b.abertos(t)["nas/offline"]; !ep.Herdado {
		t.Fatalf("host nunca visto, ja fora do ar, saiu como novo: %+v", ep)
	}

	// Ja visto com dados, cair depois e novo de verdade.
	if err := b.Registrar(2000, []nucleo.LeituraHost{leitura("pve", 2000, 50)}, nil); err != nil {
		t.Fatal(err)
	}
	if err := b.Registrar(2010, []nucleo.LeituraHost{offline("pve")},
		[]nucleo.Alerta{{Host: "pve", Chave: "offline", Nivel: nucleo.Offline}}); err != nil {
		t.Fatal(err)
	}
	if ep := b.abertos(t)["pve/offline"]; ep.Herdado || ep.AbertoEm != 2010 {
		t.Fatalf("queda de host conhecido = %+v", ep)
	}
}
