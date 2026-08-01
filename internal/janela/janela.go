package janela

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/font/gofont"
	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"sysmon/internal/atualizar"
	"sysmon/internal/nucleo"
	"sysmon/internal/tela"
)

// Medidas da tela. Densidade e o requisito principal de um painel de
// monitoramento: cabe mais host na mesma altura, e comparar de relance
// depende de tudo estar visivel junto.
const (
	AltLinha = 21
	// A linha do host e a que se le primeiro: e nela que se decide se vale
	// olhar o resto. Com a mesma altura e o mesmo corpo das medidas, a tela
	// inteira ficava plana e a hierarquia sumia. Dois pontos a mais no texto
	// e cinco pixels na linha bastam - mais que isso comeca a custar host
	// visivel na mesma altura de janela, que e o requisito principal aqui.
	AltLinhaHost = 26
	CorpoHost    = 14

	// Altura padrao do grafico do topo. E um preset, escolhido no menu
	// exibir: ver AlturasScope.
	AltScope  = 46
	AltCabec  = 38
	AltRodape = 28
	Margem    = 10
	// Onde a coluna do meio comeca, medida da margem da janela.
	//
	// Eram 190, herdados de quando os nomes das medidas ficavam recuados em
	// 110px: a coluna precisava desviar deles. Com o recuo agora saindo da
	// margem escolhida, o nome mais longo ("temperatura") termina bem antes,
	// e os 190 viravam uma faixa vazia no meio da tela - cara numa janela de
	// 470, que e como esta ferramenta e usada.
	ColNome = 155

	// Tamanho minimo, num lugar so. Sem moldura do sistema, o gerenciador de
	// janelas nao impoe limite nenhum: o arrasto do canto tem que respeitar.
	MinLarg = 470
	MinAlt  = 260

	// Piso da arvore. Abaixo disso a lista deixa de informar, e o grafico do
	// topo passa a ser o que sobra numa janela ilegivel.
	MinArvore = 92

	// Largura reservada aos icones do cabecalho, a direita. Oito botoes de
	// 24, mais o separador e a margem.
	LarguraBotoes = 8*24 + 10 + 20

	msAnim = 66 * time.Millisecond // ~15 quadros/s

	// A alca de redimensionar, no canto inferior direito. Quem desenha perto
	// dela precisa saber o tamanho para nao ficar por baixo.
	larguraCanto = 18

	// Reservado a direita dentro de uma lista rolavel: a material.List
	// recorta na propria largura e desenha a barra de rolagem ali.
	MargemLista = 12

	// Quanto uma medida recua em relacao a secao dela. So o suficiente para
	// a hierarquia se ler; mais que isso e espaco vazio numa janela estreita.
	RecuoMedida = 22
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
	oculto tela.Visiveis
	noTopo bool

	btTopo, btBaixar, btAlerta, btExibir, btHosts widget.Clickable
	btReconhecer                                  widget.Clickable
	btMin, btFechar, btMarca                      widget.Clickable
	btScopeHost, btScopeMedida                    widget.Clickable
	arrastoCanto                                  gesture.Drag

	// Dialogos: sobreposicoes na propria janela, nao janelas do sistema.
	dialogo       qualDialogo
	dlgHosts      *dialogoHosts
	dlgExibir     *dialogoExibir
	dlgAlertas    *dialogoAlertas
	dlgReconhecer *dialogoReconhecer

	mu        sync.Mutex
	linhas    []tela.Linha
	resumo    string
	corResumo int
	alertas   []nucleo.Alerta
	dica      string

	// Dica do botao sob o cursor, colhida ao desenhar o cabecalho e desenhada
	// no fim do quadro - senao a arvore, que vem depois, passaria por cima.
	dicaBotao  string
	dicaBotaoX int

	// Historico curto por (host, medida) para os sparklines. Memoria de
	// janela aberta, nao telemetria: nada disso e persistido.
	hist     map[string][]float64
	ultimoTS map[string]float64
	// O grafico do topo acompanha UM host e UMA medida.
	//
	// Ate a v5.1 ele era a media de cpu da frota. Media de hosts diferentes
	// nao mede coisa nenhuma: dois servidores a 10% e a 90% viram 50%, que
	// nao descreve nem um nem outro. Agora e uma evidencia so, escolhida.
	scopeHost   string // "" = o primeiro host com dados
	scopeMedida string // cpu | ram | temp
	scopeAlt    string // baixo | medio | alto | cheio
	margemEsq   int    // respiro a esquerda das escritas

	// Hosts recolhidos, por nome, e o clicavel de cada cabecalho. Com dez
	// hosts a arvore nao cabe na tela, e rolar para comparar dois derrota o
	// proposito de ter tudo visivel junto.
	recolhidos       map[string]bool
	cliqueHost       map[string]*widget.Clickable
	ultimaAm         time.Time
	ultimaAssinatura float64
	nivelScope       int
	hostsScope       []string // os que tem dados, para o clique circular
	largSalva        int
	altSalva         int

	// Pedidos vindos de outra thread (bandeja, instancia unica).
	fila chan string

	// Ligacao com a bandeja. A janela nao conhece Win32: ela so informa o
	// nivel e recebe pedidos pela fila, como qualquer outra origem.
	AoMudarNivel func(nivel int, dica string)
	AoAlertar    func(titulo, texto string)
	nivelAvisado int

	// Atualizacao do proprio programa. Nil quando desligada.
	Atual        *atualizar.Atualizador
	updatePedido bool
}

