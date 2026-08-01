package nucleo

import (
	"strings"
	"testing"

	"sysmon/internal/metricas"
)

func alerta(chave, valor, texto string, nivel int) Alerta {
	return Alerta{Chave: chave, Valor: valor, Texto: texto, Nivel: nivel}
}

func TestReconhecerSilenciaEnquantoOValorNaoMuda(t *testing.T) {
	// O caso que motivou o recurso: 89 desligamentos sujos e um fato conhecido
	// e aceito; nao precisa ser repetido a cada 3 segundos para sempre.
	a := alerta("smart:sda:host:desligamento_sujo", "89/206/43",
		"disco sda (energia do host): 89 de 206 ...", Critico)
	rec := Reconhecimentos{a.Chave: {Valor: a.Valor}}

	if !rec.Cobre(a) {
		t.Fatal("nao silenciou o que foi aceito")
	}
	if got := rec.Filtrar([]Alerta{a}); len(got) != 0 {
		t.Fatalf("sobrou %+v", got)
	}
	if n := NivelDe(rec.Filtrar([]Alerta{a})); n != OK {
		t.Fatalf("nivel = %d; a cor nao voltou ao normal", n)
	}
}

func TestValorNovoVoltaAAlertar(t *testing.T) {
	// De 89 para 90 e um evento novo - e a razao de o reconhecimento guardar o
	// valor em vez de so a chave.
	rec := Reconhecimentos{"smart:sda:host:desligamento_sujo": {Valor: "89/206/43"}}
	novo := alerta("smart:sda:host:desligamento_sujo", "90/207/43", "...", Critico)

	if rec.Cobre(novo) {
		t.Fatal("continuou silenciado depois de piorar")
	}
	if n := NivelDe(rec.Filtrar([]Alerta{novo})); n != Critico {
		t.Fatalf("nivel = %d, queria Critico", n)
	}
}

func TestContadorQueDesceTambemVoltaAAlertar(t *testing.T) {
	// Contador que diminui significa serie reiniciada - disco trocado de baia,
	// firmware que zerou a tabela. Merece ser visto de novo, e nao herdar o
	// "eu ja sei" de um disco que talvez nem seja o mesmo.
	rec := Reconhecimentos{"smart:sda:realocados": {Valor: "200"}}
	if rec.Cobre(alerta("smart:sda:realocados", "4", "...", Aviso)) {
		t.Fatal("herdou o reconhecimento apos a regressao")
	}
}

func TestOQueNaoTemValorNaoEReconhecivel(t *testing.T) {
	// CPU, RAM e temperatura sobem e descem o tempo todo: aceitar "CPU em 82%"
	// congelaria um numero que ja mudou no ciclo seguinte. Para esses existe o
	// ajuste de limiar. Host fora do ar e coleta parada sao transitorios, e
	// silenciar um agente morto seria a ferramenta mentindo por omissao.
	for _, chave := range []string{"cpu:temp", "ram", "psi:io", "offline",
		"coleta:parada", "disco:sda:temp"} {
		a := alerta(chave, "", "...", Critico)
		if a.Reconhecivel() {
			t.Errorf("%s nao devia ser reconhecivel", chave)
		}
		// E nem por acidente: um reconhecimento com a chave dele nao pega.
		rec := Reconhecimentos{chave: {Valor: ""}}
		if rec.Cobre(a) {
			t.Errorf("%s foi silenciado mesmo sem valor", chave)
		}
	}
}

func TestSoNumerosIgnoraARedacao(t *testing.T) {
	// Se o valor fosse a frase inteira, reescrever uma mensagem numa versao
	// nova perderia o reconhecimento de todo mundo por nada.
	a := "89 de 206 desligamentos foram inesperados (43%)"
	b := "inesperados: 89 de 206 (43%)"
	if soNumeros(a) != soNumeros(b) {
		t.Fatalf("%q != %q", soNumeros(a), soNumeros(b))
	}
	if soNumeros(a) != "89/206/43" {
		t.Fatalf("valor = %q", soNumeros(a))
	}
	// Mas o numero mudando muda o valor, que e o ponto.
	if soNumeros(a) == soNumeros("90 de 207 desligamentos ... (43%)") {
		t.Fatal("mudar a contagem nao mudou o valor")
	}
}

func TestReconhecimentoSobreviveAoArquivo(t *testing.T) {
	// Ele mora no config.json, na raiz: alertas sao a regra, reconhecimento e
	// a excecao a ela, e um mapa so tornaria o arquivo ilegivel a mao.
	bruto := map[string]any{
		"hosts": []any{},
		"reconhecidos": map[string]any{
			"smart:sda:host:desligamento_sujo": map[string]any{
				"valor": "89/206/43", "quando": 1.0, "texto": "energia"},
			"sem_valor": map[string]any{"quando": 2.0},
		},
	}
	lim := LimiaresDe(bruto)
	if got := lim.Reconhecidos["smart:sda:host:desligamento_sujo"].Valor; got != "89/206/43" {
		t.Fatalf("valor = %q", got)
	}
	// Entrada sem valor e descartada: guardar seria silenciar para sempre.
	if _, tem := lim.Reconhecidos["sem_valor"]; tem {
		t.Error("guardou reconhecimento sem valor")
	}

	cfg, err := ConfigDe(bruto)
	if err != nil {
		t.Fatal(err)
	}
	volta := cfg.ComoBruto()
	rec, ok := volta["reconhecidos"].(map[string]any)
	if !ok || len(rec) != 1 {
		t.Fatalf("ida e volta perdeu o reconhecimento: %v", volta["reconhecidos"])
	}
}

func TestAvaliarAplicaOsReconhecimentos(t *testing.T) {
	// A ponta a ponta: um RAID degradado aceito some do rodape e a cor volta;
	// se o mapa de discos piorar, ele reaparece.
	s := snap()
	deg := true
	s.Raid = []metricas.RaidArray{{Nome: "md0", Estado: "ativo",
		Discos: "U_", Degradado: &deg}}

	lim := LimiaresPadrao()
	n, alertas := Avaliar(est(s), lim)
	if n != Critico || len(alertas) != 1 {
		t.Fatalf("nivel=%d alertas=%v", n, alertas)
	}

	lim.Reconhecidos = Reconhecimentos{alertas[0].Chave: {Valor: alertas[0].Valor}}
	if n, a := Avaliar(est(s), lim); n != OK || len(a) != 0 {
		t.Fatalf("nao silenciou: nivel=%d alertas=%v", n, a)
	}

	// Um disco a menos: valor diferente, volta a alertar.
	s.Raid[0].Discos = "__"
	if n, a := Avaliar(est(s), lim); n != Critico || len(a) != 1 {
		t.Fatalf("nao voltou a alertar: nivel=%d alertas=%v", n, a)
	}

	// E o bruto continua mostrando tudo, para a tela poder revogar.
	s.Raid[0].Discos = "U_"
	if _, a := AvaliarBruto(est(s), lim); len(a) != 1 {
		t.Fatalf("o bruto escondeu o reconhecido: %v", a)
	}
}

func TestTextosPreservaAOrdem(t *testing.T) {
	got := strings.Join(Textos([]Alerta{
		alerta("a", "", "primeiro", Aviso),
		alerta("b", "", "segundo", Critico),
	}), "|")
	if got != "primeiro|segundo" {
		t.Fatalf("got = %q", got)
	}
}
