package bandeja

import (
	"strings"
	"testing"

	"sysmon/internal/nucleo"
)

func TestCorAcompanhaAJanela(t *testing.T) {
	// O icone e a janela precisam concordar: um verde no canto e um vermelho
	// na tela viram duvida sobre qual dos dois esta certo.
	casos := map[int][3]byte{
		nucleo.OK:      {0x3f, 0xb9, 0x50},
		nucleo.Aviso:   {0xd2, 0x99, 0x22},
		nucleo.Critico: {0xf8, 0x51, 0x49},
		nucleo.Offline: {0x6b, 0x76, 0x84},
	}
	vistas := map[[3]byte]bool{}
	for nivel, quer := range casos {
		r, g, b := Cor(nivel)
		if [3]byte{r, g, b} != quer {
			t.Errorf("Cor(%d) = %x%x%x, queria %x", nivel, r, g, b, quer)
		}
		vistas[[3]byte{r, g, b}] = true
	}
	if len(vistas) != 4 {
		t.Fatalf("%d cores distintas, queria 4 - dois niveis iguais na barra "+
			"de tarefas seriam indistinguiveis", len(vistas))
	}
}

func TestDicaDizOEssencialPrimeiro(t *testing.T) {
	d := Dica(nucleo.Critico, 3, 1, 5)
	if !strings.Contains(d, "3 hosts") || !strings.Contains(d, "1 offline") ||
		!strings.Contains(d, "5 alertas") {
		t.Fatalf("dica = %q", d)
	}
}

func TestDicaSemAlertaNaoInventaNumero(t *testing.T) {
	d := Dica(nucleo.OK, 1, 0, 0)
	if strings.Contains(d, "0") {
		t.Fatalf("dica = %q: zero nao precisa aparecer", d)
	}
	if !strings.Contains(d, "1 host ") && !strings.HasSuffix(d, "1 host · sem alertas") {
		t.Fatalf("dica = %q", d)
	}
}

func TestDicaCabeNoLimiteDoWindows(t *testing.T) {
	// O Windows corta a dica em 127 caracteres; cortar no meio de uma
	// palavra e pior que reticencias.
	d := Dica(nucleo.Critico, 999, 999, 999)
	if len([]rune(d)) > 127 {
		t.Fatalf("dica com %d caracteres", len([]rune(d)))
	}
}

func TestSingularEPlural(t *testing.T) {
	if !strings.Contains(Dica(nucleo.OK, 1, 0, 0), "1 host ") {
		t.Error("1 host virou plural")
	}
	if !strings.Contains(Dica(nucleo.OK, 2, 0, 0), "2 hosts") {
		t.Error("2 hosts virou singular")
	}
	if !strings.Contains(Dica(nucleo.Aviso, 2, 0, 1), "1 alerta") ||
		strings.Contains(Dica(nucleo.Aviso, 2, 0, 1), "1 alertas") {
		t.Error("1 alerta virou plural")
	}
}
