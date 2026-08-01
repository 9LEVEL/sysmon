package janela

// Rodape de dialogo: os botoes de acao, que precisam caber numa janela
// estreita.
//
// Esta ferramenta e usada encostada na lateral da tela, como um widget - uns
// 2/5 da largura. Nessa largura os dialogos encolhem junto (ver `centrado`) e
// os botoes, que tem largura de texto e nao encolhem, passavam a se sobrepor:
// o de acao a esquerda ficava por baixo do de cancelar, e o clique caia no
// errado.
//
// A regra: os da direita mandam, porque sao "salvar" e "cancelar" - as saidas.
// Se o grupo da esquerda nao couber ao lado deles, sobe uma linha.

import (
	"image"

	"gioui.org/op"

	"sysmon/internal/tela"
)

// AltRodapeDlg e a altura de uma linha de botoes, com folga.
const AltRodapeDlg = 30

// larguraGrupo soma as larguras de um grupo de botoes, com o espaco entre eles.
func (j *Janela) larguraGrupo(gtx C, bs []*Botao) int {
	total := 0
	for i, b := range bs {
		if i > 0 {
			total += 8
		}
		total += j.larguraBotao(gtx, b)
	}
	return total
}

// linhasRodape diz quantas linhas os botoes vao ocupar, sem desenhar.
//
// O chamador precisa saber ANTES de posicionar o resto: e o que define onde o
// corpo do dialogo pode terminar. Calcular depois de desenhar significaria
// descobrir a sobreposicao com o conteudo ja no lugar errado.
func (j *Janela) linhasRodape(gtx C, larg int, esq, dir []*Botao) int {
	wDir, wEsq := j.larguraGrupo(gtx, dir), j.larguraGrupo(gtx, esq)
	// 12px de folga entre os dois grupos: encostados, parecem um grupo so.
	if wEsq > 0 && wDir > 0 && 16+wEsq+12+wDir+16 > larg {
		return 2
	}
	return 1
}

// rodapeDialogo desenha os botoes e devolve quantas LINHAS ocupou.
func (j *Janela) rodapeDialogo(gtx C, larg, base int, esq, dir []*Botao) int {
	wDir := j.larguraGrupo(gtx, dir)
	linhas := j.linhasRodape(gtx, larg, esq, dir)
	yEsq := base
	if linhas == 2 {
		yEsq = base - AltRodapeDlg
	}

	x := 16
	for _, b := range esq {
		func(b *Botao, x, y int) {
			defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
			b.Layout(gtx, j)
		}(b, x, yEsq)
		x += j.larguraBotao(gtx, b) + 8
	}

	x = larg - 16 - wDir
	for _, b := range dir {
		func(b *Botao, x int) {
			defer op.Offset(image.Pt(x, base)).Push(gtx.Ops).Pop()
			b.Layout(gtx, j)
		}(b, x)
		x += j.larguraBotao(gtx, b) + 8
	}
	return linhas
}

// erroDialogo escreve a mensagem de erro acima dos botoes.
//
// Acima, e nao ao lado: ao lado ela disputava espaco com os botoes justamente
// na janela estreita, onde nao ha espaco nenhum - e um erro que nao cabe e um
// erro que ninguem le.
func (j *Janela) erroDialogo(gtx C, larg, base, linhas int, erro string) {
	if erro == "" {
		return
	}
	y := base - linhas*AltRodapeDlg + 8
	j.Texto(gtx, 16, y, j.cortarPara(gtx, erro, larg-32), tela.Vermelho, 12, false)
}

// subtitulo escreve a linha de explicacao logo abaixo do titulo do dialogo.
//
// Cortada na largura: numa janela estreita as frases passavam da borda e
// continuavam sendo desenhadas por cima da moldura, o que faz o dialogo
// parecer quebrado - e nao apenas apertado.
func (j *Janela) subtitulo(gtx C, larg int, s string) {
	j.Texto(gtx, 16, 44, j.cortarPara(gtx, s, larg-32), tela.Fraco, 12, false)
}
