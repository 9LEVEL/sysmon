package janela

import (
	"testing"

	"gioui.org/io/key"
)

// tecla injeta uma tecla e roda os quadros necessarios.
func (b *bancada) tecla(nome key.Name) {
	b.quadro()
	b.r.Queue(key.Event{Name: nome, State: key.Press})
	b.quadro()
	b.quadro()
}

func TestF5ForcaUmaColetaAgora(t *testing.T) {
	// A janela ja atualiza sozinha no intervalo, mas depois de mexer num host
	// - reiniciar um servico, liberar espaco - esperar o ciclo para confirmar
	// e tempo parado olhando para um numero que se sabe estar velho.
	//
	// O README prometia F5 desde a v4 e nada estava ligado a ele.
	b := bancadaDoisHosts(t)
	b.quadro()
	antes := len(b.j.linhas)
	b.tecla(key.NameF5)
	if len(b.j.linhas) != antes {
		t.Fatalf("linhas = %d, antes %d", len(b.j.linhas), antes)
	}
	// O que da para afirmar sem rede: a tecla foi consumida e nao derrubou
	// nada. O efeito (AtualizarAgora) e do pacote nucleo, testado la.
}

func TestEscapeFechaMesmoSemDialogoAberto(t *testing.T) {
	// Nao pode explodir nem mexer em nada quando nao ha o que fechar.
	b := novaBancada(t)
	b.tecla(key.NameEscape)
	if b.j.dialogo != semDialogo {
		t.Fatalf("dialogo = %d", b.j.dialogo)
	}
}

func TestAAreaDeTecladoNaoEngoleOsCliques(t *testing.T) {
	// A area de teclado cobre a janela inteira. No Gio quem declara depois
	// fica por cima, e o ponteiro vai para o de cima: declarada no fim, ela
	// engolia TODOS os cliques - cabecalho, dialogos, o clique que recolhe o
	// host. Este teste fixa a ordem.
	b := novaBancada(t)
	b.clique(iconeX(b.tam.X, 2), 19) // hosts
	if b.j.dialogo != dlgHosts {
		t.Fatal("o clique no cabecalho nao chegou ao botao")
	}
	// E o teclado continua funcionando por baixo dela.
	b.tecla(key.NameEscape)
	if b.j.dialogo != semDialogo {
		t.Fatal("o ESC parou de chegar")
	}
}
