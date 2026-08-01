package nucleo

import (
	"strings"
	"testing"

	"sysmon/internal/metricas"
)

// wdBlue e a tabela SMART real de um WDC WDS240G2G0A-00JH30 com 14370 horas.
// Ele tem 4 setores realocados e 4 blocos crescidos, parados, com a reserva
// intacta - o caso em que a v5.0 reclamava e a especificacao diz para nao
// reclamar.
func wdBlue() metricas.Bloco {
	a := func(id int, nome string, valor, pior, limiar int, cru int64) metricas.SmartAtributo {
		return metricas.SmartAtributo{ID: id, Nome: nome, Valor: ii(valor),
			Pior: ii(pior), Limiar: ii(limiar), Cru: i(cru)}
	}
	return metricas.Bloco{
		Dev: "sda", Tipo: "ssd", Modelo: "WDC WDS240G2G0A-00JH30", TempC: f(44),
		Smart: &metricas.Smart{
			ColetaOK: true, Saude: "ok", Serial: "204103A00EEE",
			Familia:         "WD Blue / Red / Green SSDs",
			DesgastePercent: f(0), CiclosEnergia: i(206), DesligamentosSujos: i(89),
			Atributos: []metricas.SmartAtributo{
				a(5, "Reallocated_Sector_Ct", 100, 100, 0, 4),
				a(9, "Power_On_Hours", 100, 100, 0, 14370),
				a(12, "Power_Cycle_Count", 100, 100, 0, 206),
				a(165, "Block_Erase_Count", 100, 100, 0, 9563),
				a(169, "Total_Bad_Blocks", 100, 100, 0, 199),
				a(170, "Grown_Bad_Blocks", 100, 100, 0, 4),
				a(171, "Program_Fail_Count", 100, 100, 0, 0),
				a(172, "Erase_Fail_Count", 100, 100, 0, 0),
				a(173, "Average_PE_Cycles_TLC", 100, 100, 0, 115),
				a(174, "Unexpected_Power_Loss", 100, 100, 0, 89),
				a(184, "End-to-End_Error", 100, 100, 0, 0),
				a(187, "Reported_Uncorrect", 100, 100, 0, 0),
				a(188, "Command_Timeout", 100, 100, 0, 0),
				a(199, "UDMA_CRC_Error_Count", 100, 100, 0, 0),
				a(230, "Media_Wearout_Indicator", 100, 100, 0, 29824638786336),
				a(232, "Available_Reservd_Space", 100, 100, 5, 98),
				a(244, "Temp_Throttle_Status", 0, 100, 0, 0),
			},
		},
	}
}

func TestWDBlueNaoReclamaDosQuatroSetores(t *testing.T) {
	// Quatro setores realocados com a reserva em 100 e o principio 2 em acao:
	// a reserva e o sinal, e ela esta intacta. Reclamar disso todo dia e o que
	// treina o usuario a ignorar alerta.
	_, alertas := Avaliar(comBlocos(wdBlue()), LimiaresPadrao())
	junto := strings.Join(Textos(alertas), " ")
	if strings.Contains(junto, "realocados") {
		t.Fatalf("ainda reclama dos setores parados: %v", alertas)
	}
}

func TestWDBlueAcusaAEnergia(t *testing.T) {
	// 89 de 206 desligamentos inesperados sao 43%: acima do 30% que a
	// especificacao chama de critico. E e do HOST, nao do disco.
	n, alertas := Avaliar(comBlocos(wdBlue()), LimiaresPadrao())
	if n != Critico {
		t.Fatalf("nivel = %d, alertas = %v", n, alertas)
	}
	junto := strings.Join(Textos(alertas), " ")
	if !strings.Contains(junto, "energia") || !strings.Contains(junto, "nobreak") {
		t.Fatalf("nao aponta a energia: %v", alertas)
	}
}

func TestMesmoDiscoNoAgenteAntigoReclama(t *testing.T) {
	// O mesmo disco, visto por um agente 5.0.0: sem tabela, sobra a regra
	// antiga de "um setor realocado ja avisa". E o que o usuario continua
	// vendo depois de atualizar so o cliente - e ele precisa saber por que.
	b := wdBlue()
	b.Smart = &metricas.Smart{Saude: "ok", Realocados: i(4), DesgastePercent: f(0)}
	n, alertas := Avaliar(comBlocos(b), LimiaresPadrao())
	if n != Aviso {
		t.Fatalf("nivel = %d", n)
	}
	junto := strings.Join(Textos(alertas), " ")
	if !strings.Contains(junto, "realocados") {
		t.Fatalf("alertas = %v", alertas)
	}
	if !strings.Contains(junto, "agente") {
		t.Fatalf("nao explica que o agente e antigo: %v", alertas)
	}
}