const (
	// O sparkline mostra a tendencia recente; o osciloscopio do topo mostra
	// uma janela longa. Sao a mesma serie, cortada em dois tamanhos - antes o
	// topo mantinha um historico proprio, paralelo a este.
	janelaHist = 12
	maxHist    = 200
)

// Nova cria a janela. Nao a abre: quem abre e Rodar.
func Nova(f *nucleo.Frota, caminho string, versao string) *Janela {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	return &Janela{
		frota: f, caminho: caminho, th: th, Versao: versao,
		lista:      widget.List{List: layout.List{Axis: layout.Vertical}},
		oculto:     tela.Visiveis{},
		hist:       map[string][]float64{},
		ultimoTS:   map[string]float64{},
		recolhidos: map[string]bool{},
		cliqueHost: map[string]*widget.Clickable{},
		ultimaAm:   time.Now(),
		fila:       make(chan string, 8),
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
	// O estado guarda o sempre-no-topo, mas ate a v5.1 ninguem o aplicava na
	// abertura: o botao voltava aceso e a janela voltava por baixo das outras.
	if j.noTopo {
		j.w.Option(app.TopMost(true))
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
		j.anotar(l.Host.Nome, d.TS, map[string]*float64{
			"cpu": d.CPUPercent, "ram": d.Mem.Percent, "temp": d.CPUTemp,
		})
	}

	linhas := tela.Montar(leituras, cfg.Limiares, j.oculto, j.serie)
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
	j.resumo = tela.Juntar(partes)
	j.corResumo = pior
	j.hostsScope = nomesComDados(leituras)
	j.nivelScope = nivelDoScope(leituras, j.alvoScope(leituras), cfg.Limiares)
	if assinatura != j.ultimaAssinatura {
		j.ultimaAssinatura = assinatura
		j.ultimaAm = time.Now()
	}
	j.mu.Unlock()

	if j.AoMudarNivel != nil {
		j.AoMudarNivel(pior, dicaBandeja(n, offline, len(alertas)))
	}
	// Balao so na SUBIDA de severidade. Avisar a cada coleta seria ruido, e
	// avisar na descida ("voltou ao normal") interrompe sem pedir acao.
	if j.AoAlertar != nil && pior > j.nivelAvisado && pior >= nucleo.Aviso {
		titulo := "sysmon: atencao"
		if pior == nucleo.Critico {
			titulo = "sysmon: critico"
		}
		texto := "sem detalhes"
		if len(alertas) > 0 {
			texto = alertas[0].Texto
			if len(alertas) > 1 {
				texto = fmt.Sprintf("%s (e mais %d)", alertas[0].Texto,
					len(alertas)-1)
			}
		}
		j.AoAlertar(titulo, texto)
	}
	j.nivelAvisado = pior
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
		if len(s) > maxHist {
			s = s[len(s)-maxHist:]
		}
		j.hist[k] = s
	}
}

// serie e o rabinho que cabe ao lado do valor, na linha da medida.
func (j *Janela) serie(host, medida string) []float64 {
	s := j.hist[host+":"+medida]
	if len(s) > janelaHist {
		s = s[len(s)-janelaHist:]
	}
	return s
}

// serieLonga e a mesma serie inteira, para o grafico do topo.
func (j *Janela) serieLonga(host, medida string) []float64 {
	return j.hist[host+":"+medida]
}

// MedidasScope sao as tres que cabem no eixo de 0 a 100 do grafico do topo.
//
// Nao e uma limitacao de desenho e sim o que faz o grafico ser comparavel:
// tres medidas na mesma escala percentual, e a temperatura em graus, que na
// pratica tambem vive entre 0 e 100.
var MedidasScope = []struct{ Chave, Rotulo string }{
	{"cpu", "cpu"},
	{"ram", "memoria"},
	{"temp", "temperatura"},
}

func nomesComDados(leituras []nucleo.LeituraHost) []string {
	var out []string
	for _, l := range leituras {
		if l.Estado.Dados != nil {
			out = append(out, l.Host.Nome)
		}
	}
	return out
}

