package nucleo

import "testing"

func TestBytesTemResolucaoDeDfH(t *testing.T) {
	// A referencia que todo mundo ja tem na cabeca e o df -h. Sem casa
	// decimal, 14,9G de RAM virava "15G" e 1,8T virava "2T" - e "2T / 2T"
	// ao lado de 96% parece defeito de leitura, nao arredondamento.
	//
	// Perto do teto da unidade os dois ainda coincidem, e o df -h coincide
	// igual: quem desempata e o percentual, que esta na mesma linha.
	ram := int64(16_000_000_000)
	if got := Bytes(&ram); got != "14.9G" {
		t.Fatalf("Bytes(16G) = %q, queria 14.9G", got)
	}
}

func TestBytesDistingueOrdensDeGrandeza(t *testing.T) {
	casos := []struct {
		n    int64
		quer string
	}{
		{512, "512B"},
		{2048, "2.0K"},
		{16_000_000_000, "14.9G"},
		{480_000_000_000, "447G"},
		{2_000_000_000_000, "1.8T"},
	}
	for _, c := range casos {
		if g := Bytes(&c.n); g != c.quer {
			t.Errorf("Bytes(%d) = %q, queria %q", c.n, g, c.quer)
		}
	}
}

func TestNilViraTravessaoNaoZero(t *testing.T) {
	// "0%" e uma afirmacao; "—" e a ausencia dela. Confundir os dois faz um
	// sensor ausente parecer um sensor lendo zero.
	if Bytes(nil) != "—" || Pct(nil) != "—" || Temp(nil) != "—" {
		t.Fatal("ausencia virou zero")
	}
}
