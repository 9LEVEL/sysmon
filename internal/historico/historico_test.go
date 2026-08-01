package historico

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"sysmon/internal/metricas"
)

func c(v int64) *int64 { return &v }

// leitura monta um Smart com um contador so, que e o suficiente para exercitar
// a serie - o resto dos campos nao participa do calculo.
func leitura(serial string, realocados int64) *metricas.Smart {
	return &metricas.Smart{
		Serial: serial, ColetaOK: true,
		Atributos: []metricas.SmartAtributo{
			{ID: 5, Nome: "Reallocated_Sector_Ct", Cru: c(realocados)},
		},
	}
}

func em(dias float64) time.Time {
	base := time.Unix(1_700_000_000, 0)
	return base.Add(time.Duration(dias * float64(24*time.Hour)))
}

func novo(t *testing.T) *Arquivo {
	t.Helper()
	return Abrir(filepath.Join(t.TempDir(), "h.json"))
}

// grava alimenta a serie dia a dia e devolve a ultima leitura ja anotada.
func grava(a *Arquivo, serial string, valores map[float64]int64) *metricas.Smart {
	dias := make([]float64, 0, len(valores))
	for d := range valores {
		dias = append(dias, d)
	}
	// Ordem cronologica: a serie e append-only e nao reordena.
	for i := 0; i < len(dias); i++ {
		for j := i + 1; j < len(dias); j++ {
			if dias[j] < dias[i] {
				dias[i], dias[j] = dias[j], dias[i]
			}
		}
	}
	var ultimo *metricas.Smart
	for _, d := range dias {
		ultimo = leitura(serial, valores[d])
		a.Aplicar(ultimo, em(d))
	}
	return ultimo
}

func attr(s *metricas.Smart) metricas.SmartAtributo { return s.Atributos[0] }

// ------------------------------------------------------------------ deltas

func TestSemHistoricoDeltaEnil(t *testing.T) {
	// A distincao que a especificacao exige: "nao sei" e diferente de zero.
	// Um disco recem-instalado com 3 setores realocados nao pode virar "0
	// novos setores nos ultimos 30 dias".
	a := novo(t)
	s := leitura("X1", 3)
	a.Aplicar(s, em(0))
	at := attr(s)
	if at.Delta24h != nil || at.Delta7d != nil || at.Delta30d != nil {
		t.Fatalf("inventou baseline: %+v", at)
	}
	if at.Amostras != 1 {
		t.Errorf("amostras = %d", at.Amostras)
	}
}

func TestDeltasContamDaJanelaCerta(t *testing.T) {
	a := novo(t)
	// valor 0 ha 40 dias, 4 ha 10, 6 ha 5, 10 agora.
	s := grava(a, "X1", map[float64]int64{0: 0, 30: 4, 35: 6, 40: 10})
	at := attr(s)
	// 30 dias atras e o dia 10; a amostra que vale e a ultima ANTERIOR a ele,
	// que e a do dia 0.
	if at.Delta30d == nil || *at.Delta30d != 10 {
		t.Errorf("delta30d = %v, queria 10", at.Delta30d)
	}
	// 7 dias atras e o dia 33: a do dia 35 ja esta dentro da janela e nao
	// serve de baseline; a valida e a do dia 30, com valor 4.
	if at.Delta7d == nil || *at.Delta7d != 6 {
		t.Errorf("delta7d = %v, queria 6", at.Delta7d)
	}
	if at.Base30d == nil || *at.Base30d != 0 {
		t.Errorf("base30d = %v, queria 0", at.Base30d)
	}
}

func TestJanelaMaisLongaQueOHistoricoFicaSemDado(t *testing.T) {
	// Historico de 10 dias responde 24h e 7d, mas nao 30d. Parcial e util;
	// extrapolar seria inventar taxa justo no disco novo.
	a := novo(t)
	s := grava(a, "X1", map[float64]int64{0: 5, 9: 5, 10: 8})
	at := attr(s)
	if at.Delta7d == nil || *at.Delta7d != 3 {
		t.Errorf("delta7d = %v, queria 3", at.Delta7d)
	}
	if at.Delta30d != nil {
		t.Errorf("delta30d = %v, queria nil", *at.Delta30d)
	}
}

func TestNaoAmostraMaisQueUmaVezPorIntervalo(t *testing.T) {
	// O coletor chama isto a cada 5 s. Se cada chamada virasse amostra, o
	// arquivo cresceria 17 mil pontos por dia e a janela de 24 h teria a
	// resolucao errada.
	a := novo(t)
	base := em(0)
	for i := 0; i < 100; i++ {
		a.Aplicar(leitura("X1", 5), base.Add(time.Duration(i)*5*time.Second))
	}
	s := leitura("X1", 5)
	a.Aplicar(s, base.Add(2*time.Hour))
	if n := attr(s).Amostras; n != 2 {
		t.Fatalf("amostras = %d, queria 2", n)
	}
}

// -------------------------------------------------------------- integridade

func TestContadorQueDiminuiRecomecaASerie(t *testing.T) {
	// Contador SMART so cresce. Ter caido significa que este serial nao conta
	// mais a mesma historia: deltas calculados por cima sairiam negativos, e
	// negativo em contador de degradacao nao tem leitura possivel.
	a := novo(t)
	grava(a, "X1", map[float64]int64{0: 10, 1: 12, 2: 14})
	s := leitura("X1", 4)
	a.Aplicar(s, em(3))
	at := attr(s)
	if at.Amostras != 1 {
		t.Errorf("amostras = %d, queria 1 (serie reiniciada)", at.Amostras)
	}
	if at.Delta7d != nil {
		t.Errorf("delta sobreviveu a regressao: %d", *at.Delta7d)
	}
}

