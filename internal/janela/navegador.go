package janela

import (
	"os/exec"
	"runtime"
	"strings"
)

// AbrirURL abre um endereco no navegador do sistema.
//
// Cada sistema tem o seu jeito e nenhum deles e uma chamada de biblioteca:
// no Windows e o ShellExecute que o rundll32 expoe, no macOS o `open`, no
// Linux o `xdg-open` do freedesktop. Sao tres processos externos, e por isso
// a funcao valida a URL antes: passar texto de origem duvidosa para um shell
// helper e como se abre buraco.
//
// Erro e ignorado de proposito. Nao ha o que fazer a respeito - se o
// navegador nao abre, mostrar uma caixa dizendo isso nao ajuda ninguem, e
// travar a interface por causa de um link seria pior.
func AbrirURL(url string) {
	if !urlSegura(url) {
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// Nao `cmd /c start`: ali o & e o ^ da URL sao metacaracteres do
		// interpretador de comandos.
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// urlSegura aceita so http e https, e nada que possa virar argumento.
//
// Hoje o unico chamador passa uma constante, mas essa e a garantia de que
// continuara seguro no dia em que alguem passar uma URL vinda do config.
func urlSegura(url string) bool {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return false
	}
	if strings.ContainsAny(url, " \t\r\n\"'\\|&;<>$`") {
		return false
	}
	return len(url) < 2048
}
