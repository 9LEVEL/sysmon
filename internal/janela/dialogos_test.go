package janela

import "testing"

func TestCamposCabemNaJanelaMinima(t *testing.T) {
	// A janela minima e 470; o dialogo fica 20 menor e o corpo dele 32 menor
	// ainda. Era ali que o TESTAR e o × saiam pela borda.
	const corpoMinimo = 470 - 20 - 32
	const wTestar, wRemover = 78, 34

	nome, url, token := larguraCampos(corpoMinimo, wTestar, wRemover)
	usado := nome + url + token + wTestar + wRemover + 4*8
	if usado > corpoMinimo {
		t.Fatalf("usa %d de %d disponiveis: os botoes saem pela borda",
			usado, corpoMinimo)
	}
	for _, c := range []struct {
		nome string
		v    int
	}{{"nome", nome}, {"url", url}, {"token", token}} {
		if c.v < 50 {
			t.Errorf("%s ficou com %d px, estreito demais para ler", c.nome, c.v)
		}
	}
}

func TestLarguraSobrandoVaiParaAURL(t *testing.T) {
	// Numa janela larga o desenho original volta, e o excedente vai para o
	// campo onde texto comprido de fato aparece.
	nome, url, token := larguraCampos(1200, 78, 34)
	if nome != 110 || token != 170 {
		t.Errorf("nome=%d token=%d, queria 110 e 170", nome, token)
	}
	if url <= 300 {
		t.Errorf("url = %d; a sobra nao foi para ela", url)
	}
	if soma := nome + url + token + 78 + 34 + 4*8; soma != 1200 {
		t.Errorf("soma = %d, queria 1200", soma)
	}
}

func TestNuncaDevolveLarguraNegativa(t *testing.T) {
	// Janela absurdamente estreita nao pode virar largura negativa, que no
	// Gio vira panico de constraint invalida.
	for _, larg := range []int{0, 50, 120, 200} {
		nome, url, token := larguraCampos(larg, 78, 34)
		if nome <= 0 || url <= 0 || token <= 0 {
			t.Fatalf("larg=%d deu %d/%d/%d", larg, nome, url, token)
		}
	}
}
