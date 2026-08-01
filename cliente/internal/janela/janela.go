package janela

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/font/gofont"
	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"sysmon-cliente/internal/nucleo"
)

// Medidas da tela. Densidade e o requisito principal de um painel de
// monitoramento: cabe mais host na mesma altura, e comparar de relance
// depende de tudo estar visivel junto.
const (
	AltLinha  = 21
	AltScope  = 46
	AltCabec  = 38
	AltRodape = 28
	Margem    = 10
	ColNome   = 190
	ColValor  = 230

	// Tamanho minimo, num lugar so. Sem moldura do sistema, o gerenciador de
	// janelas nao impoe limite nenhum: o arrasto do canto tem que respeitar.
	MinLarg = 470
	MinAlt  = 260

	// Piso da arvore. Abaixo disso a lista deixa de informar, e o grafico do
	// topo passa a ser o que sobra numa janela ilegivel.
	MinArvore = 92

	// Largura reservada aos icones do cabecalho, a direita. Sete botoes de
	// 24, mais o separador e a margem.
	LarguraBotoes = 7*24 + 10 + 20

	msAnim = 66 * time.Millisecond // ~15 quadros/s
)

// Janela e a interface do sysmon.
type Janela struct {
	frota     *nucleo.Frota
	caminho   string
	th        *material.Theme
	w         *app.Window
	Versao    string
	NaBandeja bool

	lista  widget.List
	oculto Visiveis
	noTopo bool

	btTopo, btBaixar, btAlerta, btExibir, btHosts widget.Clickable
	btMin, btFechar                               widget.Clickable
	arrastoCanto                                  gesture.Drag

	// Dialogos: sobreposicoes na propria janela, nao janelas do sistema.
	dialogo    qualDialogo
	dlgHosts   *dialogoHosts
	dlgExibir  *dialogoExibir
	dlgAlertas *dialogoAlertas

	mu        sync.Mutex
	linhas    []Linha
	resumo    string
	corResumo int
	alertas   []string
	dica      string

	// Historico curto por (host, medida) para os sparklines. Memoria de
	// janela aberta, nao telemetria: nada disso e persistido.
	hist             map[string][]float64
	ultimoTS         map[string]float64
	amostras         []float64 // cpu media da frota, para o osciloscopio
	ultimaAm         time.Time
	ultimaAssinatura float64
	nivelScope       int
	largSalva        int
	altSalva         int

	// Pedidos vindos de outra thread (bandeja, instancia unica).
	fila chan string
}

const janelaHist = 12

// Nova cria a janela. Nao a abre: quem abre e Rodar.
func Nova(f *nucleo.Frota, caminho string, versao string) *Janela {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	return &Janela{
		frota: f, caminho: caminho, th: th, Versao: versao,
		lista:    widget.List{List: layout.List{Axis: layout.Vertical}},
		oculto:   Visiveis{},
		hist:     map[string][]float64{},
		ultimoTS: map[string]float64{},
		ultimaAm: time.Now(),
		fila:     make(chan string, 8),
	}
}

// Pedir enfileira um comando de outra thread. A interface do Gio so pode ser
// tocada pela goroutine dela; a fila e a ponte.
func (j *Janela) Pedir(cmd string) {
	select {
	case j.fila <- cmd:
		if j.w != nil {
			j.w.Invalidate()
		}
	default:
	}
}

