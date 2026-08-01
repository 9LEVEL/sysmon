//go:build !windows

package terminal

import (
	"os"
	"strconv"
	"syscall"
	"unsafe"
)

// LarguraTerminal descobre quantas colunas ha.
//
// COLUMNS primeiro porque quem canaliza a saida costuma defini-la; depois o
// ioctl, que e a fonte de verdade; e 100 como piso razoavel quando nao ha
// terminal nenhum - a tabela precisa de um numero para nao ficar espremida.
func LarguraTerminal() int {
	if v, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && v > 20 {
		return v
	}
	var ws struct{ linhas, colunas, x, y uint16 }
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdout.Fd(),
		uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	if errno == 0 && ws.colunas > 20 {
		return int(ws.colunas)
	}
	return 100
}
