package janela

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sysmon/internal/banco"
	"sysmon/internal/metricas"
	"sysmon/internal/nucleo"
)

// bancadaComAlerta monta uma janela com um host cujo RAID esta degradado -
// alerta critico, reconhecivel, com valor estavel (o mapa de discos).
func bancadaComAlerta(t *testing.T) *bancada {
	t.Helper()
	b := novaBancada(t, nucleo.Host{Nome: "pve", URL: "http://x/metrics", Token: "t"})
	deg := true
	s := &metricas.Snapshot{IntervaloS: 5, Host: "pve", V: "5.2.0",
		Raid: []metricas.RaidArray{{Nome: "md0", Estado: "ativo",
			Discos: "U_", Degradado: &deg}}}
	b.j.frota.DefinirEstado("pve", nucleo.Estado{Dados: s})
	b.j.coletar()
	return b
}

func alertasDaJanela(b *bancada) []nucleo.Alerta {
	b.j.mu.Lock()
	defer b.j.mu.Unlock()
	return append([]nucleo.Alerta(nil), b.j.alertas...)
}

func TestAceitarUmAlertaLimpaORodapeEACor(t *testing.T) {
	// O pedido inteiro num teste: aceitar esconde o aviso e a cor volta ao
	// normal, porque o nivel e recalculado do que sobrou.
	b := bancadaComAlerta(t)
	if a := alertasDaJanela(b); len(a) != 1 {
		t.Fatalf("esperava 1 alerta, veio %+v", a)
	}
	b.j.mu.Lock()
	corAntes := b.j.corResumo
	b.j.mu.Unlock()
	if corAntes != nucleo.Critico {
		t.Fatalf("cor = %d, queria Critico", corAntes)
	}

	b.j.abrirReconhecer()
	d := b.j.dlgReconhecer
	if len(d.linhas) != 1 || d.linhas[0].botao == nil {
		t.Fatalf("a tela nao ofereceu o aceite: %+v", d.linhas)
	}
	rec := b.j.copiaReconhecidos()
	rec[d.linhas[0].a.Chave] = nucleo.Reconhecido{Valor: d.linhas[0].a.Valor}
	b.j.gravarReconhecidos(d, rec)

	if a := alertasDaJanela(b); len(a) != 0 {
		t.Fatalf("o alerta continua no rodape: %+v", a)
	}
	b.j.mu.Lock()
	corDepois := b.j.corResumo
	b.j.mu.Unlock()
	if corDepois != nucleo.OK {
		t.Fatalf("cor = %d; nao voltou ao normal", corDepois)
	}
}

func TestPiorarVoltaAAlertarMesmoAceito(t *testing.T) {
	// U_ aceito nao aceita __: um disco a menos e um evento novo.
	b := bancadaComAlerta(t)
	b.j.abrirReconhecer()
	d := b.j.dlgReconhecer
	rec := b.j.copiaReconhecidos()
	rec[d.linhas[0].a.Chave] = nucleo.Reconhecido{Valor: d.linhas[0].a.Valor}
	b.j.gravarReconhecidos(d, rec)
	if len(alertasDaJanela(b)) != 0 {
		t.Fatal("nao silenciou")
	}

	deg := true
	b.j.frota.DefinirEstado("pve", nucleo.Estado{Dados: &metricas.Snapshot{
		IntervaloS: 5, Host: "pve", V: "5.2.0",
		Raid: []metricas.RaidArray{{Nome: "md0", Estado: "ativo",
			Discos: "__", Degradado: &deg}}}})
	b.j.coletar()

	a := alertasDaJanela(b)
	if len(a) != 1 {
		t.Fatalf("nao voltou a alertar: %+v", a)
	}
	if !strings.Contains(a[0].Texto, "__") {
		t.Fatalf("alerta = %q", a[0].Texto)
	}
}

func TestAceiteSobreviveAoArquivo(t *testing.T) {
	// Uma aceitacao que some no proximo arranque e pior que uma que nao
	// aconteceu: o usuario acredita ter resolvido.
	b := bancadaComAlerta(t)
	b.j.abrirReconhecer()
	d := b.j.dlgReconhecer
	rec := b.j.copiaReconhecidos()
	rec[d.linhas[0].a.Chave] = nucleo.Reconhecido{Valor: d.linhas[0].a.Valor}
	b.j.gravarReconhecidos(d, rec)
	if d.erro != "" {
		t.Fatalf("erro ao gravar: %s", d.erro)
	}

	dados, err := os.ReadFile(b.j.caminho)
	if err != nil {
		t.Fatal(err)
	}
	var bruto map[string]any
	if err := json.Unmarshal(dados, &bruto); err != nil {
		t.Fatal(err)
	}
	m, ok := bruto["reconhecidos"].(map[string]any)
	if !ok || len(m) != 1 {
		t.Fatalf("config.json = %s", dados)
	}
	// E o config releito devolve o mesmo reconhecimento.
	cfg, err := nucleo.ConfigDe(bruto)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Limiares.Reconhecidos) != 1 {
		t.Fatalf("releitura perdeu: %+v", cfg.Limiares.Reconhecidos)
	}
}

