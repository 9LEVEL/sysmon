package terminal

import (
	"bytes"
	"strings"
	"testing"

	"sysmon/internal/metricas"
	"sysmon/internal/nucleo"
)

func f(v float64) *float64 { return &v }

func frota() []nucleo.LeituraHost {
	return []nucleo.LeituraHost{
		{Host: nucleo.Host{Nome: "pve"}, Estado: nucleo.Estado{
			Dados: &metricas.Snapshot{
				IntervaloS: 5, CPUs: 8, CPUPercent: f(23),
				Mem:    metricas.Mem{Total: 16e9, Usado: 9e9, Percent: f(60)},
				Discos: []metricas.Disco{{Mount: "/", Percent: 96}},
			}}},
		{Host: nucleo.Host{Nome: "nas"}, Estado: nucleo.Estado{
			Erro: "conexao recusada"}},
	}
}

func desenhar(o Opcoes) string {
	var b bytes.Buffer
	Desenhar(&b, frota(), nucleo.LimiaresPadrao(), []string{"pve: disco / em 96%"}, o)
	return b.String()
}

func TestSemCorNaoEmiteEscape(t *testing.T) {
	// Sequencia ANSI dentro de um arquivo de log e lixo, e canalizar a saida
	// e metade do uso deste modo.
	s := desenhar(Opcoes{Cor: false, Largura: 100})
	if strings.Contains(s, "\x1b[") {
		t.Fatalf("vazou escape com Cor=false:\n%q", s)
	}
}

func TestComCorPintaSoOQueImporta(t *testing.T) {
	s := desenhar(Opcoes{Cor: true, Largura: 100})
	if !strings.Contains(s, "\x1b[38;5;") {
		t.Fatal("nao pintou nada")
	}
	if !strings.Contains(s, reset) {
		t.Fatal("abriu cor e nao fechou - o resto do terminal ficaria colorido")
	}
}

func TestHostOfflineApareceComOMotivo(t *testing.T) {
	s := desenhar(Opcoes{Largura: 100})
	if !strings.Contains(s, "NAS") || !strings.Contains(s, "conexao recusada") {
		t.Fatalf("host offline sumiu:\n%s", s)
	}
}

func TestAlertasNoFim(t *testing.T) {
	s := desenhar(Opcoes{Largura: 100})
	i := strings.Index(s, "! pve: disco")
	if i < 0 {
		t.Fatal("alerta nao apareceu")
	}
	if i < strings.Index(s, "DESEMPENHO") {
		t.Fatal("alerta apareceu antes da tabela")
	}
}

func TestLarguraEstreitaNaoQuebraALinha(t *testing.T) {
	// Terminal apertado corta o detalhe, que e o texto mais dispensavel -
	// nunca o nome nem o valor.
	for _, larg := range []int{60, 80, 100, 200} {
		s := desenhar(Opcoes{Largura: larg})
		for _, linha := range strings.Split(s, "\n") {
			// Mede colunas visiveis: a sequencia de cor ocupa bytes e nao
			// ocupa espaco na tela.
			if n := len([]rune(semANSI(linha))); n > larg {
				t.Errorf("largura %d: linha com %d colunas: %q", larg, n, linha)
			}
		}
	}
}

func TestBarraDeTexto(t *testing.T) {
	if got := barraTexto(0, 10); got != strings.Repeat("·", 10) {
		t.Errorf("0%% = %q", got)
	}
	if got := barraTexto(100, 10); got != strings.Repeat("█", 10) {
		t.Errorf("100%% = %q", got)
	}
	if n := len([]rune(barraTexto(37, 10))); n != 10 {
		t.Errorf("largura variou: %d", n)
	}
	// Fora da escala nao pode estourar a largura da coluna.
	if n := len([]rune(barraTexto(150, 8))); n != 8 {
		t.Errorf("150%% estourou: %d", n)
	}
}

func TestMedidaIgnoraEscape(t *testing.T) {
	// Sem isto, alinhar colunas com cor daria errado: a sequencia ocupa
	// bytes e nao ocupa coluna.
	if got := semANSI("\x1b[38;5;42mabc\x1b[0m"); got != "abc" {
		t.Fatalf("semANSI = %q", got)
	}
}

func TestCortarPreservaOInicio(t *testing.T) {
	if got := cortar("abcdefghij", 5); got != "abcd…" {
		t.Fatalf("cortar = %q", got)
	}
	if got := cortar("abc", 10); got != "abc" {
		t.Fatalf("cortou sem precisar: %q", got)
	}
}

func TestTerminalEstreitoPreservaOValor(t *testing.T) {
	// A barra e enfeite: o numero ao lado dela diz a mesma coisa com
	// precisao. Num terminal apertado quem some e ela, nunca o valor.
	s := desenhar(Opcoes{Largura: 60})
	if strings.Contains(s, "██") {
		t.Error("manteve a barra num terminal de 60 colunas")
	}
	if !strings.Contains(s, "96%") {
		t.Errorf("cortou o valor:\n%s", s)
	}
	// E com espaco sobrando a barra volta.
	if !strings.Contains(desenhar(Opcoes{Largura: 120}), "██") {
		t.Error("nao desenhou a barra com 120 colunas")
	}
}
