package smart

import (
	"strings"
	"testing"
)

// achadosDeTudo produz um achado de cada familia de regra, para que este
// arquivo nao precise ser atualizado quando uma regra nova entrar - ela vai
// aparecer aqui sozinha se for disparada por alguma destas leituras.
func achadosDeTudo(t *testing.T) []Achado {
	t.Helper()
	leituras := []Leitura{
		// reserva no limite, mais desgaste e temperatura
		{Dev: "a", Tipo: "ssd", ColetaOK: true, PercentualUsado: f(96),
			TempC: f(72), TempMaxC: f(75), Throttle: true,
			DesligamentosSujos: c(39), CiclosEnergia: c(90),
			Atributos: []Atributo{
				attr("Available_Reservd_Space", 0, id(232), valor(12), limiar(10)),
				attr("Current_Pending_Sector", 12, id(197)),
				attr("UDMA_CRC_Error_Count", 12, id(199), hist(0, 3, 3, 9, 30)),
				attr("Reallocated_Sector_Ct", 20, id(5), hist(6, 12, 14, 6, 30)),
			}},
		// hdd sem reserva, contagem bruta e blocos crescidos
		{Dev: "b", Tipo: "hdd", ColetaOK: true,
			Atributos: []Atributo{
				attr("Reallocated_Sector_Ct", 200, id(5), hist(0, 20, 40, 160, 60)),
				attr("Grown_Bad_Blocks", 60, id(170), hist(0, 5, 9, 51, 60)),
				attr("Total_Bad_Blocks", 100, id(169)),
			}},
		// coleta falha
		{Dev: "c", ColetaOK: false, ErroColeta: "precisa de -d megaraid,0"},
	}
	var out []Achado
	for _, l := range leituras {
		out = append(out, Avaliar(l, Config{}).Achados...)
	}
	if len(out) < 8 {
		t.Fatalf("so %d achados; as leituras de teste pararam de cobrir as regras",
			len(out))
	}
	return out
}

func TestFormaCurtaNuncaTerminaNoMeio(t *testing.T) {
	// A tabela mostra uma linha por disco e cortava a frase por numero de
	// caracteres: saia "39 de 90 desligamentos foram", que nao e frase nem
	// numero. Toda mensagem e escrita como "o que aconteceu - o que fazer", e
	// a forma curta e a primeira metade inteira.
	for _, a := range achadosDeTudo(t) {
		curto := a.Curto()
		if curto == "" {
			t.Errorf("%s: forma curta vazia", a.Regra)
			continue
		}
		if strings.HasSuffix(curto, " ") || strings.HasSuffix(curto, ",") {
			t.Errorf("%s: forma curta termina truncada: %q", a.Regra, curto)
		}
		if len(curto) > 60 {
			t.Errorf("%s: %d caracteres nao cabem na linha do disco: %q",
				a.Regra, len(curto), curto)
		}
		if !strings.HasPrefix(a.Mensagem, curto) {
			t.Errorf("%s: a forma curta nao e o inicio da mensagem", a.Regra)
		}
	}
}

func TestConselhoSoAparecerNaMensagemInteira(t *testing.T) {
	// O conselho e o que economiza uma busca na internet, e o rodape tem
	// espaco para ele. Se a forma curta o levasse junto, nao seria curta.
	l := Leitura{Dev: "a", Tipo: "ssd", ColetaOK: true,
		DesligamentosSujos: c(39), CiclosEnergia: c(90)}
	v := Avaliar(l, Config{})
	a, ok := v.Pior()
	if !ok {
		t.Fatal("nao achou nada")
	}
	if !strings.Contains(a.Mensagem, "nobreak") {
		t.Fatalf("mensagem sem o conselho: %q", a.Mensagem)
	}
	if strings.Contains(a.Curto(), "nobreak") {
		t.Fatalf("a forma curta levou o conselho junto: %q", a.Curto())
	}
	if !strings.Contains(a.Curto(), "43%") {
		t.Fatalf("a forma curta perdeu o numero: %q", a.Curto())
	}
}