func TestOFixoNaoOfereceAceite(t *testing.T) {
	// CPU alta nao e reconhecivel: o valor muda sozinho no ciclo seguinte, e a
	// resposta certa e o limiar. A tela mostra a linha, mas sem botao.
	b := novaBancada(t, nucleo.Host{Nome: "pve", URL: "http://x/metrics", Token: "t"})
	cem := 100.0
	quente := 95.0
	b.j.frota.DefinirEstado("pve", nucleo.Estado{Dados: &metricas.Snapshot{
		IntervaloS: 5, Host: "pve", V: "5.2.0",
		CPUTemp: &quente, CPUCrit: &cem}})
	b.j.coletar()
	b.j.abrirReconhecer()

	d := b.j.dlgReconhecer
	if len(d.linhas) == 0 {
		t.Fatal("nao listou o alerta de temperatura")
	}
	for _, l := range d.linhas {
		if !l.fixo {
			t.Fatalf("ofereceu aceite para %q", l.a.Chave)
		}
		if l.botao != nil {
			t.Fatalf("desenhou botao para %q", l.a.Chave)
		}
	}
}

func TestBotaoAcendeQuandoHaAceitos(t *testing.T) {
	// Silencio precisa ser visivel: o botao aceso e a linha no rodape sao o
	// que impede "aceitei" de virar "esqueci".
	b := bancadaComAlerta(t)
	if s := b.j.resumoReconhecidos(); s != "" {
		t.Fatalf("resumo = %q sem nada aceito", s)
	}
	b.j.abrirReconhecer()
	d := b.j.dlgReconhecer
	rec := b.j.copiaReconhecidos()
	rec[d.linhas[0].a.Chave] = nucleo.Reconhecido{Valor: d.linhas[0].a.Valor}
	b.j.gravarReconhecidos(d, rec)

	if s := b.j.resumoReconhecidos(); !strings.Contains(s, "1 alerta aceito") {
		t.Fatalf("resumo = %q", s)
	}
	if !strings.Contains(textoReconhecer(b.j), "1 alerta aceito") {
		t.Fatalf("dica = %q", textoReconhecer(b.j))
	}
}

// bancadaDoisHostsMesmoAlerta monta dois hosts com a MESMA condicao - o md0
// degradado nos dois -, mas com valores diferentes. E o caso do sda com
// desligamentos sujos no pve e no dev-254: a chave da condicao e igual.
func bancadaDoisHostsMesmoAlerta(t *testing.T) *bancada {
	t.Helper()
	b := novaBancada(t,
		nucleo.Host{Nome: "pve", URL: "http://x/metrics", Token: "t"},
		nucleo.Host{Nome: "dev", URL: "http://y/metrics", Token: "t"})
	deg := true
	for host, discos := range map[string]string{"pve": "U_", "dev": "_U"} {
		s := &metricas.Snapshot{IntervaloS: 5, Host: host, V: "5.2.0",
			Raid: []metricas.RaidArray{{Nome: "md0", Estado: "ativo",
				Discos: discos, Degradado: &deg}}}
		b.j.frota.DefinirEstado(host, nucleo.Estado{Dados: s})
	}
	b.j.coletar()
	return b
}

func TestAceitarTodosComAMesmaCondicaoEmDoisHosts(t *testing.T) {
	// O bug: as duas aceitacoes iam para a mesma chave, a segunda apagava a
	// primeira, e a cada ACEITAR TODOS um dos dois voltava - revezando.
	b := bancadaDoisHostsMesmoAlerta(t)
	if a := alertasDaJanela(b); len(a) != 2 {
		t.Fatalf("esperava 2 alertas, veio %+v", a)
	}

	b.j.abrirReconhecer()
	b.j.aceitarTodos(b.j.dlgReconhecer)

	if a := alertasDaJanela(b); len(a) != 0 {
		t.Fatalf("sobrou alerta depois de aceitar todos: %+v", a)
	}
	for _, l := range b.j.dlgReconhecer.linhas {
		if !l.aceito || l.orfao {
			t.Fatalf("linha nao ficou aceita: %+v", l)
		}
	}
	if n := len(b.j.frota.Cfg().Limiares.Reconhecidos); n != 2 {
		t.Fatalf("esperava 2 reconhecimentos, um por host; veio %d", n)
	}
}