// alvoScope resolve qual host o grafico acompanha.
//
// Vazio, ou apontando para host que saiu do config, cai no primeiro que
// tenha dados - o grafico nunca fica em branco por causa de uma preferencia
// velha.
func (j *Janela) alvoScope(leituras []nucleo.LeituraHost) string {
	nomes := j.hostsScope
	if leituras != nil {
		nomes = nomesComDados(leituras)
	}
	for _, n := range nomes {
		if n == j.scopeHost {
			return n
		}
	}
	if len(nomes) > 0 {
		return nomes[0]
	}
	return ""
}

func (j *Janela) medidaScope() string {
	for _, m := range MedidasScope {
		if m.Chave == j.scopeMedida {
			return m.Chave
		}
	}
	return MedidasScope[0].Chave
}

func rotuloMedida(chave string) string {
	for _, m := range MedidasScope {
		if m.Chave == chave {
			return m.Rotulo
		}
	}
	return chave
}

// nivelDoScope pinta a curva pelo estado do host que ela mostra, e nao pelo
// pior da frota: a cor tem que falar do que esta desenhado ali.
func nivelDoScope(leituras []nucleo.LeituraHost, host string, lim nucleo.Limiares) int {
	for _, l := range leituras {
		if l.Host.Nome == host {
			n, _ := nucleo.Avaliar(l.Estado, lim)
			return n
		}
	}
	return nucleo.OK
}

// proximo devolve o item seguinte de uma lista, circulando.
func proximo(lista []string, atual string) string {
	for i, v := range lista {
		if v == atual {
			return lista[(i+1)%len(lista)]
		}
	}
	if len(lista) > 0 {
		return lista[0]
	}
	return atual
}

// dicaBandeja evita que o pacote da janela importe o da bandeja so por uma
// string - a dependencia correria no sentido errado.
func dicaBandeja(hosts, offline, alertas int) string {
	s := fmt.Sprintf("sysmon · %d host", hosts)
	if hosts != 1 {
		s += "s"
	}
	if offline > 0 {
		s += fmt.Sprintf(" · %d offline", offline)
	}
	if alertas > 0 {
		s += fmt.Sprintf(" · %d alerta", alertas)
		if alertas > 1 {
			s += "s"
		}
	}
	if offline == 0 && alertas == 0 {
		s += " · sem alertas"
	}
	return s
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
	// Ate a v5.1 isto so virava o booleano: o botao acendia e a janela nao
	// subia. O app.TopMost existe desde o Gio v0.9 e vale para Windows e
	// macOS; no X11 e no Wayland ele nao faz nada, e ali o botao continua
	// sendo so um indicador.
	if j.w != nil {
		j.w.Option(app.TopMost(j.noTopo))
	}
	j.salvarEstado()
}

// tratarEscape fecha o dialogo com a tecla ESC.
//
// Modal que so fecha por um botao especifico e uma armadilha em janela
// estreita, que e onde os botoes ficam mais apertados - justamente quando
// mais se quer sair. ESC e o gesto que todo mundo tenta primeiro.
func (j *Janela) tratarEscape(gtx C) {
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
		if !ok {
			return
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			j.dialogo = semDialogo
		}
	}
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
		j.tratarEscape(gtx)
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
	if j.btReconhecer.Clicked(gtx) {
		j.abrirReconhecer()
	}
	if j.btMarca.Clicked(gtx) {
		AbrirURL(marcaURL)
	}
	for nome, c := range j.cliqueHost {
		if c.Clicked(gtx) {
			if j.recolhidos[nome] {
				delete(j.recolhidos, nome)
			} else {
				j.recolhidos[nome] = true
			}
			j.salvarEstado()
		}
	}
	if j.btScopeHost.Clicked(gtx) {
		j.mu.Lock()
		j.scopeHost = proximo(j.hostsScope, j.alvoScope(nil))
		j.mu.Unlock()
		j.salvarEstado()
	}
	if j.btScopeMedida.Clicked(gtx) {
		chaves := make([]string, len(MedidasScope))
		for i, m := range MedidasScope {
			chaves[i] = m.Chave
		}
		j.scopeMedida = proximo(chaves, j.medidaScope())
		j.salvarEstado()
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
	if j.btBaixar.Clicked(gtx) {
		j.acaoUpdate()
	}
}

// acaoUpdate: um botao com dois papeis - procurar quando nao ha nada,
// aplicar quando ha. Dois botoes seriam um a mais, com o de aplicar passando
// a vida inteira sem ter o que fazer.
func (j *Janela) acaoUpdate() {
	if j.Atual == nil {
		return
	}
	if j.Atual.Estado().Pronta {
		j.aplicarUpdate()
		return
	}
	j.updatePedido = true // erro so aparece se voce perguntou
	j.Atual.VerificarEmThread(func() {
		if j.w != nil {
			j.w.Invalidate()
		}
	})
}