// Rodar abre a janela e so volta quando ela fecha.
func (j *Janela) Rodar(oculto bool) error {
	j.w = new(app.Window)
	j.w.Option(app.Title("sysmon"), app.Size(unit.Dp(820), unit.Dp(640)),
		app.MinSize(unit.Dp(MinLarg), unit.Dp(MinAlt)),
		app.Decorated(false))
	if oculto {
		j.w.Perform(system.ActionMinimize)
	}

	j.carregarEstado()
	if j.largSalva >= MinLarg && j.altSalva >= MinAlt {
		j.w.Option(app.Size(unit.Dp(j.largSalva), unit.Dp(j.altSalva)))
	}
	j.coletar()
	go j.laco()

	var ops op.Ops
	for {
		switch e := j.w.Event().(type) {
		case app.DestroyEvent:
			j.salvarEstado()
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			j.drenar()
			j.desenhar(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

// laco atualiza os dados e pede quadro novo.
//
// Sao dois ritmos: o dado muda no intervalo da frota, a animacao precisa de
// ~15 quadros por segundo. Redesenhar tudo a 15Hz e barato; recoletar a 15Hz
// nao seria.
func (j *Janela) laco() {
	tique := time.NewTicker(msAnim)
	defer tique.Stop()
	ultimaColeta := time.Now()
	intervalo := time.Duration(j.frota.Cfg().Intervalo * float64(time.Second))
	if intervalo < 2*time.Second {
		intervalo = 2 * time.Second
	}
	for range tique.C {
		if time.Since(ultimaColeta) >= intervalo {
			j.coletar()
			ultimaColeta = time.Now()
		}
		if j.w != nil {
			j.w.Invalidate()
		}
	}
}

// coletar transforma o estado da frota no que a tela desenha.
func (j *Janela) coletar() {
	cfg := j.frota.Cfg()
	leituras := j.frota.Estados()

	var cpus []float64
	pior := nucleo.OK
	assinatura := 0.0
	for _, l := range leituras {
		if n, _ := nucleo.Avaliar(l.Estado, cfg.Limiares); n > pior {
			pior = n
		}
		d := l.Estado.Dados
		if d == nil {
			continue
		}
		assinatura += d.TS
		if d.CPUPercent != nil {
			cpus = append(cpus, *d.CPUPercent)
		}
		j.anotar(l.Host.Nome, d.TS, map[string]*float64{
			"cpu": d.CPUPercent, "ram": d.Mem.Percent, "temp": d.CPUTemp,
		})
	}

	linhas := Montar(leituras, cfg.Limiares, j.oculto, j.serie)
	alertas := j.frota.Alertas()

	n := len(cfg.Hosts)
	partes := []string{fmt.Sprintf("%d host", n)}
	if n != 1 {
		partes[0] += "s"
	}
	offline := 0
	for _, l := range leituras {
		if l.Estado.Dados == nil {
			offline++
		}
	}
	if offline > 0 {
		partes = append(partes, fmt.Sprintf("%d offline", offline))
	}
	if len(alertas) > 0 {
		s := fmt.Sprintf("%d alerta", len(alertas))
		if len(alertas) != 1 {
			s += "s"
		}
		partes = append(partes, s)
	}

	j.mu.Lock()
	j.linhas = linhas
	j.alertas = alertas
	j.resumo = juntar(partes)
	j.corResumo = pior
	j.empilharAmostra(cpus, pior, assinatura)
	j.mu.Unlock()
}

// anotar guarda uma amostra por COLETA, nao por redesenho.
//
// A tela redesenha a 15Hz e a frota coleta no ritmo dela; sem esta guarda o
// sparkline repetiria o mesmo valor e mentiria sobre o tempo.
func (j *Janela) anotar(host string, ts float64, medidas map[string]*float64) {
	if j.ultimoTS[host] == ts {
		return
	}
	j.ultimoTS[host] = ts
	for nome, v := range medidas {
		if v == nil {
			continue
		}
		k := host + ":" + nome
		s := append(j.hist[k], *v)
		if len(s) > janelaHist {
			s = s[len(s)-janelaHist:]
		}
		j.hist[k] = s
	}
}

func (j *Janela) serie(host, medida string) []float64 {
	return j.hist[host+":"+medida]
}

func (j *Janela) empilharAmostra(cpus []float64, nivel int, assinatura float64) {
	j.nivelScope = nivel
	if assinatura == j.ultimaAssinatura {
		return
	}
	j.ultimaAssinatura = assinatura
	media := 0.0
	for _, v := range cpus {
		media += v
	}
	if len(cpus) > 0 {
		media /= float64(len(cpus))
	}
	j.amostras = append(j.amostras, media)
	if len(j.amostras) > 200 {
		j.amostras = j.amostras[len(j.amostras)-200:]
	}
	j.ultimaAm = time.Now()
}

func (j *Janela) drenar() {
	for {
		select {
		case cmd := <-j.fila:
			switch cmd {
			case "mostrar":
				j.w.Perform(system.ActionUnmaximize)
				j.w.Perform(system.ActionRaise)
			case "atualizar":
				j.frota.AtualizarAgora()
			case "topo":
				j.alternarTopo()
			case "sair":
				j.NaBandeja = false
				os.Exit(0)
			}
		default:
			return
		}
	}
}

func (j *Janela) alternarTopo() {
	j.noTopo = !j.noTopo
	// Gio ainda nao expoe sempre-no-topo por opcao de janela em todas as
	// plataformas; onde nao houver, o botao acende sem efeito e isso e
	// melhor que sumir com ele.
}

// tratarCliques roda ANTES de desenhar, e a ordem nao e detalhe.
//
// Clickable.Layout drena os cliques pendentes logo no inicio. Verificando
// depois de desenhar, o clique ja foi consumido e o botao parece morto -
// hover funciona, clique nao. E o idioma do Gio: perguntar antes, desenhar
// depois.
func (j *Janela) tratarCliques(gtx C) {
	if j.dialogo != semDialogo {
		// Com dialogo aberto, o cabecalho fica atras da cortina: aceitar
		// clique nele deixaria abrir dois dialogos ao mesmo tempo.
		j.tratarDialogo(gtx)
		return
	}
	if j.btFechar.Clicked(gtx) {
		if j.NaBandeja {
			j.w.Perform(system.ActionMinimize)
		} else {
			os.Exit(0)
		}
	}
	if j.btMin.Clicked(gtx) {
		j.w.Perform(system.ActionMinimize)
	}
	if j.btTopo.Clicked(gtx) {
		j.alternarTopo()
	}
	if j.btHosts.Clicked(gtx) {
		j.abrirHosts()
	}
	if j.btExibir.Clicked(gtx) {
		j.abrirExibir()
	}
	if j.btAlerta.Clicked(gtx) {
		j.abrirAlertas()
	}
}

func (j *Janela) tratarDialogo(gtx C) {
	switch j.dialogo {
	case dlgHosts:
		j.tratarHosts(gtx)
	case dlgExibir:
		j.tratarExibir(gtx)
	case dlgAlertas:
		j.tratarAlertas(gtx)
	}
}

// ------------------------------------------------------------------ desenho
func (j *Janela) desenhar(gtx C) {
	j.tratarCliques(gtx)

	retangulo(gtx, image.Rectangle{Max: gtx.Constraints.Max}, Fundo)
	larg, alt := gtx.Constraints.Max.X, gtx.Constraints.Max.Y

	j.mu.Lock()
	linhas := j.linhas
	alertas := j.alertas
	resumo, corResumo := j.resumo, j.corResumo
	j.mu.Unlock()

	// O cabecalho arrasta a janela: e o que substitui a barra de titulo do
	// sistema, que dispensamos.
	//
	// A area PARA antes dos botoes. O ActionInputOp e tratado pela camada da
	// plataforma, e nao pelo roteador de eventos: declarado por cima dos
	// botoes, ele engole o press e nenhum icone do cabecalho responde ao
	// clique - sintoma que so aparece clicando, nunca lendo o codigo.
	func() {
		defer clip.Rect{Max: image.Pt(larg-LarguraBotoes, AltCabec)}.
			Push(gtx.Ops).Pop()
		system.ActionInputOp(system.ActionMove).Add(gtx.Ops)
	}()

	j.cabecalho(gtx, larg, resumo, corResumo)

	y := AltCabec
	// O grafico se recolhe sozinho quando a janela fica curta demais para
	// ele e a lista. Enfeite nao espreme informacao.
	altAlertas := 0
	if len(alertas) > 0 {
		altAlertas = 6 + AltLinha*min(len(alertas), 4)
		if len(alertas) > 4 {
			altAlertas += AltLinha
		}
	}
	sobra := alt - AltCabec - AltRodape - altAlertas
	mostrarScope := j.ver("sec:TELA", "c:scope") && sobra-AltScope >= MinArvore
	if mostrarScope {
		j.osciloscopio(gtx, image.Rect(Margem, y+4, larg-Margem, y+4+AltScope))
		y += AltScope + 6
	}

	fimLista := alt - AltRodape - altAlertas
	j.tabela(gtx, linhas, image.Rect(0, y, larg, fimLista))

	if altAlertas > 0 {
		j.painelAlertas(gtx, alertas, image.Rect(0, fimLista, larg, alt-AltRodape))
	}
	j.rodape(gtx, image.Rect(0, alt-AltRodape, larg, alt))
	j.cantoRedimensionar(gtx, larg, alt)

	// Dialogo por ultimo: em immediate-mode quem desenha depois fica por
	// cima, e a cortina precisa cobrir a frota inteira.
	switch j.dialogo {
	case dlgHosts:
		j.desenharHosts(gtx)
	case dlgExibir:
		j.desenharExibir(gtx)
	case dlgAlertas:
		j.desenharAlertas(gtx)
	}
}

func (j *Janela) ver(chaves ...string) bool { return j.oculto.ver(chaves...) }

func (j *Janela) cabecalho(gtx C, larg int, resumo string, nivel int) {
	j.TextoGlow(gtx, Margem, 9, "sysmon", Titulo, 15, true)
	marca := j.Medir(gtx, "sysmon", 15, true)
	j.Texto(gtx, Margem+marca+10, 11, resumo, CorNivel(nivel), 12, false)

	// Da direita para a esquerda, na mesma ordem da versao Tkinter.
	x := larg - Margem - 24
	j.botao(gtx, x, 8, &j.btFechar, icFechar, Vermelho)
	x -= 24
	j.botao(gtx, x, 8, &j.btMin, icMinimizar, Texto)
	x -= 10
	retangulo(gtx, image.Rect(x+4, 12, x+5, 26), Grade)
	x -= 20
	j.botao(gtx, x, 8, &j.btHosts, icHosts, Texto)
	x -= 24
	j.botao(gtx, x, 8, &j.btExibir, icExibir, Texto)
	x -= 24
	j.botao(gtx, x, 8, &j.btAlerta, icAlerta, Texto)
	x -= 24
	j.botao(gtx, x, 8, &j.btBaixar, icBaixar, Texto)
	x -= 24
	j.botaoLigado(gtx, x, 8, &j.btTopo, icTopo, j.noTopo)

}

func (j *Janela) botao(gtx C, x, y int, c *widget.Clickable,
	ic func(C, float32, float32, color.NRGBA), corHover color.NRGBA) {
	j.desenhaBotao(gtx, x, y, c, ic, corHover, false)
}

func (j *Janela) botaoLigado(gtx C, x, y int, c *widget.Clickable,
	ic func(C, float32, float32, color.NRGBA), ligado bool) {
	j.desenhaBotao(gtx, x, y, c, ic, Texto, ligado)
}

func (j *Janela) desenhaBotao(gtx C, x, y int, c *widget.Clickable,
	ic func(C, float32, float32, color.NRGBA), corHover color.NRGBA, ligado bool) {
	defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints = layout.Exact(image.Pt(24, 22))
	c.Layout(g, func(g C) D {
		cor := Fraco
		if ligado {
			cor = Titulo
		}
		if c.Hovered() {
			retangulo(g, image.Rect(1, 1, 23, 21), Grade)
			if !ligado {
				cor = corHover
			}
		}
		ic(g, 12, 11, cor)
		return D{Size: image.Pt(24, 22)}
	})
}

func (j *Janela) osciloscopio(gtx C, r image.Rectangle) {
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	base, teto := float32(r.Max.Y-7), float32(r.Min.Y+7)
	const passo = 18

	j.mu.Lock()
	amostras := append([]float64(nil), j.amostras...)
	nivel := j.nivelScope
	desde := time.Since(j.ultimaAm).Seconds()
	j.mu.Unlock()

	intervalo := math.Max(j.frota.Cfg().Intervalo, 0.5)
	// O deslize entre coletas e interpolacao do EIXO, nao do dado: a curva
	// anda porque o tempo anda, e nao porque alguem inventou pontos.
	desloca := float32(passo * clamp(desde/intervalo, 0, 1))

	corGrade := Alfa(Grade, 150)
	for x := float32(r.Min.X) - desloca; x < float32(r.Max.X); x += passo * 2 {
		polilinha(gtx, []f32.Point{{X: x, Y: teto - 3}, {X: x, Y: base + 3}},
			corGrade, 1)
	}

	cor := CorNivel(nivel)
	if nivel == nucleo.OK {
		cor = Ativo // cyan: "esta vivo", nao "esta em alerta"
	}

	if len(amostras) < 2 {
		j.Texto(gtx, r.Min.X+4, r.Min.Y+8, "aguardando leitura", Fraco, 12, false)
		return
	}
	cabem := r.Dx()/passo + 3
	if len(amostras) > cabem {
		amostras = amostras[len(amostras)-cabem:]
	}
	pts := make([]f32.Point, len(amostras))
	for i, v := range amostras {
		x := float32(r.Max.X) - float32(len(amostras)-1-i)*passo - desloca
		y := base - (base-teto)*float32(clamp(v, 0, 100))/100
		pts[i] = f32.Pt(x, y)
	}
	glow(gtx, pts, cor)

	p := pts[len(pts)-1]
	for _, c := range []struct {
		r float32
		a uint8
	}{{7, 45}, {4, 110}, {2, 255}} {
		circulo(gtx, p, c.r, Alfa(cor, c.a))
	}

	// Tarja atras do numero: a curva passa por cima do canto direito e o
	// halo do texto sozinho nao vence uma linha acesa cruzando a leitura.
	txt := "cpu " + fmtPct(amostras[len(amostras)-1])
	w := j.Medir(gtx, txt, 12, true)
	retangulo(gtx, image.Rect(r.Max.X-w-14, r.Min.Y-1, r.Max.X, r.Min.Y+18), Fundo)
	j.TextoGlow(gtx, r.Max.X-w-6, r.Min.Y, txt, cor, 12, true)
}

func (j *Janela) tabela(gtx C, linhas []Linha, r image.Rectangle) {
	defer op.Offset(image.Pt(r.Min.X, r.Min.Y)).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints = layout.Exact(image.Pt(r.Dx(), r.Dy()))
	larg := r.Dx()

	material.List(j.th, &j.lista).Layout(g, len(linhas), func(g C, i int) D {
		j.linha(g, linhas[i], larg)
		return D{Size: image.Pt(larg, AltLinha)}
	})
}

func (j *Janela) linha(gtx C, l Linha, larg int) {
	xNome := Margem + l.Recuo*30
	xDet := Margem + ColNome
	xValor := larg - Margem

	if l.Host {
		// A linha do host pinta inteira: um host critico fica vermelho de
		// ponta a ponta, e nao so no nome.
		retangulo(gtx, image.Rect(0, 0, larg, AltLinha), Selecao)
		j.Texto(gtx, Margem, 3, l.Nome, l.Cor, 12, true)
		j.Texto(gtx, xDet, 3, l.Detalhe, Titulo, 12, true)
		j.TextoDir(gtx, xValor, 3, l.Valor, l.Cor, 12, true)
		return
	}
	if l.Secao {
		j.Texto(gtx, xNome+30, 3, l.Nome, Fraco, 12, true)
		return
	}

	j.Texto(gtx, xNome+40, 3, l.Nome, l.Cor, 12, false)
	j.Texto(gtx, xDet, 3, l.Detalhe, Fraco, 12, false)

	// Coluna do valor: sparkline, barra e numero, colados na direita.
	xFim := xValor - j.Medir(gtx, l.Valor, 12, false) - 8
	if l.Pct >= 0 {
		bx := xFim - 100
		if bx > xDet+20 {
			barra(gtx, image.Rect(bx, 6, bx+96, 15), l.Pct, l.Cor)
			xFim = bx - 8
		}
	}
	if len(l.Serie) > 1 {
		sx := xFim - 70
		if sx > xDet+20 {
			sparkline(gtx, l.Serie, image.Rect(sx, 4, sx+66, 17), l.Cor)
		}
	}
	j.TextoDir(gtx, xValor, 3, l.Valor, l.Cor, 12, false)
}

func (j *Janela) painelAlertas(gtx C, alertas []string, r image.Rectangle) {
	defer op.Offset(image.Pt(0, r.Min.Y)).Push(gtx.Ops).Pop()
	y := 4
	for i, a := range alertas {
		if i >= 4 {
			j.Texto(gtx, Margem, y, fmt.Sprintf("  + %d outros", len(alertas)-4),
				Vermelho, 12, false)
			break
		}
		j.Texto(gtx, Margem, y, "! "+a, Vermelho, 12, false)
		y += AltLinha
	}
}

func (j *Janela) rodape(gtx C, r image.Rectangle) {
	retangulo(gtx, r, Fundo)
	j.mu.Lock()
	dica := j.dica
	j.mu.Unlock()
	texto := dica
	if texto == "" {
		if len(j.frota.Cfg().Hosts) == 0 {
			texto = "sem hosts · use o icone de servidores para configurar"
		} else {
			texto = fmt.Sprintf("atualiza %.0fs · F5 forca · arraste pelo topo",
				j.frota.Cfg().Intervalo)
		}
	}
	j.Texto(gtx, Margem, r.Min.Y+5, texto, Fraco, 12, false)
}

// cantoRedimensionar desenha a alca e trata o arrasto.
//
// O Gio expoe ActionMove para arrastar a janela, mas nao tem equivalente
// para redimensionar: sem moldura do sistema, a janela simplesmente nao
// mudaria de tamanho. Entao o canto e nosso.
//
// O truque que mantem isso estavel: a area de arrasto e declarada em
// coordenadas da JANELA, sem deslocamento. Assim a posicao do ponteiro ja e
// o tamanho novo, e o calculo nao acumula erro conforme a janela cresce -
// que e o defeito classico de somar deltas quadro a quadro.
func (j *Janela) cantoRedimensionar(gtx C, larg, alt int) {
	r := image.Rect(larg-18, alt-18, larg, alt)
	area := clip.Rect(r).Push(gtx.Ops)
	j.arrastoCanto.Add(gtx.Ops)
	pointer.CursorSouthEastResize.Add(gtx.Ops)
	area.Pop()

	for {
		ev, ok := j.arrastoCanto.Update(gtx.Metric, gtx.Source, gesture.Both)
		if !ok {
			break
		}
		if ev.Kind != pointer.Drag {
			continue
		}
		w := math.Max(float64(ev.Position.X)+8, MinLarg)
		h := math.Max(float64(ev.Position.Y)+8, MinAlt)
		ppd := float64(gtx.Metric.PxPerDp)
		if ppd <= 0 {
			ppd = 1
		}
		j.largSalva, j.altSalva = int(w/ppd), int(h/ppd)
		j.w.Option(app.Size(unit.Dp(w/ppd), unit.Dp(h/ppd)))
	}

	cor := Fraco
	if j.arrastoCanto.Pressed() {
		cor = Texto
	}
	tracos(gtx, cor, f32.Pt(float32(larg-4), float32(alt-12)),
		f32.Pt(float32(larg-12), float32(alt-4)))
	tracos(gtx, cor, f32.Pt(float32(larg-4), float32(alt-7)),
		f32.Pt(float32(larg-7), float32(alt-4)))
}

func fmtPct(v float64) string { return fmt.Sprintf("%.0f%%", v) }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
