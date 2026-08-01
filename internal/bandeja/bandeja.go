// Package bandeja poe o sysmon no canto do relogio.
//
// E o que faz a ferramenta ser util sem estar aberta: o icone muda de cor
// pelo PIOR host da frota, entao um olhar para a barra de tarefas responde
// "esta tudo bem?" sem abrir nada. Fechar a janela nao encerra o programa -
// ele continua vigiando dali.
//
// A implementacao de verdade e do Windows, em Win32 puro por syscall. Nao ha
// biblioteca envolvida: o mesmo caminho que ja usamos no lancador para a
// caixa de dialogo, e que mantem o binario sem dependencia para isto.
package bandeja

import (
	"fmt"

	"sysmon/internal/nucleo"
)

// Acoes sao os ganchos que a bandeja chama. Todos rodam na thread da
// bandeja: quem precisa tocar a interface deve enfileirar, nunca desenhar
// direto.
type Acoes struct {
	Mostrar   func()
	Atualizar func()
	Topo      func()
	Sair      func()
	NoTopo    func() bool
}

// Bandeja e o icone vivo.
type Bandeja interface {
	// Estado repinta o icone e troca a dica do mouse.
	Estado(nivel int, dica string)
	// Notificar mostra um balao. Usado so na MUDANCA de severidade: um
	// balao por coleta seria ruido, e ruido vira alerta ignorado.
	Notificar(titulo, texto string)
	Fechar()
}

// Cor devolve o RGB do icone para cada severidade.
//
// Sao as mesmas cores da janela, e nao outras: o icone e a janela precisam
// concordar, senao um verde no canto e um vermelho na tela viram duvida
// sobre qual dos dois esta certo.
func Cor(nivel int) (r, g, b byte) {
	switch nivel {
	case nucleo.Aviso:
		return 0xd2, 0x99, 0x22
	case nucleo.Critico:
		return 0xf8, 0x51, 0x49
	case nucleo.Offline:
		return 0x6b, 0x76, 0x84
	}
	return 0x3f, 0xb9, 0x50
}

// Dica monta o texto que aparece ao parar o mouse sobre o icone.
//
// O Windows corta a dica em 127 caracteres, entao ela precisa dizer o
// essencial primeiro: quantos hosts, quantos fora do ar, quantos alertas.
func Dica(nivel, hosts, offline, alertas int) string {
	s := fmt.Sprintf("sysmon · %d host", hosts)
	if hosts != 1 {
		s += "s"
	}
	switch {
	case offline > 0 && alertas > 0:
		s += fmt.Sprintf(" · %d offline · %d alerta", offline, alertas)
	case offline > 0:
		s += fmt.Sprintf(" · %d offline", offline)
	case alertas > 0:
		s += fmt.Sprintf(" · %d alerta", alertas)
	default:
		return s + " · sem alertas"
	}
	if alertas > 1 {
		s += "s"
	}
	return corta(s, 127)
}

func corta(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
