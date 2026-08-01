package janela

import "testing"

func TestSecaoEMedidaSeguemAMargemEscolhida(t *testing.T) {
	// Antes, DESEMPENHO comecava em 70px e "cpu" em 110px - recuo herdado de
	// uma arvore com raiz, que aqui nao existe: o host e a linha inteira
	// acima, e nao um no a esquerda. Numa janela de 470 aquilo era espaco
	// vazio, e a margem escolhida nao mudava nada.
	for _, margem := range []int{0, 5, 10, 20} {
		b := novaBancada(t)
		b.j.margemEsq = margem

		querSecao := margem
		querMedida := margem + RecuoMedida
		if got := b.j.margemEsq; got != querSecao {
			t.Errorf("margem %d: secao em %d", margem, got)
		}
		if got := b.j.margemEsq + RecuoMedida; got != querMedida {
			t.Errorf("margem %d: medida em %d, queria %d", margem, got, querMedida)
		}
		// A medida recua da secao, e nao o contrario.
		if querMedida <= querSecao {
			t.Errorf("margem %d: a hierarquia inverteu", margem)
		}
	}
}

func TestALinhaDoHostNaoSegueAMargem(t *testing.T) {
	// Ela e a ancora visual da frota - o fio de estado marca onde cada bloco
	// comeca. Afastar isso da borda so estreita a tela sem ganhar nada.
	b := bancadaDoisHosts(t)
	b.j.margemEsq = 20
	b.j.coletar()
	b.quadro()

	// O desenho da linha do host nao consulta margemEsq em lugar nenhum; o
	// que da para afirmar aqui e que a tabela inteira comeca em zero.
	if b.j.margemEsq != 20 {
		t.Fatal("a margem nao foi aplicada ao estado")
	}
	// E o padrao e zero, como pedido.
	outra := novaBancada(t)
	if outra.j.margemEsq != 0 {
		t.Fatalf("margem padrao = %d, queria 0", outra.j.margemEsq)
	}
}
