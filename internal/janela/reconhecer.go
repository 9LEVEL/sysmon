package janela

// A tela de "alertas e notificacoes": o que esta alertando agora, e o que ja
// foi visto e aceito.
//
// Existe porque um alerta que nao pode ser resolvido nem aceito acaba sendo
// ignorado - e a partir dai TODOS sao. "89 desligamentos sujos" e um fato do
// hardware, verdadeiro, que nao muda por si so: repeti-lo a cada 3 segundos
// para sempre nao acrescenta nada e treina o olho a nao ler o rodape.
//
// O que ele NAO faz e silenciar de vez. O reconhecimento guarda o VALOR que
// disparou; quando o valor muda, o alerta volta. Aceitar 89 nao aceita 90.

import (
	"fmt"
	"image"
	"image/color"
	"sort"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"sysmon/internal/nucleo"
	"sysmon/internal/tela"
)

type linhaReconhecer struct {
	a       nucleo.Alerta
	rec     nucleo.Reconhecido
	aceito  bool
	botao   *Botao
	fixo    bool // nao reconhecivel: ajuste o limiar
	orfao   bool // reconhecido, mas nao esta mais alertando
	revogar *Botao
}

type dialogoReconhecer struct {
	linhas   []*linhaReconhecer
	lista    widget.List
	fechar   *Botao
	aceitTod *Botao
	limpar   *Botao
	erro     string
}

func (j *Janela) abrirReconhecer() {
	d := &dialogoReconhecer{
		lista:    widget.List{List: layout.List{Axis: layout.Vertical}},
		fechar:   NovoBotao("FECHAR", tela.Fraco),
		aceitTod: NovoBotao("ACEITAR TODOS", tela.Verde),
		limpar:   NovoBotao("VOLTAR A ALERTAR TUDO", tela.Ambar),
	}
	j.montarReconhecer(d)
	j.dlgReconhecer = d
	j.dialogo = dlgReconhecer
}

// montarReconhecer junta o que esta alertando com o que ja foi aceito.
//
// A lista sai do AlertasBrutos - com os reconhecidos incluidos -, porque a
// tela precisa mostrar tambem o que esta silenciado: senao nao haveria como
// revogar olhando para o que se revoga.
func (j *Janela) montarReconhecer(d *dialogoReconhecer) {
	rec := j.frota.Cfg().Limiares.Reconhecidos
	vistos := map[string]bool{}
	d.linhas = nil

	for _, a := range j.frota.AlertasBrutos() {
		l := &linhaReconhecer{a: a, fixo: !a.Reconhecivel()}
		if r, ok := rec[a.Chave]; ok && r.Valor == a.Valor {
			l.aceito, l.rec = true, r
		}
		if !l.fixo {
			l.botao = NovoBotao(rotuloAceite(l.aceito), corAceite(l.aceito))
		}
		vistos[a.Chave] = true
		d.linhas = append(d.linhas, l)
	}

	// Reconhecimentos que nao correspondem a nenhum alerta atual: ou a
	// condicao passou, ou o host saiu do config. Ficam visiveis para poderem
	// ser removidos - lixo invisivel em arquivo de configuracao e o tipo de
	// coisa que confunde meses depois.
	var orfaos []string
	for chave, r := range rec {
		if !vistos[chave] {
			orfaos = append(orfaos, chave)
			_ = r
		}
	}
	sort.Strings(orfaos)
	for _, chave := range orfaos {
		d.linhas = append(d.linhas, &linhaReconhecer{
			a:       nucleo.Alerta{Chave: chave, Texto: rec[chave].Texto},
			rec:     rec[chave],
			aceito:  true,
			orfao:   true,
			revogar: NovoBotao("REMOVER", tela.Fraco),
		})
	}
}

func rotuloAceite(aceito bool) string {
	if aceito {
		return "VOLTAR A ALERTAR"
	}
	return "ACEITAR"
}

func corAceite(aceito bool) color.NRGBA {
	if aceito {
		return tela.Ambar
	}
	return tela.Verde
}

func (j *Janela) desenharReconhecer(gtx C) {
	d := j.dlgReconhecer
	j.cortina(gtx)
	r := centrado(gtx, 760, 480)
	j.moldura(gtx, r, "alertas e notificacoes")
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	larg, alt := r.Dx(), r.Dy()

	j.subtitulo(gtx, larg, "aceitar um alerta o esconde ate o valor mudar - "+
		"89 aceito volta a avisar em 90")

	corpo := image.Rect(16, 68, larg-16, alt-56)
	func() {
		defer op.Offset(corpo.Min).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Exact(image.Pt(corpo.Dx(), corpo.Dy()))
		if len(d.linhas) == 0 {
			j.Texto(g, 0, 4, "nenhum alerta. nada a aceitar.", tela.Verde, 12, false)
			return
		}
		material.List(j.th, &d.lista).Layout(g, len(d.linhas),
			func(g C, i int) D { return j.linhaReconhecerLayout(g, d.linhas[i], corpo.Dx()) })
	}()

	y := alt - 42
	var esq []*Botao
	if temAceitavel(d) {
		esq = append(esq, d.aceitTod)
	}
	if temAceito(d) {
		esq = append(esq, d.limpar)
	}
	linhas := j.rodapeDialogo(gtx, larg, y, esq, []*Botao{d.fechar})
	j.erroDialogo(gtx, larg, y, linhas, d.erro)
}

