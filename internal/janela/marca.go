package janela

import (
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"

	"sysmon/internal/tela"
)

// A assinatura do rodape, com brilho e umas faiscas em volta.
//
// A regra que ela obedece: ser notada e nao ser lida. Fica no canto oposto ao
// que se olha, o brilho e fraco e as particulas sao de um pixel - a ideia e
// que o olho perceba movimento de relance, e nao que dispute com o dado que
// esta na tela. Se em algum momento isso atrapalhar a leitura de um alerta, a
// dose esta errada.

const (
	marcaTexto = "9LEVEL.com.br"
	marcaURL   = "https://9level.com.br"

	// Quantas faiscas. Dezesseis bastam para o olho perceber movimento; mais
	// que isso vira confete no canto de uma ferramenta de monitoramento.
	nFaiscas = 16

	// Um ciclo completo. Longo de proposito: particula rapida chama atencao
	// como se fosse um aviso, e nada aqui e aviso.
	cicloFaisca = 7 * time.Second

	// A varredura do brilho pelas letras - o efeito de nickname de forum que
	// o pedido citou. Mais lenta que as faiscas, e com uma pausa entre
	// passadas: brilho que varre sem parar cansa em dez segundos.
	cicloBrilho = 4200 * time.Millisecond

	// Quantas fatias verticais compoem a varredura. O Gio nao tem gradiente
	// aplicado a glifo; o degrade sai de redesenhar o texto recortado em
	// faixas, cada uma com o alfa da envoltoria. Vinte fatias num texto de
	// ~90px dao ~4px cada, que a esta escala o olho le como continuo.
	fatiasBrilho = 20
)

// faisca e uma particula em orbita da assinatura.
//
// As orbitas sao definidas por constantes primas entre si para que o conjunto
// demore a se repetir - assim o movimento nao vira um padrao reconhecivel,
// que e quando o efeito comeca a incomodar.
type faisca struct {
	fase   float64 // 0..1, onde comeca no ciclo
	raioX  float64 // amplitude horizontal, em fracao da largura do texto
	raioY  float64 // amplitude vertical, em pixels
	veloc  float64 // multiplicador do ciclo
	brilho uint8
}

var faiscas = func() [nFaiscas]faisca {
	var out [nFaiscas]faisca
	for i := range out {
		f := float64(i) / nFaiscas
		out[i] = faisca{
			fase:   f,
			raioX:  0.52 + 0.20*math.Sin(float64(i)*2.399),
			raioY:  6 + 6*math.Cos(float64(i)*1.618),
			veloc:  0.7 + 0.45*math.Sin(float64(i)*0.911),
			brilho: uint8(110 + 110*math.Abs(math.Sin(float64(i)*1.27))),
		}
	}
	return out
}()

// marca desenha a assinatura clicavel e devolve onde ela comeca.
func (j *Janela) marca(gtx C, r image.Rectangle) int {
	w := j.Medir(gtx, marcaTexto, 11, false)
	// A alca de redimensionar mora nos ultimos 18px do canto. A assinatura
	// para antes dela: texto por baixo de alca nao e clicavel nem legivel.
	x := r.Max.X - Margem - larguraCanto - w
	y := r.Min.Y + 6
	hover := j.btMarca.Hovered()

	texto := image.Rect(x, y, x+w, y+14)
	j.faiscasDaMarca(gtx, texto, hover)

	defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, 16))
	j.btMarca.Layout(g, func(g C) D {
		cor := tela.Alfa(tela.Titulo, 200)
		if hover {
			cor = tela.Titulo
			pointer.CursorPointer.Add(g.Ops)
			// Sublinhado so no hover: linha permanente competiria com o
			// rodape, que e onde os avisos aparecem.
			retangulo(g, image.Rect(0, 14, w, 15), tela.Alfa(cor, 120))
		}
		j.marcaBrilhante(g, w, cor, hover)
		return D{Size: image.Pt(w, 16)}
	})
	return x
}

