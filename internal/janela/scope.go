package janela

// Altura do grafico do topo, como preset.
//
// Ele nasceu com 46 pixels - o suficiente para dizer "esta vivo" e para a
// curva caber sem espremer a arvore. Mas quem usa a janela larga, ou quem
// esta acompanhando um pico especifico, quer ver a FORMA da curva, e em 46
// pixels uma variacao de 20 pontos percentuais e um tremor.
//
// E preset e nao arrasto porque o valor certo depende do que a pessoa esta
// fazendo naquele dia, nao de um ajuste fino: quatro opcoes cobrem o caso, e
// uma alca de redimensionar no meio da tela custaria mais atencao do que
// vale.

// AlturasScope, da mais discreta para a mais generosa.
var AlturasScope = []struct {
	Chave  string
	Rotulo string
	Px     int
}{
	{"baixo", "baixo", 46},
	{"medio", "medio", 84},
	{"alto", "alto", 130},
	{"cheio", "cheio", 190},
}

// alturaScope resolve o preset escolhido.
//
// O piso da arvore continua mandando: se o grafico nao couber junto com a
// lista, ele se recolhe sozinho (ver `mostrarScope`). Escolher "cheio" numa
// janela baixa esconde o grafico em vez de espremer o dado - enfeite nao
// espreme informacao.
func (j *Janela) alturaScope() int {
	for _, a := range AlturasScope {
		if a.Chave == j.scopeAlt {
			return a.Px
		}
	}
	return AltScope
}

func (j *Janela) rotuloAlturaScope() string {
	for _, a := range AlturasScope {
		if a.Chave == j.scopeAlt {
			return a.Rotulo
		}
	}
	return AlturasScope[0].Rotulo
}

// MargensEsq sao os presets de respiro a esquerda das escritas.
//
// Sem margem, o texto encosta na borda da janela - e, com o fio de estado do
// host desenhado ali, encosta no fio. Cinco pixels ja resolvem; quem usa a
// janela larga costuma querer mais.
var MargensEsq = []struct {
	Rotulo string
	Px     int
}{
	{"0", 0},
	{"5", 5},
	{"10", 10},
	{"20", 20},
}
