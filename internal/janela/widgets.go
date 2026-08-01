package janela

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"sysmon/internal/tela"
)

// Widgets dos dialogos.
//
// O Gio traz material.Editor e material.CheckBox, mas com a cara do Material
// Design: fundo claro, sublinhado animado, cantos arredondados. Numa janela
// que e um painel escuro e monoespacado, isso destoa a ponto de parecer outro
// programa. Sao poucos widgets e o controle vale o codigo.

// Campo e uma entrada de texto de uma linha.
type Campo struct {
	ed     widget.Editor
	Rotulo string
	Larg   int
}

func NovoCampo(rotulo string, larg int) *Campo {
	c := &Campo{Rotulo: rotulo, Larg: larg}
	c.ed.SingleLine = true
	c.ed.Submit = true
	return c
}

func (c *Campo) Texto() string    { return c.ed.Text() }
func (c *Campo) Definir(s string) { c.ed.SetText(s) }

// Focado pergunta ao roteador de eventos quem tem o teclado. Em versoes
// recentes do Gio o estado de foco saiu do widget e virou consulta ao
// contexto - o widget nao guarda mais isso sozinho.
func (c *Campo) Focado(gtx C) bool      { return gtx.Focused(&c.ed) }
func (c *Campo) Editor() *widget.Editor { return &c.ed }

// Layout desenha o campo e devolve a altura ocupada.
func (c *Campo) Layout(gtx C, j *Janela) D {
	larg := c.Larg
	if larg <= 0 {
		larg = gtx.Constraints.Max.X
	}
	const alt = 26

	// A borda muda com o foco: sem isso, num tema escuro, nao ha como saber
	// em qual campo o teclado esta batendo.
	borda := tela.Grade
	if gtx.Focused(&c.ed) {
		borda = tela.Titulo
	}
	r := image.Rect(0, 0, larg, alt)
	paint.FillShape(gtx.Ops, tela.Painel, clip.UniformRRect(r, 3).Op(gtx.Ops))
	paint.FillShape(gtx.Ops, borda,
		clip.Stroke{Path: clip.UniformRRect(r, 3).Path(gtx.Ops), Width: 1}.Op())

	defer op.Offset(image.Pt(7, 5)).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints = layout.Exact(image.Pt(larg-14, alt-10))
	e := material.Editor(j.th, &c.ed, c.Rotulo)
	e.Color = tela.Texto
	e.HintColor = tela.Fraco
	e.Font.Typeface = "Go Mono"
	e.TextSize = unit.Sp(12)
	e.Layout(g)
	return D{Size: image.Pt(larg, alt)}
}

// Caixa e uma caixa de marcacao.
type Caixa struct {
	b      widget.Bool
	Rotulo string
}

func NovaCaixa(rotulo string, marcada bool) *Caixa {
	c := &Caixa{Rotulo: rotulo}
	c.b.Value = marcada
	return c
}

func (c *Caixa) Marcada() bool    { return c.b.Value }
func (c *Caixa) Marcar(v bool)    { c.b.Value = v }
func (c *Caixa) Mudou(gtx C) bool { return c.b.Update(gtx) }

func (c *Caixa) Layout(gtx C, j *Janela, cor color.NRGBA, negrito bool) D {
	larg := gtx.Constraints.Max.X
	const alt = 20
	g := gtx
	g.Constraints = layout.Exact(image.Pt(larg, alt))
	c.b.Update(g)
	return c.b.Layout(g, func(g C) D {
		r := image.Rect(2, 4, 14, 16)
		paint.FillShape(g.Ops, tela.Painel, clip.UniformRRect(r, 2).Op(g.Ops))
		cb := tela.Grade
		if c.b.Hovered() {
			cb = tela.Fraco
		}
		paint.FillShape(g.Ops, cb,
			clip.Stroke{Path: clip.UniformRRect(r, 2).Path(g.Ops), Width: 1}.Op())
		if c.b.Value {
			// Marca desenhada, e nao um caractere: alinha igual em qualquer
			// fonte e nao depende de o sistema ter o glifo.
			tracos(g, tela.Titulo, ptf(4.5, 10), ptf(7, 13), ptf(11.5, 6.5))
		}
		j.Texto(g, 22, 2, c.Rotulo, cor, 12, negrito)
		return D{Size: image.Pt(larg, alt)}
	})
}

