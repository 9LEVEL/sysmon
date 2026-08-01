package janela

import "testing"

// A janela minima e 470; o dialogo fica 20 menor e o corpo dele 32 menor
// ainda. Era ali que o campo virava fresta e o botao de remover saia pela
// borda.
const corpoMinimo = 470 - 20 - 32

func TestAsDuasLinhasDoHostCabemNaJanelaMinima(t *testing.T) {
	const wTestar, wRemover = 78, 100
	nome, url, token := larguraCampos(corpoMinimo, wTestar, wRemover)

	// Primeira linha: apelido + url.
	if usado := nome + 8 + url; usado > corpoMinimo {
		t.Errorf("primeira linha usa %d de %d", usado, corpoMinimo)
	}
	// Segunda: token + os dois botoes, que nao encolhem.
	if usado := token + 8 + wTestar + 8 + wRemover; usado > corpoMinimo {
		t.Errorf("segunda linha usa %d de %d", usado, corpoMinimo)
	}
	for _, c := range []struct {
		nome string
		v    int
	}{{"apelido", nome}, {"url", url}, {"token", token}} {
		if c.v < 60 {
			t.Errorf("%s ficou com %d px, estreito demais para ler", c.nome, c.v)
		}
	}
}

func TestLarguraSobrandoVaiParaOsCamposLongos(t *testing.T) {
	// Numa janela larga, o apelido nao precisa crescer - ele tem cinco
	// letras. O espaco vai para url e token, que sao onde texto comprido de
	// fato aparece.
	nome, url, token := larguraCampos(1200, 78, 100)
	if nome != 130 {
		t.Errorf("apelido = %d, queria a largura preferida de 130", nome)
	}
	if soma := nome + 8 + url; soma != 1200 {
		t.Errorf("a primeira linha nao ocupa a largura: %d", soma)
	}
	if token != 1200-78-100-16 {
		t.Errorf("token = %d; a sobra nao foi para ele", token)
	}
}

func TestNuncaDevolveLarguraNegativa(t *testing.T) {
	// Janela absurdamente estreita nao pode virar largura negativa, que no
	// Gio vira panico de constraint invalida.
	for _, larg := range []int{0, 50, 120, 200} {
		nome, url, token := larguraCampos(larg, 78, 100)
		if nome <= 0 || url <= 0 || token <= 0 {
			t.Fatalf("larg=%d deu %d/%d/%d", larg, nome, url, token)
		}
	}
}