func (j *Janela) linhaReconhecerLayout(gtx C, l *linhaReconhecer, larg int) D {
	const alt = 40
	cor := tela.CorNivel(l.a.Nivel)
	rotulo := l.a.Texto

	var bt *Botao
	switch {
	case l.orfao:
		cor = tela.Fraco
		if rotulo == "" {
			rotulo = l.a.Chave
		}
		rotulo = "(nao esta mais alertando) " + rotulo
		bt = l.revogar
	case l.aceito:
		cor = tela.Fraco
		bt = l.botao
	case l.fixo:
		bt = nil
	default:
		bt = l.botao
	}

	larguraBt := 0
	if bt != nil {
		larguraBt = j.larguraBotao(gtx, bt)
		defer func() {
			defer op.Offset(image.Pt(larg-larguraBt, 4)).Push(gtx.Ops).Pop()
			bt.Layout(gtx, j)
		}()
	}

	espaco := larg - larguraBt - 12
	j.Texto(gtx, 0, 2, j.cortarPara(gtx, rotulo, espaco), cor, 12, false)

	// A segunda linha explica o estado em vez de repetir o alerta.
	var nota string
	switch {
	case l.fixo:
		nota = "ajustavel pelo limiar, no icone de aviso - este valor muda sozinho"
	case l.aceito && l.rec.Quando > 0:
		nota = "aceito em " + quando(l.rec.Quando) + " · volta a avisar se mudar de " +
			l.a.Valor
	case l.aceito:
		nota = "aceito · volta a avisar se mudar de " + l.a.Valor
	case l.orfao:
		nota = "reconhecimento sem alerta correspondente"
	default:
		nota = "valor atual: " + l.a.Valor
	}
	j.Texto(gtx, 0, 20, j.cortarPara(gtx, nota, espaco), tela.Ocioso, 11, false)

	return D{Size: image.Pt(larg, alt)}
}

func quando(ts float64) string {
	return time.Unix(int64(ts), 0).Format("02/01 15:04")
}

func temAceitavel(d *dialogoReconhecer) bool {
	for _, l := range d.linhas {
		if !l.fixo && !l.aceito && !l.orfao {
			return true
		}
	}
	return false
}

func temAceito(d *dialogoReconhecer) bool {
	for _, l := range d.linhas {
		if l.aceito {
			return true
		}
	}
	return false
}

// tratarReconhecer roda antes do desenho - ver Janela.tratarCliques.
func (j *Janela) tratarReconhecer(gtx C) {
	d := j.dlgReconhecer
	if d == nil {
		return
	}
	if d.fechar.Clicado(gtx) {
		j.dialogo = semDialogo
		return
	}
	if d.aceitTod.Clicado(gtx) {
		rec := j.copiaReconhecidos()
		for _, l := range d.linhas {
			if !l.fixo && !l.aceito && !l.orfao {
				rec[l.a.Chave] = nucleo.Reconhecido{Valor: l.a.Valor,
					Quando: float64(time.Now().Unix()), Texto: l.a.Texto}
			}
		}
		j.gravarReconhecidos(d, rec)
		return
	}
	if d.limpar.Clicado(gtx) {
		j.gravarReconhecidos(d, nucleo.Reconhecimentos{})
		return
	}
	for _, l := range d.linhas {
		if l.orfao && l.revogar != nil && l.revogar.Clicado(gtx) {
			rec := j.copiaReconhecidos()
			delete(rec, l.a.Chave)
			j.gravarReconhecidos(d, rec)
			return
		}
		if l.botao == nil || !l.botao.Clicado(gtx) {
			continue
		}
		rec := j.copiaReconhecidos()
		if l.aceito {
			delete(rec, l.a.Chave)
		} else {
			rec[l.a.Chave] = nucleo.Reconhecido{Valor: l.a.Valor,
				Quando: float64(time.Now().Unix()), Texto: l.a.Texto}
		}
		j.gravarReconhecidos(d, rec)
		return
	}
}

func (j *Janela) copiaReconhecidos() nucleo.Reconhecimentos {
	out := nucleo.Reconhecimentos{}
	for k, v := range j.frota.Cfg().Limiares.Reconhecidos {
		out[k] = v
	}
	return out
}

// gravarReconhecidos aplica a mudanca no arquivo e na frota.
//
// Grava ANTES de aplicar: uma aceitacao que some no proximo arranque e pior
// que uma que nao aconteceu, porque o usuario acredita ter resolvido.
func (j *Janela) gravarReconhecidos(d *dialogoReconhecer, rec nucleo.Reconhecimentos) {
	bruto := j.frota.Cfg().ComoBruto()
	m := map[string]any{}
	for k, v := range rec {
		e := map[string]any{"valor": v.Valor, "quando": v.Quando}
		if v.Texto != "" {
			e["texto"] = v.Texto
		}
		m[k] = e
	}
	if len(m) == 0 {
		delete(bruto, "reconhecidos")
	} else {
		bruto["reconhecidos"] = m
	}

	nova, err := nucleo.ConfigDe(bruto)
	if err != nil {
		d.erro = err.Error()
		return
	}
	if err := nucleo.SalvarConfig(j.caminho, bruto); err != nil {
		d.erro = "nao consegui gravar: " + err.Error()
		return
	}
	d.erro = ""
	j.frota.Trocar(nova)
	j.coletar()
	j.montarReconhecer(d)
}

// resumoReconhecidos e a linha do rodape que impede o silencio de virar
// esquecimento: quem aceitou dez alertas precisa lembrar que aceitou.
func (j *Janela) resumoReconhecidos() string {
	n := len(j.frota.Cfg().Limiares.Reconhecidos)
	if n == 0 {
		return ""
	}
	if n == 1 {
		return "1 alerta aceito"
	}
	return fmt.Sprintf("%d alertas aceitos", n)
}
