//go:build windows

package terminal

import (
	"os"
	"strconv"
	"syscall"
	"unsafe"
)

var (
	kernel32          = syscall.NewLazyDLL("kernel32.dll")
	pGetConsoleBuffer = kernel32.NewProc("GetConsoleScreenBufferInfo")
	pGetConsoleMode   = kernel32.NewProc("GetConsoleMode")
	pSetConsoleMode   = kernel32.NewProc("SetConsoleMode")
)

type coord struct{ x, y int16 }
type retanguloPequeno struct{ esq, topo, dir, base int16 }
type infoBuffer struct {
	tamanho    coord
	cursor     coord
	atributos  uint16
	janela     retanguloPequeno
	tamanhoMax coord
}

func LarguraTerminal() int {
	if v, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && v > 20 {
		return v
	}
	var info infoBuffer
	r, _, _ := pGetConsoleBuffer.Call(os.Stdout.Fd(),
		uintptr(unsafe.Pointer(&info)))
	if r != 0 {
		if l := int(info.janela.dir - info.janela.esq + 1); l > 20 {
			return l
		}
	}
	return 100
}

// habilitarANSI liga o processamento de sequencias de escape no console.
//
// O console do Windows so interpreta ANSI desde a build 1511, e mesmo assim
// so quando o programa pede. Sem isto a tabela sairia salpicada de "[38;5;"
// no lugar das cores - pior que sem cor nenhuma.
func prepararANSI() bool {
	const virtualTerminal = 0x0004
	h := os.Stdout.Fd()
	var modo uint32
	if r, _, _ := pGetConsoleMode.Call(h, uintptr(unsafe.Pointer(&modo))); r == 0 {
		return false
	}
	if modo&virtualTerminal != 0 {
		return true
	}
	r, _, _ := pSetConsoleMode.Call(h, uintptr(modo|virtualTerminal))
	return r != 0
}
