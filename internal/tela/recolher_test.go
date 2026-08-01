package tela

import "testing"

func arvore() []Linha {
	return []Linha{
		{Host: true, Nome: "PVE"},
		{Secao: true, Nome: "DESEMPENHO"},
		{Nome: "cpu"},
		{Nome: "memoria"},
		{Host: true, Nome: "NAS"},
		{Secao: true, Nome: "DESEMPENHO"},
		{Nome: "cpu"},
	}
}

func nomesDe(l []Linha) []string {
	out := make([]string, 0, len(l))
	for _, x := range l {
		out = append(out, x.Nome)
	}
	return out
}

func TestRecolherEscondeSoOBlocoDoHost(t *testing.T) {
	// Com dez hosts a arvore nao cabe na tela, e rolar para comparar dois
	// derrota o proposito de ter tudo visivel junto.
	got := Recolher(arvore(), map[string]bool{"PVE": true})
	quer := []string{"PVE", "NAS", "DESEMPENHO", "cpu"}
	if len(got) != len(quer) {
		t.Fatalf("linhas = %v, queria %v", nomesDe(got), quer)
	}
	for i := range quer {
		if got[i].Nome != quer[i] {
			t.Fatalf("linhas = %v, queria %v", nomesDe(got), quer)
		}
	}
}

func TestOCabecalhoDoHostNuncaSome(t *testing.T) {
	// Host que some da lista e host que ninguem lembra de expandir de novo.
	got := Recolher(arvore(), map[string]bool{"PVE": true, "NAS": true})
	if len(got) != 2 || got[0].Nome != "PVE" || got[1].Nome != "NAS" {
		t.Fatalf("linhas = %v", nomesDe(got))
	}
	for _, l := range got {
		if !l.Recolhido {
			t.Errorf("%s nao foi marcado como recolhido; a seta apontaria errado",
				l.Nome)
		}
	}
}

func TestSemNadaRecolhidoNaoMexeNaLista(t *testing.T) {
	orig := arvore()
	if got := Recolher(orig, nil); len(got) != len(orig) {
		t.Fatalf("linhas = %d, queria %d", len(got), len(orig))
	}
	if got := Recolher(orig, map[string]bool{}); len(got) != len(orig) {
		t.Fatalf("mapa vazio mexeu na lista")
	}
}

func TestNomeDesconhecidoNaoEscondeNada(t *testing.T) {
	// Host removido do config, com o recolhimento sobrando no estado da tela.
	orig := arvore()
	if got := Recolher(orig, map[string]bool{"sumiu": true}); len(got) != len(orig) {
		t.Fatalf("linhas = %v", nomesDe(got))
	}
}
