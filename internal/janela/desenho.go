package janela

import (
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"sysmon/internal/tela"
)

type C = layout.Context
type D = layout.Dimensions

// Camadas do halo: (largura do traco, alfa).
//
// Larga e apagada por baixo, fina e viva por cima - e a soma que da a
// impressao de luz vazando. Aqui o alfa e de verdade; na versao Tkinter o
// mesmo efeito era simulado misturando a cor com o fundo, o que so funciona
// sobre fundo conhecido e deixa degrau visivel.
var camadasGlow = []struct {
	larg float32
	alfa uint8
}{{9, 28}, {5, 60}, {2.6, 140}, {1.4, 255}}

// Texto desenha na posicao dada e devolve a largura ocupada.
func (j *Janela) Texto(gtx C, x, y int, s string, c color.NRGBA, tam unit.Sp,
	negrito bool) int {
	if s == "" {
		return 0
	}
	defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints.Min = image.Point{}
	l := material.Label(j.th, tam, s)
	l.Color = c
	l.Font.Typeface = "Go Mono"
	l.MaxLines = 1
	if negrito {
		l.Font.Weight = font.Bold
	}
	return l.Layout(g).Size.X
}

// Medir devolve a largura de um texto sem desenhar nada.
//
// op.Record captura as operacoes num buffer que e descartado: e o jeito do
// Gio de perguntar "quanto isto ocuparia" sem sujar o quadro.
func (j *Janela) Medir(gtx C, s string, tam unit.Sp, negrito bool) int {
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints.Min = image.Point{}
	l := material.Label(j.th, tam, s)
	l.Font.Typeface = "Go Mono"
	l.MaxLines = 1
	if negrito {
		l.Font.Weight = font.Bold
	}
	d := l.Layout(g)
	m.Stop()
	return d.Size.X
}

// TextoDir alinha pela direita: os numeros ficam colados na borda, que e o
// que permite compara-los de relance entre linhas.
func (j *Janela) TextoDir(gtx C, xDir, y int, s string, c color.NRGBA,
	tam unit.Sp, negrito bool) {
	j.Texto(gtx, xDir-j.Medir(gtx, s, tam, negrito), y, s, c, tam, negrito)
}

func (j *Janela) TextoGlow(gtx C, x, y int, s string, c color.NRGBA,
	tam unit.Sp, negrito bool) {
	halo := tela.Alfa(c, 70)
	for _, d := range []image.Point{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
		j.Texto(gtx, x+d.X, y+d.Y, s, halo, tam, negrito)
	}
	j.Texto(gtx, x, y, s, c, tam, negrito)
}

func polilinha(gtx C, pts []f32.Point, c color.NRGBA, larg float32) {
	if len(pts) < 2 {
		return
	}
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(pts[0])
	for _, q := range pts[1:] {
		p.LineTo(q)
	}
	paint.FillShape(gtx.Ops, c, clip.Stroke{Path: p.End(), Width: larg}.Op())
}

func glow(gtx C, pts []f32.Point, c color.NRGBA) {
	for _, l := range camadasGlow {
		polilinha(gtx, pts, tela.Alfa(c, l.alfa), l.larg)
	}
}

func circulo(gtx C, p f32.Point, r float32, c color.NRGBA) {
	// Arredonda para fora e garante ao menos 2px de lado: o clip.Ellipse
	// truncado num retangulo de 3px sai como um traco horizontal, e um campo
	// de particulas virava um campo de hifens.
	x0, y0 := math.Floor(float64(p.X-r)), math.Floor(float64(p.Y-r))
	x1, y1 := math.Ceil(float64(p.X+r)), math.Ceil(float64(p.Y+r))
	if x1-x0 < 2 {
		x1 = x0 + 2
	}
	if y1-y0 < 2 {
		y1 = y0 + 2
	}
	caixa := image.Rect(int(x0), int(y0), int(x1), int(y1))
	paint.FillShape(gtx.Ops, c, clip.Ellipse(caixa).Op(gtx.Ops))
}

func retangulo(gtx C, r image.Rectangle, c color.NRGBA) {
	paint.FillShape(gtx.Ops, c, clip.Rect(r).Op())
}

// barra desenha o preenchimento proporcional.
//
// Retangulo de verdade, e nao os blocos █···· que a versao Tkinter montava
// com caracteres: alinha em qualquer fonte e ganha canto arredondado.
func barra(gtx C, r image.Rectangle, pct float64, c color.NRGBA) {
	paint.FillShape(gtx.Ops, tela.Alfa(tela.Grade, 220), clip.UniformRRect(r, 2).Op(gtx.Ops))
	w := int(float64(r.Dx()) * clamp(pct, 0, 100) / 100)
	if w > 0 {
		cheio := image.Rect(r.Min.X, r.Min.Y, r.Min.X+w, r.Max.Y)
		paint.FillShape(gtx.Ops, c, clip.UniformRRect(cheio, 2).Op(gtx.Ops))
	}
}

