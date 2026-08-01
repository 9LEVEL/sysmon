package janela

import (
	"fmt"
	"image"
	"sort"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"sysmon/internal/nucleo"
	"sysmon/internal/tela"
)

// Os dialogos sao sobreposicoes na propria janela, e nao janelas do sistema.
//
// Duas razoes. A janela nao tem moldura, entao uma segunda janela apareceria
// com a barra de titulo do sistema e destoaria. E abrir janela nova em cima
// de outra, com foco e sempre-no-topo em jogo, e uma das partes mais
// irregulares entre plataformas - trocar isso por um retangulo desenhado
// elimina uma classe inteira de bug que so aparece na maquina do usuario.

type qualDialogo int

const (
	semDialogo qualDialogo = iota
	dlgHosts
	dlgExibir
	dlgAlertas
)

// ---------------------------------------------------------------- hosts
type linhaHost struct {
	nome, url, token *Campo
	testar           *Botao
	remover          *Botao
	resultado        string
	ok               bool
}

type dialogoHosts struct {
	linhas    []*linhaHost
	lista     widget.List
	adicionar *Botao
	salvar    *Botao
	cancelar  *Botao
	erro      string
}

func (j *Janela) abrirHosts() {
	d := &dialogoHosts{
		lista:     widget.List{List: layout.List{Axis: layout.Vertical}},
		adicionar: NovoBotao("+ HOST", tela.Texto),
		salvar:    NovoBotao("SALVAR", tela.Verde),
		cancelar:  NovoBotao("CANCELAR", tela.Fraco),
	}
	for _, h := range j.frota.Cfg().Hosts {
		d.linhas = append(d.linhas, novaLinhaHost(h.Nome, h.URL, h.Token))
	}
	if len(d.linhas) == 0 {
		d.linhas = append(d.linhas, novaLinhaHost("", "", ""))
	}
	j.dlgHosts = d
	j.dialogo = dlgHosts
}

func novaLinhaHost(nome, url, token string) *linhaHost {
	l := &linhaHost{
		nome:    NovoCampo("apelido", 110),
		url:     NovoCampo("http://ip:9109/metrics", 300),
		token:   NovoCampo("token", 170),
		testar:  NovoBotao("TESTAR", tela.Titulo),
		remover: NovoBotao("×", tela.Vermelho),
	}
	l.nome.Definir(nome)
	l.url.Definir(url)
	l.token.Definir(token)
	return l
}

func (j *Janela) desenharHosts(gtx C) {
	d := j.dlgHosts
	j.cortina(gtx)
	r := centrado(gtx, 780, 460)
	j.moldura(gtx, r, "hosts")

	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	larg, alt := r.Dx(), r.Dy()

	j.Texto(gtx, 16, 44, "a url e o token sao os que o install.sh imprimiu em "+
		"cada host", tela.Fraco, 12, false)

	// Lista rolavel: com muitos hosts o dialogo nao pode crescer alem da
	// janela, e cortar o botao de salvar seria pior que rolar.
	corpo := image.Rect(16, 68, larg-16, alt-56)
	func() {
		defer op.Offset(corpo.Min).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Exact(image.Pt(corpo.Dx(), corpo.Dy()))
		material.List(j.th, &d.lista).Layout(g, len(d.linhas),
			func(g C, i int) D {
				return j.linhaHostLayout(g, d, i, corpo.Dx())
			})
	}()

	// Rodape com as acoes.
	//
	// Os tres botoes tem largura de texto e o dialogo encolhe junto com a
	// janela: numa janela estreita eles se sobrepunham, e o de adicionar
	// ficava por baixo do de cancelar. Quando nao cabem lado a lado, o de
	// adicionar sobe uma linha em vez de disputar o mesmo espaco.
	y := alt - 42
	wAdicionar := j.larguraBotao(gtx, d.adicionar)
	wSalvar := j.larguraBotao(gtx, d.salvar)
	wCancelar := j.larguraBotao(gtx, d.cancelar)

	xCancelar := larg - 24 - wSalvar - wCancelar
	yAdicionar := y
	if 16+wAdicionar+12 > xCancelar {
		yAdicionar = y - 30
	}
	func() {
		defer op.Offset(image.Pt(16, yAdicionar)).Push(gtx.Ops).Pop()
		d.adicionar.Layout(gtx, j)
	}()
	if d.erro != "" {
		// O erro ocupa o que sobra entre adicionar e cancelar, e e cortado
		// para nao invadir os botoes - texto de erro por baixo de botao nao
		// e nem erro nem botao.
		xErro := 16 + wAdicionar + 12
		if yAdicionar != y {
			xErro = 16
		}
		if sobra := xCancelar - 8 - xErro; sobra > 40 {
			j.Texto(gtx, xErro, y+6, j.cortarPara(gtx, d.erro, sobra),
				tela.Vermelho, 12, false)
		}
	}
	func() {
		defer op.Offset(image.Pt(larg-16-wSalvar, y)).Push(gtx.Ops).Pop()
		d.salvar.Layout(gtx, j)
	}()
	func() {
		defer op.Offset(image.Pt(xCancelar, y)).Push(gtx.Ops).Pop()
		d.cancelar.Layout(gtx, j)
	}()
}

