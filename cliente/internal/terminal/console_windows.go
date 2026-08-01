//go:build windows

package terminal

import (
	"os"
	"syscall"
)

var pAttachConsole = kernel32.NewProc("AttachConsole")

// PrenderConsole liga a saida do programa ao console de quem o chamou.
//
// O binario e compilado como GUI (-H windowsgui) para que o duplo clique nao
// abra janela preta. O efeito colateral e que ele nasce SEM console: rodar
// `sysmon.exe term` num terminal imprimiria em lugar nenhum, e a tabela
// simplesmente nao apareceria.
//
// AttachConsole(ATTACH_PARENT_PROCESS) resolve: o processo se anexa ao
// console que ja existe. Quando nao ha nenhum - duplo clique - a chamada
// falha e nao fazemos nada, que e o comportamento certo.
//
// Devolve false quando nao ha console; quem chama pode entao avisar por
// caixa de dialogo em vez de escrever para o vazio.
func PrenderConsole() bool {
	const paiDoProcesso = ^uintptr(0) // (DWORD)-1
	r, _, _ := pAttachConsole.Call(paiDoProcesso)
	if r == 0 {
		return false
	}
	// Reabrir os tres descritores: o AttachConsole liga o processo ao
	// console, mas nao redireciona o que ja estava aberto.
	for _, p := range []struct {
		nome string
		alvo **os.File
	}{
		{"CONOUT$", &os.Stdout},
		{"CONOUT$", &os.Stderr},
		{"CONIN$", &os.Stdin},
	} {
		modo := os.O_RDWR
		h, err := syscall.Open(p.nome, modo, 0)
		if err != nil {
			continue
		}
		*p.alvo = os.NewFile(uintptr(h), p.nome)
	}
	return true
}