func TestChaveAntigaSemHostContinuaValendoEMigra(t *testing.T) {
	// Um config.json gravado antes da correcao tem a chave sem host, com o
	// valor de quem gravou por ultimo. Ela segue silenciando aquele host, e
	// o ACEITAR TODOS a converte para a chave nova sem perder nada.
	b := bancadaDoisHostsMesmoAlerta(t)
	b.j.abrirReconhecer()
	d := b.j.dlgReconhecer
	var dev nucleo.Alerta
	for _, l := range d.linhas {
		if l.a.Host == "dev" {
			dev = l.a
		}
	}
	b.j.gravarReconhecidos(d, nucleo.Reconhecimentos{
		dev.Chave: {Valor: dev.Valor, Quando: 1}})

	a := alertasDaJanela(b)
	if len(a) != 1 || a[0].Host != "pve" {
		t.Fatalf("a chave antiga deveria silenciar so o dev: %+v", a)
	}

	b.j.aceitarTodos(d)
	rec := b.j.frota.Cfg().Limiares.Reconhecidos
	if _, tem := rec[dev.Chave]; tem {
		t.Fatalf("a chave antiga nao foi convertida: %+v", rec)
	}
	if r := rec[dev.ChaveAceite()]; r.Valor != dev.Valor || r.Quando != 1 {
		t.Fatalf("a conversao perdeu o aceite original: %+v", r)
	}
	if a := alertasDaJanela(b); len(a) != 0 {
		t.Fatalf("sobrou alerta: %+v", a)
	}
}

func TestAceiteVaiParaOBancoENaoParaOConfig(t *testing.T) {
	b := bancadaComAlerta(t)
	bd, err := banco.Abrir(filepath.Join(t.TempDir(), banco.NomeArquivo))
	if err != nil {
		t.Fatal(err)
	}
	defer bd.Fechar()
	b.j.Banco = bd

	b.j.abrirReconhecer()
	b.j.aceitarTodos(b.j.dlgReconhecer)

	if a := alertasDaJanela(b); len(a) != 0 {
		t.Fatalf("o alerta continua: %+v", a)
	}
	rec, err := bd.Reconhecidos()
	if err != nil || len(rec) != 1 {
		t.Fatalf("banco: %+v %v", rec, err)
	}
	if _, err := os.Stat(b.j.caminho); err == nil {
		dados, _ := os.ReadFile(b.j.caminho)
		if strings.Contains(string(dados), "reconhecidos") {
			t.Fatal("o aceite foi parar no config.json")
		}
	}
}

func TestDialogoMostraDesdeQuando(t *testing.T) {
	b := bancadaComAlerta(t)
	bd, err := banco.Abrir(filepath.Join(t.TempDir(), banco.NomeArquivo))
	if err != nil {
		t.Fatal(err)
	}
	defer bd.Fechar()
	b.j.Banco = bd
	b.j.coletar() // registra no diario

	b.j.abrirReconhecer()
	l := b.j.dlgReconhecer.linhas[0]
	// A bancada injeta a leitura sem ts, entao o host nao tem "primeira
	// leitura" registrada e o episodio nao e herdado: e novo.
	if !l.novo || !strings.HasPrefix(l.historia, "novo · desde ") {
		t.Fatalf("historia = %q novo=%v", l.historia, l.novo)
	}
	b.quadro() // desenha com a terceira linha sem quebrar nada
}

func TestHistoriaDoAlerta(t *testing.T) {
	agora := time.Unix(10*86400, 0)
	ts := func(d time.Duration) float64 { return float64(agora.Add(-d).Unix()) }

	casos := []struct {
		nome string
		ep   banco.Episodio
		re   *banco.Reinicio
		quer string
		novo bool
	}{
		{"herdado", banco.Episodio{AbertoEm: ts(72 * time.Hour), Herdado: true},
			nil, "ja estava assim na 1a leitura, em ", false},
		{"antigo", banco.Episodio{AbertoEm: ts(72 * time.Hour)}, nil, "desde ", false},
		{"novo", banco.Episodio{AbertoEm: ts(time.Hour)}, nil, "novo · desde ", true},
		{"antigo que mudou agora", banco.Episodio{AbertoEm: ts(72 * time.Hour),
			Herdado: true, UltimaMudanca: &banco.Mudanca{Em: ts(time.Hour),
				De: "89/267/33", Para: "90/268/34"}},
			&banco.Reinicio{BootEm: ts(time.Hour), VistoAntes: ts(2 * time.Hour)},
			"ja estava assim na 1a leitura, em ", true},
	}
	for _, c := range casos {
		re := banco.Reinicio{}
		if c.re != nil {
			re = *c.re
		}
		got, novo := historia(c.ep, true, re, c.re != nil, agora)
		if !strings.HasPrefix(got, c.quer) || novo != c.novo {
			t.Errorf("%s: %q novo=%v", c.nome, got, novo)
		}
		if c.ep.UltimaMudanca != nil && !strings.Contains(got, "novo: 89/267/33 -> 90/268/34 em ") {
			t.Errorf("%s: sem a mudanca: %q", c.nome, got)
		}
		if c.re != nil && !strings.Contains(got, "host religou ") {
			t.Errorf("%s: sem o reinicio: %q", c.nome, got)
		}
	}
	if got, _ := historia(banco.Episodio{}, false, banco.Reinicio{}, false, agora); got != "" {
		t.Errorf("sem episodio: %q", got)
	}
}
