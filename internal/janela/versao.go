package janela

// A tela de versao: "estou na mais nova?" com resposta que fica na tela.
//
// O botao de baixar respondia so pelo rodape, e o rodape e dos alertas -
// com um disco alertando, a resposta nunca aparecia. A pergunta e rara e a
// resposta precisa ser inequivoca, entao e um dialogo: abre ao clicar, mostra
// a busca acontecendo, e fica ate voce fechar.

import (
	"image/color"
	"strconv"
	"time"

	"gioui.org/op"

	"sysmon/internal/atualizar"
	"sysmon/internal/tela"
)

type dialogoVersao struct {
	fechar    *Botao
	instalar  *Botao
	novamente *Botao
}

func (j *Janela) abrirVersao() {
	j.dlgVersao = &dialogoVersao{
		fechar:    NovoBotao("FECHAR", tela.Fraco),
		instalar:  NovoBotao("INSTALAR E REINICIAR", tela.Verde),
		novamente: NovoBotao("PROCURAR DE NOVO", tela.Titulo),
	}
	j.dialogo = dlgVersao
}

// situacaoVersao traduz o estado do atualizador no que a tela diz. Separada
// do desenho para poder ser testada sem janela.
type situacaoVersao struct {
	titulo   string
	detalhe  string
	cor      color.NRGBA
	instalar bool // ha versao baixada e conferida
	repetir  bool // a busca terminou e pode ser refeita
}

func situacaoDe(e atualizar.Estado, agora time.Time) situacaoVersao {
	instalada := "versao instalada: " + e.Versao
	switch {
	case !e.Suportado:
		return situacaoVersao{
			titulo:  "atualizacao automatica indisponivel",
			detalhe: instalada + " · rodando sem um executavel proprio (go run)",
			cor:     tela.Fraco,
		}
	case e.Pronta:
		return situacaoVersao{
			titulo:   "versao " + e.Disponivel + " pronta para instalar",
			detalhe:  instalada + " · a nova ja foi baixada e conferida (SHA256)",
			cor:      tela.Verde,
			instalar: true,
		}
	case e.Checando && e.Disponivel != "":
		return situacaoVersao{
			titulo:  "baixando a versao " + e.Disponivel + "...",
			detalhe: instalada,
			cor:     tela.Titulo,
		}
	case e.Checando:
		return situacaoVersao{
			titulo:  "procurando versao nova...",
			detalhe: instalada,
			cor:     tela.Titulo,
		}
	case e.Erro != "":
		return situacaoVersao{
			titulo:  "nao consegui verificar",
			detalhe: e.Erro,
			cor:     tela.Ambar,
			repetir: true,
		}
	case e.Conferido.IsZero():
		// Nada perguntado ainda nesta execucao - o dialogo abre ja buscando,
		// entao isto so aparece por um instante.
		return situacaoVersao{titulo: "procurando versao nova...",
			detalhe: instalada, cor: tela.Titulo}
	}
	return situacaoVersao{
		titulo:  "voce esta na versao mais recente: " + e.Versao,
		detalhe: "conferido no GitHub " + haQuanto(agora.Sub(e.Conferido)),
		cor:     tela.Verde,
		repetir: true,
	}
}

func haQuanto(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "agora"
	case d < time.Hour:
		return "ha " + strconv.Itoa(int(d/time.Minute)) + " min"
	case d < 48*time.Hour:
		return "ha " + strconv.Itoa(int(d/time.Hour)) + " h"
	}
	return "ha " + strconv.Itoa(int(d/(24*time.Hour))) + " dias"
}

func (j *Janela) desenharVersao(gtx C) {
	d := j.dlgVersao
	j.cortina(gtx)
	r := centrado(gtx, 520, 190)
	j.moldura(gtx, r, "versao do sysmon")
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	larg, alt := r.Dx(), r.Dy()

	s := situacaoDe(j.Atual.Estado(), time.Now())
	j.Texto(gtx, 16, 52, j.cortarPara(gtx, s.titulo, larg-32), s.cor, 13, true)
	j.Texto(gtx, 16, 78, j.cortarPara(gtx, s.detalhe, larg-32), tela.Fraco, 12, false)

	var esq, dir []*Botao
	if s.instalar {
		esq = append(esq, d.instalar)
	}
	if s.repetir {
		esq = append(esq, d.novamente)
	}
	dir = append(dir, d.fechar)
	j.rodapeDialogo(gtx, larg, alt-42, esq, dir)
}

// tratarVersao roda antes do desenho - ver Janela.tratarCliques.
func (j *Janela) tratarVersao(gtx C) {
	d := j.dlgVersao
	if d == nil {
		return
	}
	if d.fechar.Clicado(gtx) {
		j.dialogo = semDialogo
		return
	}
	if d.instalar.Clicado(gtx) && j.Atual.Estado().Pronta {
		j.aplicarUpdate()
		return
	}
	if d.novamente.Clicado(gtx) {
		j.Atual.VerificarEmThread(func() {
			if w := j.wAtual.Load(); w != nil {
				w.Invalidate()
			}
		})
	}
}
