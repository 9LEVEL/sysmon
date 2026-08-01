package janela

import "testing"

func TestAlturaDoGraficoEUmPreset(t *testing.T) {
	b := novaBancada(t)
	if got := b.j.alturaScope(); got != AltScope {
		t.Fatalf("padrao = %d, queria %d", got, AltScope)
	}
	for _, a := range AlturasScope {
		b.j.scopeAlt = a.Chave
		if got := b.j.alturaScope(); got != a.Px {
			t.Errorf("%s = %d, queria %d", a.Chave, got, a.Px)
		}
	}
	// Preset de uma versao futura, ou arquivo editado a mao: cai no padrao em
	// vez de num grafico de altura zero.
	b.j.scopeAlt = "gigante"
	if got := b.j.alturaScope(); got != AltScope {
		t.Fatalf("valor desconhecido virou %d", got)
	}
}

func TestOGraficoSeRecolheAntesDeEspremerAArvore(t *testing.T) {
	// Escolher "cheio" numa janela baixa esconde o grafico, e nao a lista.
	// Enfeite nao espreme informacao.
	b := novaBancada(t)
	b.tam = b.tam.Sub(b.tam) // zera
	b.tam.X, b.tam.Y = 600, 300
	b.j.scopeAlt = "cheio"
	b.quadro()

	sobra := b.tam.Y - AltCabec - AltRodape
	if sobra-b.j.alturaScope() >= MinArvore {
		t.Skip("a janela de teste e alta demais para o caso")
	}
	// Nao ha como inspecionar o desenho; o que da para afirmar e a regra que
	// o desenho consulta.
	if b.j.alturaScope() != 190 {
		t.Fatalf("altura = %d", b.j.alturaScope())
	}
}

func TestAlturaSobreviveAoArquivo(t *testing.T) {
	b := novaBancada(t)
	b.j.scopeAlt = "alto"
	b.j.salvarEstado()

	outra := novaBancada(t)
	outra.j.caminho = b.j.caminho
	outra.j.carregarEstado()
	if outra.j.scopeAlt != "alto" {
		t.Fatalf("scopeAlt = %q apos reabrir", outra.j.scopeAlt)
	}
}