// sparkline desenha a serie como curva, nao como os caracteres ▁▂▃.
//
// A escala tem piso: oscilar entre 3.0 e 3.2 nao e novidade nenhuma, e
// autoescala pura transformaria isso num grafico dramatico.
func sparkline(gtx C, s []float64, r image.Rectangle, c color.NRGBA) {
	if len(s) < 2 {
		return
	}
	mn, mx := s[0], s[0]
	for _, v := range s {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
	}
	if mx-mn < 20 {
		meio := (mx + mn) / 2
		mn, mx = meio-10, meio+10
	}
	pts := make([]f32.Point, len(s))
	for i, v := range s {
		x := float32(r.Min.X) + float32(i)/float32(len(s)-1)*float32(r.Dx())
		y := float32(r.Max.Y) - float32(clamp((v-mn)/(mx-mn), 0, 1))*float32(r.Dy())
		pts[i] = f32.Pt(x, y)
	}
	polilinha(gtx, pts, tela.Alfa(c, 90), 3)
	polilinha(gtx, pts, c, 1.3)
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ------------------------------------------------------------------ icones
//
// Desenhados a vetor, e nao como glifo de fonte: aparecem iguais em qualquer
// sistema e escalam sem serrilhar. Cada um recebe o centro e a cor.

func tracos(gtx C, c color.NRGBA, pts ...f32.Point) { polilinha(gtx, pts, c, 2) }

func icTopo(gtx C, x, y float32, c color.NRGBA) { // sempre no topo
	tracos(gtx, c, f32.Pt(x-5, y-5), f32.Pt(x+5, y-5))
	tracos(gtx, c, f32.Pt(x, y+5), f32.Pt(x, y-2))
	tracos(gtx, c, f32.Pt(x-3.2, y-1.5), f32.Pt(x, y-4.6), f32.Pt(x+3.2, y-1.5))
}

func icBaixar(gtx C, x, y float32, c color.NRGBA) { // atualizar o programa
	tracos(gtx, c, f32.Pt(x, y-6), f32.Pt(x, y+1))
	tracos(gtx, c, f32.Pt(x-3.4, y-2.4), f32.Pt(x, y+1.4), f32.Pt(x+3.4, y-2.4))
	tracos(gtx, c, f32.Pt(x-5.5, y+5), f32.Pt(x+5.5, y+5))
}

func icAlerta(gtx C, x, y float32, c color.NRGBA) { // limiares
	tracos(gtx, c, f32.Pt(x, y-6), f32.Pt(x-6.4, y+5.5), f32.Pt(x+6.4, y+5.5),
		f32.Pt(x, y-6))
	tracos(gtx, c, f32.Pt(x, y-1.6), f32.Pt(x, y+1.6))
	circulo(gtx, f32.Pt(x, y+4), 1.2, c)
}

func icExibir(gtx C, x, y float32, c color.NRGBA) { // escolher o que aparece
	for _, dy := range []float32{-5, 0, 5} {
		tracos(gtx, c, f32.Pt(x-6, y+dy), f32.Pt(x-4.4, y+dy+1.9),
			f32.Pt(x-1.4, y+dy-2.4))
		tracos(gtx, c, f32.Pt(x+0.8, y+dy), f32.Pt(x+6, y+dy))
	}
}

func icHosts(gtx C, x, y float32, c color.NRGBA) { // servidores empilhados
	for _, dy := range []float32{-5.6, 1.1} {
		tracos(gtx, c, f32.Pt(x-6, y+dy), f32.Pt(x+6, y+dy),
			f32.Pt(x+6, y+dy+4.5), f32.Pt(x-6, y+dy+4.5), f32.Pt(x-6, y+dy))
		circulo(gtx, f32.Pt(x-3.6, y+dy+2.2), 1, c)
	}
}

func icMinimizar(gtx C, x, y float32, c color.NRGBA) {
	tracos(gtx, c, f32.Pt(x-5, y+4), f32.Pt(x+5, y+4))
}

func icFechar(gtx C, x, y float32, c color.NRGBA) {
	tracos(gtx, c, f32.Pt(x-5, y-5), f32.Pt(x+5, y+5))
	tracos(gtx, c, f32.Pt(x-5, y+5), f32.Pt(x+5, y-5))
}
