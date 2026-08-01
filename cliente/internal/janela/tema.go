// Package janela desenha a interface do sysmon.
//
// Gio e immediate-mode: nao existe widget de arvore nem tabela, tudo e
// desenhado a cada quadro. O custo disso e escrever o layout na mao; o ganho
// e controle total sobre densidade e cor, que e exatamente o que uma tela de
// monitoramento precisa - e o que nenhum toolkit de proposito geral entrega
// sem briga.
package janela

import (
	"image/color"

	"sysmon-cliente/internal/nucleo"
)

// Paleta escura. Os tons de status ficam acima de 4.5:1 no fundo, e o valor
// numerico sempre acompanha - cor reforca, nunca carrega sozinha.
var (
	Fundo    = rgb(0x0b, 0x0e, 0x14)
	Painel   = rgb(0x0f, 0x13, 0x1b)
	Grade    = rgb(0x1b, 0x21, 0x30)
	Texto    = rgb(0xc9, 0xd1, 0xd9)
	Fraco    = rgb(0x6b, 0x76, 0x84)
	Titulo   = rgb(0x58, 0xa6, 0xff)
	Verde    = rgb(0x3f, 0xb9, 0x50)
	Ambar    = rgb(0xd2, 0x99, 0x22)
	Vermelho = rgb(0xf8, 0x51, 0x49)
	Selecao  = rgb(0x16, 0x1b, 0x26)
	Ativo    = rgb(0x39, 0xc5, 0xcf)
	Ocioso   = rgb(0x4a, 0x55, 0x63)
)

func rgb(r, g, b uint8) color.NRGBA { return color.NRGBA{R: r, G: g, B: b, A: 255} }

// Alfa devolve a cor com transparencia. O Gio tem canal alfa de verdade, o
// que permite halo e sobreposicao sem simular mistura com o fundo.
func Alfa(c color.NRGBA, a uint8) color.NRGBA { c.A = a; return c }

// CorNivel mapeia a severidade da avaliacao para a paleta.
func CorNivel(n int) color.NRGBA {
	switch n {
	case nucleo.Aviso:
		return Ambar
	case nucleo.Critico:
		return Vermelho
	case nucleo.Offline:
		return Fraco
	}
	return Texto
}

// Cinco degraus de magnitude, e nao tres.
//
// Com so ok/aviso/critico, sair de 3% para 30% de CPU nao mudava nada na
// tela - e essa e a variacao que interessa no dia a dia. Os dois degraus de
// cima continuam sendo os limiares de alerta, entao ambar e vermelho seguem
// querendo dizer "olhe para isto".
func CorMagnitude(pct float64, aviso, critico float64) color.NRGBA {
	switch {
	case pct >= critico:
		return Vermelho
	case pct >= aviso:
		return Ambar
	case pct >= 50:
		return Ativo
	case pct >= 20:
		return Texto
	}
	return Ocioso
}