// aplicarUpdate troca o binario e sobe a versao nova no lugar desta.
func (j *Janela) aplicarUpdate() {
	exe, err := j.Atual.Aplicar()
	if err != nil {
		j.mu.Lock()
		j.dica = "nao consegui aplicar: " + err.Error()
		j.mu.Unlock()
		return
	}
	j.salvarEstado()
	// O processo novo sobe antes de este sair: assim a bandeja nao pisca
	// vazia, e se o exec falhar ainda estamos aqui para contar.
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		j.mu.Lock()
		j.dica = "troquei o binario, mas nao consegui reiniciar: " + err.Error()
		j.mu.Unlock()
		return
	}
	j.NaBandeja = false
	os.Exit(0)
}

// textoUpdate e a linha de status sobre atualizacao.
func (j *Janela) textoUpdate() string {
	if j.Atual == nil {
		return ""
	}
	e := j.Atual.Estado()
	if !e.Suportado {
		return ""
	}
	switch {
	case e.Pronta:
		return "atualizacao " + e.Disponivel + " pronta · clique em ⭳"
	case e.Checando:
		return "procurando atualizacao..."
	case e.Erro != "" && j.updatePedido:
		return "atualizacao: " + e.Erro
	case j.updatePedido:
		j.updatePedido = false
		return "ja esta na versao mais nova"
	}
	return ""
}

func (j *Janela) tratarDialogo(gtx C) {
	switch j.dialogo {
	case dlgHosts:
		j.tratarHosts(gtx)
	case dlgExibir:
		j.tratarExibir(gtx)
	case dlgAlertas:
		j.tratarAlertas(gtx)
	case dlgReconhecer:
		j.tratarReconhecer(gtx)
	}
}

// ------------------------------------------------------------------ desenho
func (j *Janela) desenhar(gtx C) {
	j.tratarCliques(gtx)

	retangulo(gtx, image.Rectangle{Max: gtx.Constraints.Max}, tela.Fundo)
	larg, alt := gtx.Constraints.Max.X, gtx.Constraints.Max.Y
	j.dicaBotao = "" // recolhida de novo pelo cabecalho, se houver hover

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
	altScope := j.alturaScope()
	mostrarScope := j.Ver("sec:TELA", "c:scope") && sobra-altScope >= MinArvore
	if mostrarScope {
		j.osciloscopio(gtx, image.Rect(Margem, y+4, larg-Margem, y+4+altScope))
		y += altScope + 6
	}

	fimLista := alt - AltRodape - altAlertas
	// Recolhido no DESENHO, e nao na coleta: assim o clique responde no
	// quadro seguinte, e nao no proximo ciclo de rede.
	//
	// A margem NAO desloca a tabela inteira: a linha do host e a ancora
	// visual da frota - o fio de estado dela marca onde cada bloco comeca - e
	// afastar isso da borda so estreita a tela. A margem vale para o que esta
	// DENTRO do bloco, que e onde as escritas encostavam.
	j.tabela(gtx, tela.Recolher(linhas, j.recolhidos),
		image.Rect(0, y, larg, fimLista))

	if altAlertas > 0 {
		j.painelAlertas(gtx, alertas, image.Rect(0, fimLista, larg, alt-AltRodape))
	}
	j.rodape(gtx, image.Rect(0, alt-AltRodape, larg, alt))
	j.cantoRedimensionar(gtx, larg, alt)
	j.dicaDoBotao(gtx, larg)

	// A janela pede o foco do teclado enquanto ha dialogo aberto, senao o
	// ESC nunca chega ate aqui.
	if j.dialogo != semDialogo {
		area := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
		event.Op(gtx.Ops, j)
		area.Pop()
	}

	// Dialogo por ultimo: em immediate-mode quem desenha depois fica por
	// cima, e a cortina precisa cobrir a frota inteira.
	switch j.dialogo {
	case dlgHosts:
		j.desenharHosts(gtx)
	case dlgExibir:
		j.desenharExibir(gtx)
	case dlgAlertas:
		j.desenharAlertas(gtx)
	case dlgReconhecer:
		j.desenharReconhecer(gtx)
	}
}

func (j *Janela) Ver(chaves ...string) bool { return j.oculto.Ver(chaves...) }