func TestSeriaisNaoSeMisturam(t *testing.T) {
	// sda vira sdb quando alguem troca a ordem dos cabos. O historico segue o
	// serial, nao a baia.
	a := novo(t)
	grava(a, "X1", map[float64]int64{0: 0, 10: 50})
	s := leitura("X2", 3)
	a.Aplicar(s, em(10))
	if d := attr(s).Delta7d; d != nil {
		t.Fatalf("herdou historico do outro disco: %d", *d)
	}
}

func TestColetaFalhaNaoVaiParaOHistorico(t *testing.T) {
	// Gravar uma leitura que nao aconteceu criaria um degrau falso no proximo
	// delta.
	a := novo(t)
	s := &metricas.Smart{Serial: "X1", ColetaOK: false, ErroColeta: "sem acesso"}
	if a.Aplicar(s, em(0)) {
		t.Fatal("registrou coleta falha")
	}
	if len(a.Seriais()) != 0 {
		t.Fatalf("seriais = %v", a.Seriais())
	}
}

func TestSemSerialNaoRegistra(t *testing.T) {
	// Sem serial nao ha chave estavel; indexar por dev faria dois discos
	// compartilharem historico depois de um recabeamento.
	a := novo(t)
	s := leitura("", 5)
	if a.Aplicar(s, em(0)) {
		t.Fatal("registrou sem serial")
	}
}

// ------------------------------------------------------------- persistencia

func TestSobreviveAoReinicio(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), "h.json")
	a := Abrir(caminho)
	grava(a, "X1", map[float64]int64{0: 0, 8: 5})
	if err := a.Salvar(); err != nil {
		t.Fatal(err)
	}

	// Sem persistencia a amostra do dia 0 teria sumido e o delta seria nil;
	// com ela, a baseline de 7 dias atras (dia 1,5) e o valor 0 do dia 0.
	b := Abrir(caminho)
	s := leitura("X1", 9)
	b.Aplicar(s, em(8.5))
	if d := attr(s).Delta7d; d == nil || *d != 9 {
		t.Fatalf("delta7d = %v, queria 9", d)
	}
}

func TestArquivoCorrompidoComecaDoZeroSemDerrubar(t *testing.T) {
	// Perder o passado e ruim; nao gravar o futuro e pior. E o agente nao
	// pode deixar de responder /metrics por causa disto.
	caminho := filepath.Join(t.TempDir(), "h.json")
	if err := os.WriteFile(caminho, []byte("{lixo"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := Abrir(caminho)
	if a.Erro() == nil {
		t.Error("nao reportou o arquivo ilegivel")
	}
	s := leitura("X1", 1)
	if !a.Aplicar(s, em(0)) {
		t.Fatal("nao registrou depois do arquivo corrompido")
	}
	if err := a.Salvar(); err != nil {
		t.Fatal(err)
	}
}

func TestCompactacaoLimitaOTamanho(t *testing.T) {
	// Uma amostra por hora durante 180 dias seriam 4320 pontos por disco,
	// para responder perguntas que so olham 24 h, 7 e 30 dias.
	a := novo(t)
	s := leitura("X1", 0)
	for h := 0; h < 200*24; h++ {
		s = leitura("X1", int64(h))
		a.Aplicar(s, em(0).Add(time.Duration(h)*time.Hour))
	}
	n := attr(s).Amostras
	if n > 300 {
		t.Fatalf("amostras = %d; a compactacao nao esta rodando", n)
	}
	// E ainda assim precisa responder as tres janelas.
	at := attr(s)
	if at.Delta24h == nil || at.Delta7d == nil || at.Delta30d == nil {
		t.Fatalf("compactou demais: %+v", at)
	}
	// Dentro da janela densa a resolucao e horaria e a resposta e exata.
	if *at.Delta24h != 24 {
		t.Errorf("delta24h = %d, queria 24", *at.Delta24h)
	}
	// Fora dela e diaria, entao a baseline de "30 dias atras" pode estar ate
	// um dia mais para tras - o delta sai por cima, nunca por baixo, que e o
	// lado seguro para quem esta tentando prever falha de disco.
	if *at.Delta30d < 30*24 || *at.Delta30d > 31*24 {
		t.Errorf("delta30d = %d, fora de [%d,%d]", *at.Delta30d, 30*24, 31*24)
	}
}

func TestRetencaoDescartaOAntigo(t *testing.T) {
	a := novo(t)
	s := leitura("X1", 0)
	for d := 0; d < 200; d++ {
		s = leitura("X1", int64(d))
		a.Aplicar(s, em(float64(d)))
	}
	if n := attr(s).Amostras; n > RetencaoDias+2 {
		t.Fatalf("amostras = %d, retencao e de %d dias", n, RetencaoDias)
	}
}

func TestCaminhoPadraoSegueOSystemd(t *testing.T) {
	t.Setenv("STATE_DIRECTORY", "/var/lib/private/sysmon:/outro")
	if got := CaminhoPadrao(); got != "/var/lib/private/sysmon/smart-historico.json" {
		t.Fatalf("caminho = %q", got)
	}
}