// larguraBotao repete a conta que Botao.Layout faz para se medir. Sao os
// mesmos 22 de padding; o rodape precisa saber antes de desenhar, para
// decidir se os tres cabem na mesma linha.
func (j *Janela) larguraBotao(gtx C, b *Botao) int {
	return j.Medir(gtx, b.Rotulo, 12, true) + 22
}

// tratarHosts roda antes do desenho - ver Janela.tratarCliques.
func (j *Janela) tratarHosts(gtx C) {
	d := j.dlgHosts
	if d == nil {
		return
	}
	if d.adicionar.Clicado(gtx) {
		d.linhas = append(d.linhas, novaLinhaHost("", "", ""))
	}
	if d.cancelar.Clicado(gtx) {
		j.dialogo = semDialogo
	}
	if d.salvar.Clicado(gtx) {
		j.salvarHosts(d)
	}
	for i, l := range d.linhas {
		if l.remover.Clicado(gtx) {
			d.linhas = append(d.linhas[:i], d.linhas[i+1:]...)
			break
		}
		if l.testar.Clicado(gtx) {
			j.testar(l)
		}
	}
}

func (j *Janela) linhaHostLayout(gtx C, d *dialogoHosts, i, larg int) D {
	l := d.linhas[i]
	const alt = 58
	x := 0
	desenha := func(w func(C) D, larguraFixa int) {
		defer op.Offset(image.Pt(x, 0)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints.Max.X = larguraFixa
		w(g)
		x += larguraFixa + 8
	}
	nome, url, token := larguraCampos(larg,
		j.larguraBotao(gtx, l.testar), j.larguraBotao(gtx, l.remover))
	desenha(func(g C) D { return l.nome.Layout(g, j) }, nome)
	desenha(func(g C) D { return l.url.Layout(g, j) }, url)
	desenha(func(g C) D { return l.token.Layout(g, j) }, token)
	desenha(func(g C) D { return l.testar.Layout(g, j) }, j.larguraBotao(gtx, l.testar))
	desenha(func(g C) D { return l.remover.Layout(g, j) }, j.larguraBotao(gtx, l.remover))

	if l.resultado != "" {
		cor := tela.Vermelho
		if l.ok {
			cor = tela.Verde
		}
		j.Texto(gtx, 2, 30, l.resultado, cor, 12, false)
	}
	return D{Size: image.Pt(larg, alt)}
}

// larguraCampos reparte a largura disponivel entre apelido, url e token.
//
// As tres larguras eram fixas (110, 300, 170). Somadas aos dois botoes e aos
// espacos passavam de 700 px, e o dialogo encolhe junto com a janela: abaixo
// disso o TESTAR e o × saiam pela borda, cortados ou por cima do token.
//
// Os botoes nao encolhem - o texto deles nao cabe menor - entao o que sobra
// e repartido na mesma proporcao de antes, com um minimo por campo para que
// nenhum vire uma fresta onde nao da para ler nada.
func larguraCampos(larg, wTestar, wRemover int) (nome, url, token int) {
	const (
		pNome, pURL, pToken       = 110, 300, 170
		minNome, minURL, minToken = 60, 110, 70
		espacos                   = 4 * 8
	)
	sobra := larg - wTestar - wRemover - espacos
	total := pNome + pURL + pToken
	if sobra >= total {
		// Cabe o desenho original; o que exceder vai para a url, que e o
		// campo onde texto comprido de fato aparece.
		return pNome, pURL + (sobra - total), pToken
	}
	if sobra < minNome+minURL+minToken {
		return minNome, minURL, minToken
	}
	nome = max(minNome, sobra*pNome/total)
	token = max(minToken, sobra*pToken/total)
	url = max(minURL, sobra-nome-token)
	return nome, url, token
}

// testar consulta o host numa goroutine: a interface nao pode congelar por
// quatro segundos de timeout enquanto o usuario espera resposta.
func (j *Janela) testar(l *linhaHost) {
	url, token := l.url.Texto(), l.token.Texto()
	l.resultado, l.ok = "testando...", false
	go func() {
		ok, msg := nucleo.TestarHost(url, token, 4*time.Second)
		l.ok, l.resultado = ok, msg
		if j.w != nil {
			j.w.Invalidate()
		}
	}()
}

func (j *Janela) salvarHosts(d *dialogoHosts) {
	cfg := j.frota.Cfg()
	// Token em branco mantem o que ja havia: mudar um apelido nao deve
	// exigir redigitar o segredo.
	antigos := map[string]string{}
	for _, h := range cfg.Hosts {
		antigos[h.Nome] = h.Token
	}

	var hosts []any
	for _, l := range d.linhas {
		url := strings.TrimSpace(l.url.Texto())
		if url == "" {
			continue
		}
		nome := strings.TrimSpace(l.nome.Texto())
		token := l.token.Texto()
		if token == "" {
			token = antigos[nome]
		}
		hosts = append(hosts, map[string]any{
			"nome": nome, "url": url, "token": token,
		})
	}

	bruto := cfg.ComoBruto()
	bruto["hosts"] = hosts
	// Valida ANTES de gravar: um erro de digitacao nao pode destruir o
	// arquivo que estava funcionando.
	nova, err := nucleo.ConfigDe(bruto)
	if err != nil {
		d.erro = err.Error()
		return
	}
	if err := nucleo.SalvarConfig(j.caminho, bruto); err != nil {
		d.erro = "nao consegui gravar: " + err.Error()
		return
	}
	j.frota.Trocar(nova)
	j.dialogo = semDialogo
	j.coletar()
}

// ---------------------------------------------------------------- exibir
type dialogoExibir struct {
	secoes   []*Caixa
	itens    [][]*Caixa
	lista    widget.List
	tudo     *Botao
	nada     *Botao
	aplicar  *Botao
	cancelar *Botao
}

func (j *Janela) abrirExibir() {
	d := &dialogoExibir{
		lista:    widget.List{List: layout.List{Axis: layout.Vertical}},
		tudo:     NovoBotao("TUDO", tela.Texto),
		nada:     NovoBotao("NADA", tela.Texto),
		aplicar:  NovoBotao("APLICAR", tela.Verde),
		cancelar: NovoBotao("CANCELAR", tela.Fraco),
	}
	for _, s := range tela.Catalogo {
		d.secoes = append(d.secoes, NovaCaixa(s.Nome, !j.oculto["sec:"+s.Nome]))
		var itens []*Caixa
		for _, i := range s.Itens {
			itens = append(itens, NovaCaixa(i.Rotulo, !j.oculto[i.Chave]))
		}
		d.itens = append(d.itens, itens)
	}
	j.dlgExibir = d
	j.dialogo = dlgExibir
}

func (j *Janela) desenharExibir(gtx C) {
	d := j.dlgExibir
	j.cortina(gtx)
	r := centrado(gtx, 560, 520)
	j.moldura(gtx, r, "exibir")
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	larg, alt := r.Dx(), r.Dy()

	j.Texto(gtx, 16, 44, "desmarque o que nao quer ver", tela.Fraco, 12, false)

	// Uma lista plana com secoes e itens: cada entrada sabe a qual secao
	// pertence, o que evita aninhar listas rolaveis dentro de listas.
	type entrada struct{ secao, item int }
	var plana []entrada
	for si := range tela.Catalogo {
		plana = append(plana, entrada{si, -1})
		for ii := range tela.Catalogo[si].Itens {
			plana = append(plana, entrada{si, ii})
		}
	}

	corpo := image.Rect(16, 68, larg-16, alt-56)
	func() {
		defer op.Offset(corpo.Min).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Exact(image.Pt(corpo.Dx(), corpo.Dy()))
		material.List(j.th, &d.lista).Layout(g, len(plana), func(g C, i int) D {
			e := plana[i]
			if e.item < 0 {
				g.Constraints.Max.X = corpo.Dx()
				dm := d.secoes[e.secao].Layout(g, j, tela.Titulo, true)
				if nota := tela.Catalogo[e.secao].Nota; nota != "" {
					j.Texto(g, 22+j.Medir(g, tela.Catalogo[e.secao].Nome, 12, true)+10,
						2, nota, tela.Fraco, 12, false)
				}
				return dm
			}
			defer op.Offset(image.Pt(26, 0)).Push(g.Ops).Pop()
			g.Constraints.Max.X = corpo.Dx() - 26
			return d.itens[e.secao][e.item].Layout(g, j, tela.Texto, false)
		})
	}()

	y := alt - 42
	pos := 16
	for _, b := range []*Botao{d.tudo, d.nada} {
		func(b *Botao, x int) {
			defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
			b.Layout(gtx, j)
		}(b, pos)
		pos += j.Medir(gtx, b.Rotulo, 12, true) + 30
	}
	lSalvar := j.Medir(gtx, d.aplicar.Rotulo, 12, true) + 22
	lCancel := j.Medir(gtx, d.cancelar.Rotulo, 12, true) + 22
	func() {
		defer op.Offset(image.Pt(larg-16-lSalvar, y)).Push(gtx.Ops).Pop()
		d.aplicar.Layout(gtx, j)
	}()
	func() {
		defer op.Offset(image.Pt(larg-24-lSalvar-lCancel, y)).Push(gtx.Ops).Pop()
		d.cancelar.Layout(gtx, j)
	}()

}

func (j *Janela) tratarExibir(gtx C) {
	d := j.dlgExibir
	if d == nil {
		return
	}
	if d.tudo.Clicado(gtx) {
		j.marcarTudo(d, true)
	}
	if d.nada.Clicado(gtx) {
		j.marcarTudo(d, false)
	}
	if d.cancelar.Clicado(gtx) {
		j.dialogo = semDialogo
	}
	if d.aplicar.Clicado(gtx) {
		j.aplicarExibir(d)
	}
}

func (j *Janela) marcarTudo(d *dialogoExibir, v bool) {
	for si := range d.secoes {
		d.secoes[si].Marcar(v)
		for ii := range d.itens[si] {
			d.itens[si][ii].Marcar(v)
		}
	}
}

func (j *Janela) aplicarExibir(d *dialogoExibir) {
	oculto := tela.Visiveis{}
	for si, s := range tela.Catalogo {
		if !d.secoes[si].Marcada() {
			oculto["sec:"+s.Nome] = true
		}
		for ii, it := range s.Itens {
			if !d.itens[si][ii].Marcada() {
				oculto[it.Chave] = true
			}
		}
	}
	j.oculto = oculto
	j.salvarEstado()
	j.dialogo = semDialogo
	j.coletar()
}

// ---------------------------------------------------------------- alertas
type dialogoAlertas struct {
	aviso, critico []*Campo
	mounts         *Campo
	lista          widget.List
	restaurar      *Botao
	salvar         *Botao
	cancelar       *Botao
	erro           string
}

func (j *Janela) abrirAlertas() {
	d := &dialogoAlertas{
		lista:     widget.List{List: layout.List{Axis: layout.Vertical}},
		restaurar: NovoBotao("PADROES", tela.Texto),
		salvar:    NovoBotao("SALVAR", tela.Verde),
		cancelar:  NovoBotao("CANCELAR", tela.Fraco),
		mounts:    NovoCampo("/boot, /boot/efi", 300),
	}
	lim := j.frota.Cfg().Limiares
	j.preencherAlertas(d, lim)
	d.mounts.Definir(strings.Join(lim.IgnorarMounts, ", "))
	j.dlgAlertas = d
	j.dialogo = dlgAlertas
}

func (j *Janela) preencherAlertas(d *dialogoAlertas, lim nucleo.Limiares) {
	d.aviso, d.critico = nil, nil
	for _, c := range nucleo.Campos {
		p := *c.Ler(&lim)
		ca := NovoCampo("aviso", 80)
		cc := NovoCampo("critico", 80)
		ca.Definir(fmtNum(p.Aviso))
		cc.Definir(fmtNum(p.Critico))
		d.aviso = append(d.aviso, ca)
		d.critico = append(d.critico, cc)
	}
}

func (j *Janela) desenharAlertas(gtx C) {
	d := j.dlgAlertas
	j.cortina(gtx)
	r := centrado(gtx, 640, 470)
	j.moldura(gtx, r, "limiares de alerta")
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	larg, alt := r.Dx(), r.Dy()

	j.Texto(gtx, 16, 44, "aviso e critico de cada medida", tela.Fraco, 12, false)

	corpo := image.Rect(16, 68, larg-16, alt-92)
	func() {
		defer op.Offset(corpo.Min).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Exact(image.Pt(corpo.Dx(), corpo.Dy()))
		material.List(j.th, &d.lista).Layout(g, len(nucleo.Campos),
			func(g C, i int) D {
				c := nucleo.Campos[i]
				j.Texto(g, 0, 6, c.Rotulo, tela.Texto, 12, false)
				func() {
					defer op.Offset(image.Pt(corpo.Dx()-180, 0)).Push(g.Ops).Pop()
					gg := g
					gg.Constraints.Max.X = 80
					d.aviso[i].Layout(gg, j)
				}()
				func() {
					defer op.Offset(image.Pt(corpo.Dx()-90, 0)).Push(g.Ops).Pop()
					gg := g
					gg.Constraints.Max.X = 80
					d.critico[i].Layout(gg, j)
				}()
				return D{Size: image.Pt(corpo.Dx(), 32)}
			})
	}()

	j.Texto(gtx, 16, alt-84, "ignorar filesystems (separados por virgula)",
		tela.Fraco, 12, false)
	func() {
		defer op.Offset(image.Pt(16, alt-70)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints.Max.X = 300
		d.mounts.Layout(g, j)
	}()

	y := alt - 38
	func() {
		defer op.Offset(image.Pt(16, y)).Push(gtx.Ops).Pop()
		d.restaurar.Layout(gtx, j)
	}()
	if d.erro != "" {
		j.Texto(gtx, 130, y+6, d.erro, tela.Vermelho, 12, false)
	}
	lSalvar := j.Medir(gtx, d.salvar.Rotulo, 12, true) + 22
	lCancel := j.Medir(gtx, d.cancelar.Rotulo, 12, true) + 22
	func() {
		defer op.Offset(image.Pt(larg-16-lSalvar, y)).Push(gtx.Ops).Pop()
		d.salvar.Layout(gtx, j)
	}()
	func() {
		defer op.Offset(image.Pt(larg-24-lSalvar-lCancel, y)).Push(gtx.Ops).Pop()
		d.cancelar.Layout(gtx, j)
	}()

}

func (j *Janela) tratarAlertas(gtx C) {
	d := j.dlgAlertas
	if d == nil {
		return
	}
	if d.restaurar.Clicado(gtx) {
		j.preencherAlertas(d, nucleo.LimiaresPadrao())
		d.mounts.Definir(strings.Join(nucleo.LimiaresPadrao().IgnorarMounts, ", "))
	}
	if d.cancelar.Clicado(gtx) {
		j.dialogo = semDialogo
	}
	if d.salvar.Clicado(gtx) {
		j.salvarAlertas(d)
	}
}

func (j *Janela) salvarAlertas(d *dialogoAlertas) {
	cfg := j.frota.Cfg()
	lim := cfg.Limiares
	alertas := map[string]any{}
	for i, c := range nucleo.Campos {
		a, err1 := strconv.ParseFloat(strings.TrimSpace(d.aviso[i].Texto()), 64)
		cr, err2 := strconv.ParseFloat(strings.TrimSpace(d.critico[i].Texto()), 64)
		if err1 != nil || err2 != nil {
			d.erro = "valor invalido em " + c.Nome
			return
		}
		// Aviso acima do critico nunca dispararia aviso: o critico venceria
		// sempre, e o campo pareceria ignorado.
		if a > cr {
			d.erro = c.Nome + ": aviso acima do critico"
			return
		}
		alertas[c.Nome] = []any{a, cr}
	}

	var mounts []any
	for _, m := range strings.Split(d.mounts.Texto(), ",") {
		if m = strings.TrimSpace(m); m != "" {
			mounts = append(mounts, m)
		}
	}

	bruto := cfg.ComoBruto()
	bruto["alertas"] = alertas
	bruto["ignorar_mounts"] = mounts
	nova, err := nucleo.ConfigDe(bruto)
	if err != nil {
		d.erro = err.Error()
		return
	}
	if err := nucleo.SalvarConfig(j.caminho, bruto); err != nil {
		d.erro = "nao consegui gravar: " + err.Error()
		return
	}
	_ = lim
	j.frota.Trocar(nova)
	j.dialogo = semDialogo
	j.coletar()
}

func fmtNum(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

var _ = fmt.Sprintf
var _ = sort.Strings