func (j *Janela) cabecalho(gtx C, larg int, resumo string, nivel int) {
	j.TextoGlow(gtx, Margem, 9, "sysmon", tela.Titulo, 15, true)
	marca := j.Medir(gtx, "sysmon", 15, true)
	j.Texto(gtx, Margem+marca+10, 11, resumo, tela.CorNivel(nivel), 12, false)

	// Da direita para a esquerda, na mesma ordem da versao Tkinter.
	x := larg - Margem - 24
	j.botao(gtx, x, 8, &j.btFechar, icFechar, tela.Vermelho, textoFechar(j.NaBandeja))
	x -= 24
	j.botao(gtx, x, 8, &j.btMin, icMinimizar, tela.Texto, "minimizar")
	x -= 10
	retangulo(gtx, image.Rect(x+4, 12, x+5, 26), tela.Grade)
	x -= 20
	j.botao(gtx, x, 8, &j.btHosts, icHosts, tela.Texto, "hosts monitorados")
	x -= 24
	j.botao(gtx, x, 8, &j.btExibir, icExibir, tela.Texto, "escolher o que aparece")
	x -= 24
	j.botao(gtx, x, 8, &j.btAlerta, icAlerta, tela.Texto, "limiares de alerta")
	x -= 24
	j.botaoLigado(gtx, x, 8, &j.btReconhecer, icReconhecer,
		len(j.frota.Cfg().Limiares.Reconhecidos) > 0, textoReconhecer(j))
	x -= 24
	// tela.Verde quando ha versao pronta: azul ja quer dizer "ligado" no botao de
	// sempre-no-topo, e isto aqui e outra coisa.
	if j.Atual != nil && j.Atual.Estado().Pronta {
		j.desenhaBotao(gtx, x, 8, &j.btBaixar, icBaixar, tela.Verde, true,
			"instalar a versao "+j.Atual.Estado().Versao+" e reiniciar")
	} else {
		j.botao(gtx, x, 8, &j.btBaixar, icBaixar, tela.Texto, "procurar atualizacao")
	}
	x -= 24
	j.botaoLigado(gtx, x, 8, &j.btTopo, icTopo, j.noTopo, textoTopo(j.noTopo))
}

// textoReconhecer diz na dica quantos alertas estao aceitos. O botao fica
// aceso quando ha algum: silencio precisa ser visivel, senao vira esquecimento.
func textoReconhecer(j *Janela) string {
	if s := j.resumoReconhecidos(); s != "" {
		return "alertas e notificacoes · " + s
	}
	return "alertas e notificacoes"
}

func textoFechar(naBandeja bool) string {
	if naBandeja {
		return "fechar para a bandeja"
	}
	return "sair"
}

func textoTopo(ligado bool) string {
	if ligado {
		return "sempre no topo: ligado"
	}
	return "sempre no topo"
}

func (j *Janela) botao(gtx C, x, y int, c *widget.Clickable,
	ic func(C, float32, float32, color.NRGBA), corHover color.NRGBA, dica string) {
	j.desenhaBotao(gtx, x, y, c, ic, corHover, false, dica)
}

func (j *Janela) botaoLigado(gtx C, x, y int, c *widget.Clickable,
	ic func(C, float32, float32, color.NRGBA), ligado bool, dica string) {
	j.desenhaBotao(gtx, x, y, c, ic, tela.Titulo, ligado, dica)
}

func (j *Janela) desenhaBotao(gtx C, x, y int, c *widget.Clickable,
	ic func(C, float32, float32, color.NRGBA), corHover color.NRGBA,
	ligado bool, dica string) {
	defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints = layout.Exact(image.Pt(24, 22))
	c.Layout(g, func(g C) D {
		cor := tela.Fraco
		if ligado {
			cor = corHover // ligado usa a cor passada: azul no topo, verde no update
		}
		if c.Hovered() {
			retangulo(g, image.Rect(1, 1, 23, 21), tela.Grade)
			if !ligado {
				cor = corHover
			}
			// So anota: desenhar aqui deixaria a dica atras da arvore.
			j.dicaBotao, j.dicaBotaoX = dica, x
		}
		ic(g, 12, 11, cor)
		return D{Size: image.Pt(24, 22)}
	})
}

// dicaDoBotao desenha o balao do icone sob o cursor.
//
// Sete icones sem rotulo e um enigma novo a cada release. A versao Tkinter
// tinha essa dica e ela se perdeu na migracao para o Gio - onde nao ha
// tooltip pronto, entao e um retangulo e um texto.
func (j *Janela) dicaDoBotao(gtx C, larg int) {
	if j.dicaBotao == "" {
		return
	}
	const alt = 20
	w := j.Medir(gtx, j.dicaBotao, 11, false) + 14
	// Ancorada no botao, mas sem sair pela borda da janela.
	x := j.dicaBotaoX + 12 - w/2
	if x+w > larg-4 {
		x = larg - 4 - w
	}
	if x < 4 {
		x = 4
	}
	y := AltCabec - 2
	r := image.Rect(x, y, x+w, y+alt)
	retangulo(gtx, image.Rect(r.Min.X-1, r.Min.Y-1, r.Max.X+1, r.Max.Y+1), tela.Titulo)
	retangulo(gtx, r, tela.Painel)
	j.Texto(gtx, x+7, y+4, j.dicaBotao, tela.Texto, 11, false)
}

