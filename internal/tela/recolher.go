package tela

// Recolher esconde as linhas dos hosts marcados, deixando so o cabecalho.
//
// Com quatro hosts a arvore inteira cabe na tela; com dez, nao cabe nem perto,
// e rolar para comparar dois deles derrota o proposito de ter tudo visivel
// junto. Recolher o que ja foi conferido e o que devolve a comparacao de
// relance.
//
// Mora aqui, e nao no desenho, porque e decisao sobre o que aparece - a mesma
// natureza de Visiveis. O desenho continua burro.
func Recolher(linhas []Linha, recolhidos map[string]bool) []Linha {
	if len(recolhidos) == 0 {
		return linhas
	}
	out := make([]Linha, 0, len(linhas))
	pulando := false
	for _, l := range linhas {
		if l.Host {
			// O cabecalho do host SEMPRE aparece: recolher esconde o detalhe,
			// nao o host. Um host que some da lista e um host que ninguem
			// lembra de expandir de novo.
			pulando = recolhidos[l.Nome]
			l.Recolhido = pulando
			out = append(out, l)
			continue
		}
		if !pulando {
			out = append(out, l)
		}
	}
	return out
}