// faiscasDaMarca desenha as particulas orbitando a assinatura.
//
// O relogio entra pelo tempo de parede e nao por contador de quadros: a
// janela redesenha a ~15 Hz, mas perde quadros quando o sistema esta
// ocupado, e um contador faria a animacao acelerar e desacelerar junto com a
// carga da maquina - justo numa ferramenta que existe para mostrar carga.
func (j *Janela) faiscasDaMarca(gtx C, texto image.Rectangle, hover bool) {
	t := float64(time.Now().UnixNano()%int64(cicloFaisca)) / float64(cicloFaisca)
	cx := float64(texto.Min.X+texto.Max.X) / 2
	cy := float64(texto.Min.Y+texto.Max.Y) / 2
	largura := float64(texto.Dx() + 30)

	// Onde as letras estao, para as particulas apagarem ao cruzar. Por cima
	// do texto elas seriam sujeira, e nao efeito.
	letras := texto.Inset(-2)

	for _, f := range faiscas {
		a := 2 * math.Pi * (t*f.veloc + f.fase)
		px := cx + math.Cos(a)*f.raioX*largura
		py := cy + math.Sin(a*1.37+f.fase*6)*f.raioY

		alfa := float64(f.brilho)
		if hover {
			alfa *= 1.6 // ao passar o mouse o efeito se assume
		}
		// Desvanece ao entrar na area das letras, e some de vez no meio
		// delas - a transicao suave e o que evita a particula piscar.
		if p := image.Pt(int(px), int(py)); p.In(letras) {
			dx := math.Abs(px-cx) / (float64(texto.Dx()) / 2)
			alfa *= clamp(dx*dx, 0, 0.35)
		}
		if alfa < 8 {
			continue
		}
		if alfa > 235 {
			alfa = 235
		}
		c := tela.Alfa(tela.Titulo, uint8(alfa))
		p := ptf(float32(px), float32(py))
		circulo(gtx, p, 2.2, tela.Alfa(c, uint8(alfa/5)))
		circulo(gtx, p, 1.0, c)
	}
}

// marcaBrilhante desenha o nome com uma luz varrendo as letras.
//
// O Gio nao aplica gradiente a glifo, entao o degrade sai por recorte: o
// texto e redesenhado em fatias verticais, cada uma com o alfa de uma
// gaussiana centrada na posicao da varredura. Fora da passagem da luz o
// custo e zero - as fatias sem alfa nem chegam a ser desenhadas.
func (j *Janela) marcaBrilhante(gtx C, w int, base color.NRGBA, hover bool) {
	j.TextoGlow(gtx, 0, 0, marcaTexto, base, 11, false)

	f := float64(time.Now().UnixNano()%int64(cicloBrilho)) / float64(cicloBrilho)
	// A luz percorre de -0,25 a 1,25 na primeira metade do ciclo e some na
	// segunda: e a pausa que separa uma passada da outra.
	if f > 0.55 && !hover {
		return
	}
	centro := (-0.25 + 1.5*clamp(f/0.55, 0, 1)) * float64(w)

	// Largura da luz em pixels, e o quanto ela clareia no auge.
	const larguraLuz = 26.0
	brilho := color.NRGBA{R: 235, G: 245, B: 255, A: 255}

	passo := float64(w) / fatiasBrilho
	for i := 0; i < fatiasBrilho; i++ {
		x0 := float64(i) * passo
		d := (x0 + passo/2 - centro) / larguraLuz
		env := math.Exp(-d * d * 2.2)
		if env < 0.04 {
			continue
		}
		a := env * 235
		if hover {
			a = env * 255
		}
		func() {
			r := image.Rect(int(x0), 0, int(x0+passo)+1, 16)
			defer clip.Rect(r).Push(gtx.Ops).Pop()
			j.Texto(gtx, 0, 0, marcaTexto, tela.Alfa(brilho, uint8(a)), 11, false)
		}()
	}
}