func (j *Janela) osciloscopio(gtx C, r image.Rectangle) {
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	base, teto := float32(r.Max.Y-7), float32(r.Min.Y+7)
	const passo = 18

	j.mu.Lock()
	host := j.alvoScope(nil)
	if host == "" || !contem(j.hostsScope, host) {
		if len(j.hostsScope) > 0 {
			host = j.hostsScope[0]
		}
	}
	medida := j.medidaScope()
	amostras := append([]float64(nil), j.serieLonga(host, medida)...)
	nivel := j.nivelScope
	desde := time.Since(j.ultimaAm).Seconds()
	j.mu.Unlock()

	intervalo := math.Max(j.frota.Cfg().Intervalo, 0.5)
	// O deslize entre coletas e interpolacao do EIXO, nao do dado: a curva
	// anda porque o tempo anda, e nao porque alguem inventou pontos.
	desloca := float32(passo * clamp(desde/intervalo, 0, 1))

	corGrade := tela.Alfa(tela.Grade, 150)
	for x := float32(r.Min.X) - desloca; x < float32(r.Max.X); x += passo * 2 {
		polilinha(gtx, []f32.Point{{X: x, Y: teto - 3}, {X: x, Y: base + 3}},
			corGrade, 1)
	}

	cor := tela.CorNivel(nivel)
	if nivel == nucleo.OK {
		cor = tela.Ativo // cyan: "esta vivo", nao "esta em alerta"
	}

	if len(amostras) >= 2 {
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
			circulo(gtx, p, c.r, tela.Alfa(cor, c.a))
		}

		// Tarja atras do numero: a curva passa por cima do canto direito e o
		// halo do texto sozinho nao vence uma linha acesa cruzando a leitura.
		txt := valorScope(medida, amostras[len(amostras)-1])
		w := j.Medir(gtx, txt, 12, true)
		retangulo(gtx, image.Rect(r.Max.X-w-14, r.Min.Y-1, r.Max.X, r.Min.Y+18),
			tela.Fundo)
		j.TextoGlow(gtx, r.Max.X-w-6, r.Min.Y, txt, cor, 12, true)
	} else {
		j.Texto(gtx, r.Min.X+4, r.Min.Y+8, "aguardando leitura", tela.Fraco, 12, false)
	}

	j.seletoresScope(gtx, r, host, medida)
}

// seletoresScope desenha as duas etiquetas clicaveis que dizem - e escolhem -
// o que a curva mostra.
//
// Ficam por cima da curva, num canto: sem elas, "12%" no grafico e um numero
// sem referencia, e nao havia como saber de qual host nem de que medida.
func (j *Janela) seletoresScope(gtx C, r image.Rectangle, host, medida string) {
	if host == "" {
		return
	}
	x := r.Min.X + 4
	x = j.etiquetaScope(gtx, &j.btScopeHost, x, r.Min.Y-2, host, len(j.hostsScope) > 1)
	j.etiquetaScope(gtx, &j.btScopeMedida, x+6, r.Min.Y-2, rotuloMedida(medida), true)
}

func (j *Janela) etiquetaScope(gtx C, c *widget.Clickable, x, y int,
	txt string, clicavel bool) int {
	w := j.Medir(gtx, txt, 11, false)
	if !clicavel {
		// Um host so, ou uma medida so: continua sendo legenda, e nao um
		// botao que nao leva a lugar nenhum.
		j.Texto(gtx, x, y, txt, tela.Alfa(tela.Fraco, 170), 11, false)
		return x + w
	}
	defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w+10, 16))
	c.Layout(g, func(g C) D {
		cor := tela.Alfa(tela.Fraco, 170)
		if c.Hovered() {
			cor = tela.Titulo
			pointer.CursorPointer.Add(g.Ops)
			retangulo(g, image.Rect(0, 1, w+8, 15), tela.Alfa(tela.Grade, 200))
		}
		j.Texto(g, 3, 0, txt, cor, 11, false)
		// A setinha diz que da para trocar, sem gastar uma linha de texto.
		tracos(g, tela.Alfa(cor, 200), ptf(float32(w)+3, 6), ptf(float32(w)+5.5, 9),
			ptf(float32(w)+8, 6))
		return D{Size: image.Pt(w+10, 16)}
	})
	return x + w + 10
}

// valorScope formata a leitura atual na unidade da medida - por cento nas
// duas primeiras, graus na terceira.
func valorScope(medida string, v float64) string {
	if medida == "temp" {
		return fmt.Sprintf("%.0fC", v)
	}
	return fmtPct(v)
}

func contem(lista []string, v string) bool {
	for _, x := range lista {
		if x == v {
			return true
		}
	}
	return false
}

