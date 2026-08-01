//go:build windows

// sysmon.exe - lancador do sysmon no Windows.
//
// Faz o mesmo que o sysmon.vbs sempre fez, mas como executavel: promove a
// atualizacao ja baixada e sobe o sysmon.pyz sem janela de console. Existe
// por dois motivos.
//
// O primeiro e que "duplo clique num .bat" e uma experiencia ruim: janela
// preta, associacao de arquivo que qualquer coisa sequestra, e nenhum icone.
//
// O segundo e que os erros de partida no Windows nao tinham para onde ir. O
// .vbs roda sob wscript e o .bat fecha a janela; quando o Python nao estava
// no PATH, o programa simplesmente nao abria, sem dizer por que. Aqui esses
// casos viram uma caixa de dialogo, que e o unico canal que sempre existe.
//
// Em Go, e nao em C, porque o repositorio ja compila Go no CI: o agente e
// escrito nele. Nenhuma toolchain nova, cross-compila do Linux com uma linha
// e sai um binario estatico. Nao ha dependencia externa aqui - so a stdlib -
// pela mesma razao que o agente nao tem.
//
// O que ele NAO faz: embutir o Python. Continua sendo necessario ter Python
// instalado; isto troca a janela preta por um executavel, nao a dependencia.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// versao e sobrescrita no build: -ldflags "-X main.versao=..."
var versao = "dev"

const (
	pyz      = "sysmon.pyz"
	pendente = "sysmon-novo.pyz"

	// No logon o Windows ainda sobe rede e servicos. Comecar a sondar host
	// nesse momento so gera "offline" que se resolve sozinho em seguida.
	esperaLogon = 20 * time.Second

	// A troca insiste porque, quando quem pede e o botao da interface, este
	// processo comeca antes de o sysmon antigo terminar de sair - e ate la o
	// arquivo esta em uso.
	tentativasTroca = 20
	intervaloTroca  = 300 * time.Millisecond
)

func main() {
	pasta, err := os.Executable()
	if err != nil {
		alertar("Nao consegui descobrir a minha propria pasta:\n\n" + err.Error())
		os.Exit(1)
	}
	pasta = filepath.Dir(pasta)
	if err := os.Chdir(pasta); err != nil {
		alertar("Nao consegui entrar na pasta do programa:\n\n" + err.Error())
		os.Exit(1)
	}

	alvo := filepath.Join(pasta, pyz)
	if _, err := os.Stat(alvo); err != nil {
		alertar("Nao encontrei o " + pyz + " em:\n\n" + pasta +
			"\n\nO lancador e o sysmon.pyz precisam ficar na mesma pasta.")
		os.Exit(1)
	}

	trocarPendente(pasta)

	agora, args := separarAgora(os.Args[1:])
	if len(args) == 0 && !agora {
		// Sem argumento nenhum: e o autostart do logon.
		time.Sleep(esperaLogon)
		args = []string{"--oculto"}
	}

	python, comoAchei := acharPython()
	if python == "" {
		alertar("Python nao encontrado nesta maquina.\n\n" +
			"Instale de https://python.org e marque a caixa\n" +
			"\"Add python.exe to PATH\" durante a instalacao.\n\n" +
			"Depois abra o sysmon de novo.")
		os.Exit(1)
	}

	cmd := exec.Command(python, append([]string{alvo}, args...)...)
	cmd.Dir = pasta
	// Sem console: o pythonw ja e a versao sem janela, e este processo some
	// logo em seguida. HideWindow cobre o caso de termos caido no python.exe.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		alertar("Encontrei o Python em:\n\n" + python + "\n(" + comoAchei + ")\n\n" +
			"mas nao consegui inicia-lo:\n\n" + err.Error())
		os.Exit(1)
	}
	// Nao esperamos: o sysmon vive por conta propria, e este lancador so
	// existe para chegar ate aqui.
}

// separarAgora consome o /agora, que e nosso e nao do sysmon.
//
// E como o botao de atualizar pede "sobe ja": sem a espera de logon e sem
// minimizar na bandeja. Repassar essa opcao adiante faria o argparse do
// Python reclamar de argumento desconhecido.
func separarAgora(entrada []string) (bool, []string) {
	agora := false
	saida := make([]string, 0, len(entrada))
	for _, a := range entrada {
		if strings.EqualFold(a, "/agora") {
			agora = true
			continue
		}
		saida = append(saida, a)
	}
	return agora, saida
}

// trocarPendente promove a atualizacao baixada, se houver.
//
// A troca acontece AQUI, antes de o Python abrir o arquivo: no Windows um
// processo nao sobrescreve com seguranca o proprio .pyz que tem aberto.
// Falhar nao e fatal - o sysmon sobe na versao antiga e o arquivo pendente
// espera o proximo arranque.
func trocarPendente(pasta string) {
	novo := filepath.Join(pasta, pendente)
	if _, err := os.Stat(novo); err != nil {
		return
	}
	alvo := filepath.Join(pasta, pyz)
	for i := 0; i < tentativasTroca; i++ {
		if err := os.Rename(novo, alvo); err == nil {
			return
		}
		// Remover antes de renomear: o Rename do Windows nao sobrescreve.
		os.Remove(alvo)
		if err := os.Rename(novo, alvo); err == nil {
			return
		}
		time.Sleep(intervaloTroca)
	}
}

// acharPython devolve o interpretador e como ele foi encontrado.
//
// Preferimos o pythonw.exe: e o Python sem console, e o que faz a janela
// subir limpa. O python.exe entra so como ultimo recurso.
func acharPython() (string, string) {
	if p, err := exec.LookPath("pythonw.exe"); err == nil {
		return p, "PATH"
	}
	// py.exe e o lancador oficial, instalado mesmo quando o Python nao entrou
	// no PATH - o caso mais comum de "instalei e nao funciona".
	if p, err := exec.LookPath("pyw.exe"); err == nil {
		return p, "lancador py"
	}
	for _, base := range []string{
		os.Getenv("LOCALAPPDATA") + `\Programs\Python`,
		os.Getenv("ProgramFiles") + `\Python`,
		`C:\Python`,
	} {
		if p := procurarEm(base); p != "" {
			return p, base
		}
	}
	if p, err := exec.LookPath("python.exe"); err == nil {
		return p, "PATH (com console)"
	}
	return "", ""
}

// procurarEm varre as instalacoes padrao (Python313, Python312, ...) e
// devolve a mais nova que tiver pythonw.exe.
func procurarEm(base string) string {
	itens, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	melhor := ""
	for _, it := range itens {
		if !it.IsDir() || !strings.HasPrefix(strings.ToLower(it.Name()), "python") {
			continue
		}
		p := filepath.Join(base, it.Name(), "pythonw.exe")
		if _, err := os.Stat(p); err == nil && p > melhor {
			melhor = p // ordem alfabetica basta: Python313 > Python312
		}
	}
	return melhor
}

// alertar mostra uma caixa de dialogo do proprio Windows.
//
// Compilado com -H windowsgui nao existe console para escrever: sem isto,
// toda falha de partida seria um programa que simplesmente nao abre.
func alertar(texto string) {
	const mbIconWarning = 0x30
	titulo, err1 := syscall.UTF16PtrFromString("sysmon " + versao)
	corpo, err2 := syscall.UTF16PtrFromString(texto)
	if err1 != nil || err2 != nil {
		return
	}
	proc := syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
	proc.Call(0, uintptr(unsafe.Pointer(corpo)), uintptr(unsafe.Pointer(titulo)),
		mbIconWarning)
}
