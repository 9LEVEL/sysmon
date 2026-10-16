//go:build windows

package janela

import (
	"syscall"

	"gioui.org/app"
	"gioui.org/io/event"
)

// Esconder de verdade, e nao minimizar.
//
// O Gio so sabe minimizar, e minimizada a janela continua na barra de
// tarefas: o X "fechava para a bandeja" e o sysmon seguia ali embaixo, como
// se nao tivesse fechado. Com SW_HIDE ela sai da tela e da barra, e so a
// bandeja a traz de volta - que e o que o X promete.
//
// ShowWindowAsync, e nao ShowWindow: este manda a mensagem e ESPERA a thread
// da janela responder, e a thread do Gio pode estar parada esperando por
// nos - o que travaria as duas. O Async so posta e volta.
var (
	pShowWindowAsync = syscall.NewLazyDLL("user32.dll").NewProc("ShowWindowAsync")
)

const (
	swHide = 0
	swShow = 5
)

type janelaNativa struct {
	hwnd uintptr
	// pendente guarda o pedido de esconder feito antes de o handle existir:
	// o --oculto pede logo na abertura, e o Gio so entrega o HWND depois.
	pendente bool
}

func (n *janelaNativa) ver(e event.Event) {
	v, ok := e.(app.Win32ViewEvent)
	if !ok {
		return
	}
	n.hwnd = v.HWND
	if n.hwnd != 0 && n.pendente {
		n.pendente = false
		pShowWindowAsync.Call(n.hwnd, swHide)
	}
}

func (n *janelaNativa) esconder() bool {
	if n.hwnd == 0 {
		n.pendente = true
		return true
	}
	pShowWindowAsync.Call(n.hwnd, swHide)
	return true
}

func (n *janelaNativa) mostrar() {
	n.pendente = false
	if n.hwnd != 0 {
		pShowWindowAsync.Call(n.hwnd, swShow)
	}
}
