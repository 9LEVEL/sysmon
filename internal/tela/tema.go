// Package tela e o modelo do que aparece na tela, sem saber desenhar.
//
// Existe separado porque a JANELA e o TERMINAL mostram a mesma coisa. Com a
// montagem das linhas aqui, os dois nao tem como divergir sobre o que exibir
// ou sobre a cor de cada valor - que era exatamente o problema que existia
// enquanto cada tela montava a sua.
//
// Nao depende de nenhum toolkit grafico: e o que permite o modo texto existir
// sem arrastar o Gio junto, e o que torna tudo isto testavel sem abrir tela.
package tela

import (
	"image/color"

	"sysmon/internal/nucleo"
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

	// Dois tons so para DISTINGUIR, nunca para dizer que algo esta mal - por
	// isso ficam fora da escala de status. Sao usados nas duas janelas da
	// carga, onde o problema nao e gravidade e sim saber qual numero e qual.
	Ciano   = rgb(0x5c, 0xc8, 0xd4)
	Magenta = rgb(0xc2, 0x8b, 0xdb)
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