// Botao e um botao de texto.
type Botao struct {
	c      widget.Clickable
	Rotulo string
	Cor    color.NRGBA
}

func NovoBotao(rotulo string, cor color.NRGBA) *Botao {
	return &Botao{Rotulo: rotulo, Cor: cor}
}

func (b *Botao) Clicado(gtx C) bool { return b.c.Clicked(gtx) }

func (b *Botao) Layout(gtx C, j *Janela) D {
	larg := j.Medir(gtx, b.Rotulo, 12, true) + 22
	const alt = 26
	g := gtx
	g.Constraints = layout.Exact(image.Pt(larg, alt))
	return b.c.Layout(g, func(g C) D {
		r := image.Rect(0, 0, larg, alt)
		fundo := tela.Painel
		if b.c.Hovered() {
			fundo = tela.Grade
		}
		paint.FillShape(g.Ops, fundo, clip.UniformRRect(r, 3).Op(g.Ops))
		paint.FillShape(g.Ops, tela.Alfa(b.Cor, 160),
			clip.Stroke{Path: clip.UniformRRect(r, 3).Path(g.Ops), Width: 1}.Op())
		j.Texto(g, 11, 5, b.Rotulo, b.Cor, 12, true)
		return D{Size: image.Pt(larg, alt)}
	})
}

func ptf(x, y float32) f32.Point { return f32.Point{X: x, Y: y} }

// tela.Painel desenha o fundo de um dialogo modal.
//
// A cortina escurece o que esta atras em vez de esconder: continua dando
// para ver que a frota esta la, o que evita a sensacao de ter trocado de
// programa ao abrir uma configuracao.
func (j *Janela) cortina(gtx C) {
	retangulo(gtx, image.Rectangle{Max: gtx.Constraints.Max}, tela.Alfa(tela.Fundo, 225))
}

// moldura desenha o quadro de um dialogo.
//
// A borda era cinza (tela.Grade), a mesma cor das linhas de grade do fundo:
// o dialogo parecia uma area desenhada por cima, e nao uma coisa em primeiro
// plano. Agora e ciano com halo - o mesmo tom que a curva do topo usa para
// dizer "isto esta vivo" -, e a diferenca e o dialogo passar a ter borda em
// vez de contorno.
func (j *Janela) moldura(gtx C, r image.Rectangle, titulo string) {
	paint.FillShape(gtx.Ops, tela.Painel, clip.UniformRRect(r, 5).Op(gtx.Ops))

	// Halo: tres tracos concentricos, do mais largo e apagado para o mais
	// fino e vivo. E a mesma receita do glow das curvas.
	for _, camada := range []struct {
		fora float32
		larg float32
		alfa uint8
	}{{5, 6, 16}, {2.5, 3, 34}, {0, 1.4, 210}} {
		rr := image.Rect(r.Min.X-int(camada.fora), r.Min.Y-int(camada.fora),
			r.Max.X+int(camada.fora), r.Max.Y+int(camada.fora))
		paint.FillShape(gtx.Ops, tela.Alfa(tela.Ativo, camada.alfa),
			clip.Stroke{Path: clip.UniformRRect(rr, 5).Path(gtx.Ops),
				Width: camada.larg}.Op())
	}

	j.TextoGlow(gtx, r.Min.X+16, r.Min.Y+12, titulo, tela.Ativo, 13, true)
	// A regra sob o titulo acompanha, apagada: separa sem virar segunda borda.
	retangulo(gtx, image.Rect(r.Min.X+16, r.Min.Y+34, r.Max.X-16, r.Min.Y+35),
		tela.Alfa(tela.Ativo, 90))
}

// centrado devolve um retangulo centrado na janela, respeitando o tamanho
// dela: dialogo maior que a janela nao caberia, e cortar botao de salvar e
// pior que apertar o conteudo.
func centrado(gtx C, larg, alt int) image.Rectangle {
	max := gtx.Constraints.Max
	if larg > max.X-20 {
		larg = max.X - 20
	}
	if alt > max.Y-20 {
		alt = max.Y - 20
	}
	x := (max.X - larg) / 2
	y := (max.Y - alt) / 2
	return image.Rect(x, y, x+larg, y+alt)
}