// detalhe desenha a coluna do meio, em cor unica ou repartida.
//
// Repartir importa quando a cor separa duas coisas em vez de indicar
// gravidade - as duas janelas da carga sao o caso: o problema ali nao e saber
// se esta ruim, e sim qual numero e qual.
func (j *Janela) detalhe(gtx C, x, fim int, l tela.Linha) {
	espaco := fim - x
	if espaco <= 0 {
		return // a coluna da direita comeu tudo: melhor nada que sobreposto
	}
	if len(l.Partes) == 0 {
		j.Texto(gtx, x, 3, j.cortarPara(gtx, l.Detalhe, espaco), tela.Fraco,
			12, false)
		return
	}
	// Repartido em cores, ou nada: cortar no meio de uma parte deixaria "5m
	// 0.9" em ciano seguido de nada, que se le como um numero truncado. Se
	// nao cabe inteiro, cai para a versao de uma cor so, que corta bem.
	total := 0
	for _, p := range l.Partes {
		total += j.Medir(gtx, p.Texto, 12, false)
	}
	if total > espaco {
		j.Texto(gtx, x, 3, j.cortarPara(gtx, l.Detalhe, espaco), tela.Fraco,
			12, false)
		return
	}
	for _, p := range l.Partes {
		j.Texto(gtx, x, 3, p.Texto, p.Cor, 12, false)
		x += j.Medir(gtx, p.Texto, 12, false)
	}
}

func (j *Janela) tabela(gtx C, linhas []tela.Linha, r image.Rectangle) {
	defer op.Offset(image.Pt(r.Min.X, r.Min.Y)).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints = layout.Exact(image.Pt(r.Dx(), r.Dy()))
	larg := r.Dx()

	material.List(j.th, &j.lista).Layout(g, len(linhas), func(g C, i int) D {
		j.linha(g, linhas[i], larg)
		return D{Size: image.Pt(larg, alturaLinha(linhas[i]))}
	})
}

// clicavelHost devolve (criando na primeira vez) o alvo de clique de um host.
// Guardado por nome porque o widget do Gio precisa sobreviver entre quadros
// para reconhecer press e release como um clique.
func (j *Janela) clicavelHost(nome string) *widget.Clickable {
	c, ok := j.cliqueHost[nome]
	if !ok {
		c = &widget.Clickable{}
		j.cliqueHost[nome] = c
	}
	return c
}

// alturaLinha existe porque a linha do host e mais alta que as demais - a
// lista do Gio aceita altura por item, entao a hierarquia nao custa nada.
func alturaLinha(l tela.Linha) int {
	if l.Host {
		return AltLinhaHost
	}
	return AltLinha
}

func (j *Janela) linha(gtx C, l tela.Linha, larg int) {
	// O recuo sai da margem escolhida, e nao de multiplos de 30 a partir de
	// uma margem fixa. Antes, DESEMPENHO comecava em 70px e "cpu" em 110px -
	// um deslocamento herdado da arvore com raiz, que aqui nao existe: o host
	// e a linha inteira acima, e nao um no a esquerda. Aqueles 110px eram
	// espaco vazio numa janela de 470.
	xSecao := j.margemEsq
	xMedida := j.margemEsq + RecuoMedida
	xDet := Margem + ColNome
	xValor := larg - Margem

	if l.Host {
		// A linha inteira e o alvo do clique que recolhe: um host tem nome
		// curto, e mirar em quatro letras seria pior que mirar na linha.
		c := j.clicavelHost(l.Nome)
		g := gtx
		g.Constraints = layout.Exact(image.Pt(larg, AltLinhaHost))
		c.Layout(g, func(g C) D { return D{Size: image.Pt(larg, AltLinhaHost)} })
		if c.Hovered() {
			pointer.CursorPointer.Add(gtx.Ops)
		}

		// A linha do host pinta inteira: um host critico fica vermelho de
		// ponta a ponta, e nao so no nome.
		fundo := tela.Selecao
		if c.Hovered() {
			fundo = tela.Grade
		}
		retangulo(gtx, image.Rect(0, 0, larg, AltLinhaHost), fundo)
		// Um fio da cor do estado na borda esquerda: com varios hosts, e o
		// que separa os blocos sem gastar uma linha em branco entre eles.
		retangulo(gtx, image.Rect(0, 0, 3, AltLinhaHost), l.Cor)
		icSeta(gtx, float32(Margem+5), AltLinhaHost/2, l.Recolhido,
			tela.Alfa(l.Cor, 200))
		xNomeHost := Margem + 14
		larguraNome := j.Medir(gtx, l.Nome, CorpoHost, true)
		j.TextoGlow(gtx, xNomeHost, 5, l.Nome, l.Cor, CorpoHost, true)
		// A coluna do meio comeca logo APOS o nome, e nao na coluna fixa das
		// medidas. Numa janela estreita aqueles 190px de recuo eram espaco
		// vazio: o nome do host tem tres letras, e o que vem depois - modelo
		// do processador, memoria, sistema - ficava sem lugar.
		xDet = xNomeHost + larguraNome + 20
		// A coluna do meio para ANTES do valor. Sem esse limite, numa janela
		// estreita - que e como esta ferramenta e usada, encostada na lateral
		// como um widget - o "9.1G de 14.9G · Debian" passava por cima do
		// "57C · cpu 24% · ram 63%" e as duas ficavam ilegiveis.
		fim := xValor - j.Medir(gtx, l.Valor, CorpoHost, true) - 12
		// Um ponto menor que o resto: no cabecalho ele e contexto - modelo
		// do processador, memoria, sistema -, e nao a medida que se compara.
		j.Texto(gtx, xDet, 7, j.cortarPara(gtx, l.Detalhe, fim-xDet),
			tela.Titulo, 11, true)
		j.TextoDir(gtx, xValor, 5, l.Valor, l.Cor, CorpoHost, true)
		return
	}
	if l.Secao {
		j.Texto(gtx, xSecao, 3, l.Nome, tela.Fraco, 12, true)
		return
	}

	j.Texto(gtx, xMedida, 3, l.Nome, l.Cor, 12, false)

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
			xFim = sx - 8
		}
	}
	// O detalhe entra por ultimo porque so agora se sabe onde a coluna da
	// direita comecou: com barra e sparkline desenhados, o espaco que sobra
	// e xFim - xDet, e e nele que "8 nucleos · Intel Core i7-9700K" tem que
	// caber ou ser cortado.
	j.detalhe(gtx, xDet, xFim-8, l)
	j.TextoDir(gtx, xValor, 3, l.Valor, l.Cor, 12, false)
}

func (j *Janela) painelAlertas(gtx C, alertas []nucleo.Alerta, r image.Rectangle) {
	defer op.Offset(image.Pt(0, r.Min.Y)).Push(gtx.Ops).Pop()
	// Cortado na largura: as frases dos achados de SMART sao longas de
	// proposito - elas dizem o que fazer -, e numa janela estreita seguiam
	// desenhando por cima da borda da janela.
	espaco := r.Dx() - 2*Margem
	y := 4
	for i, a := range alertas {
		if i >= 4 {
			j.Texto(gtx, Margem, y, fmt.Sprintf("  + %d outros", len(alertas)-4),
				tela.Vermelho, 12, false)
			break
		}
		cor := tela.Vermelho
		if a.Nivel == nucleo.Aviso {
			cor = tela.Ambar
		}
		j.Texto(gtx, Margem, y, j.cortarPara(gtx, "! "+a.Texto, espaco), cor,
			12, false)
		y += AltLinha
	}
}

func (j *Janela) rodape(gtx C, r image.Rectangle) {
	retangulo(gtx, r, tela.Fundo)
	j.mu.Lock()
	dica := j.dica
	j.mu.Unlock()
	texto := dica
	if texto == "" {
		if len(j.frota.Cfg().Hosts) == 0 {
			texto = "sem hosts · use o icone de servidores para configurar"
		} else {
			texto = fmt.Sprintf("atualiza %.0fs · arraste pelo topo",
				j.frota.Cfg().Intervalo)
		}
		// A informacao de atualizacao entra AO LADO, nao no lugar: a dica de
		// uso continua valendo enquanto houver versao nova esperando.
		if u := j.textoUpdate(); u != "" {
			texto += " · " + u
		}
		// Silencio precisa ser visivel. Um alerta aceito nao aparece em lugar
		// nenhum - e sem esta linha, seis meses depois ninguem lembra que
		// aceitou, e a ferramenta parece estar dizendo que esta tudo bem.
		if s := j.resumoReconhecidos(); s != "" {
			texto += " · " + s
		}
	}
	// A assinatura fica na direita e o texto para antes dela: sem esse limite,
	// uma dica comprida passaria por baixo do link e os dois ficariam
	// ilegiveis.
	fim := j.marca(gtx, r)
	if l := j.Medir(gtx, texto, 12, false); Margem+l > fim-10 {
		texto = j.cortarPara(gtx, texto, fim-10-Margem)
	}
	j.Texto(gtx, Margem, r.Min.Y+5, texto, tela.Fraco, 12, false)
}

// cortarPara encurta ate caber, com reticencias.
//
// A primeira versao entrava no laco sem conferir se o texto ja cabia: toda
// frase perdia o ultimo caractere e ganhava reticencias, mesmo sobrando meia
// tela. "disco /backup em 96%" virava "disco /backup em 96…", o que faz a
// interface parecer estar escondendo alguma coisa quando nao esta.
func (j *Janela) cortarPara(gtx C, s string, larg int) string {
	if larg <= 0 {
		return ""
	}
	if j.Medir(gtx, s, 12, false) <= larg {
		return s
	}
	r := []rune(s)
	for len(r) > 1 {
		r = r[:len(r)-1]
		if j.Medir(gtx, string(r)+"…", 12, false) <= larg {
			return string(r) + "…"
		}
	}
	return ""
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
	r := image.Rect(larg-larguraCanto, alt-larguraCanto, larg, alt)
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

	cor := tela.Fraco
	if j.arrastoCanto.Pressed() {
		cor = tela.Texto
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

// NoTopo diz se o modo sempre-no-topo esta ligado. Usado pelo menu da
// bandeja para desenhar a marca de selecao.
func (j *Janela) NoTopo() bool { return j.noTopo }
