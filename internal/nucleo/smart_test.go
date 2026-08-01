package nucleo

import (
	"strings"
	"testing"

	"sysmon/internal/metricas"
	"sysmon/internal/smart"
)

func ii(v int) *int { return &v }

// disco monta um bloco como o agente >= 5.1 manda: coleta_ok afirmado e a
// tabela de atributos junto.
func disco(atributos ...metricas.SmartAtributo) metricas.Bloco {
	return metricas.Bloco{
		Dev: "sda", Tipo: "ssd",
		Smart: &metricas.Smart{
			ColetaOK: true, Saude: "ok", Serial: "X1", Atributos: atributos,
		},
	}
}

func comBlocos(bs ...metricas.Bloco) Estado {
	s := snap()
	s.Blocos = bs
	return est(s)
}

// ------------------------------------------------- compatibilidade de versao

func TestAgenteAntigoNaoViraColetaFalha(t *testing.T) {
	// Ate a v5.0 o agente mandava so o resumo: sem tabela, sem serial, sem
	// coleta_ok. Avaliar aquilo com as regras novas diria "coleta falhou" para
	// todo disco de toda frota que ainda nao atualizou o agente - um alerta
	// falso por disco, no dia do upgrade do cliente.
	b := metricas.Bloco{Dev: "sda", Smart: &metricas.Smart{
		Saude: "ok", DesgastePercent: f(20)}}
	if _, ok := LeituraSmart(b); ok {
		t.Fatal("aceitou resumo antigo como leitura completa")
	}
	if n, alertas := Avaliar(comBlocos(b), LimiaresPadrao()); n != OK {
		t.Fatalf("nivel = %d, alertas = %v", n, alertas)
	}
}

func TestAgenteNovoComColetaFalhaEAvaliado(t *testing.T) {
	// O outro lado da moeda: coleta_ok falso COM motivo e um agente novo
	// dizendo que nao alcancou o disco. Isso tem que virar alerta.
	b := metricas.Bloco{Dev: "sda", Smart: &metricas.Smart{
		ColetaOK: false, ErroColeta: "precisa de -d megaraid,0"}}
	l, ok := LeituraSmart(b)
	if !ok {
		t.Fatal("nao reconheceu o agente novo")
	}
	if l.ColetaOK {
		t.Fatal("perdeu o estado de falha")
	}
	n, alertas := Avaliar(comBlocos(b), LimiaresPadrao())
	if n != Aviso {
		t.Fatalf("nivel = %d, queria Aviso", n)
	}
	if !strings.Contains(strings.Join(Textos(alertas), " "), "megaraid") {
		t.Fatalf("perdeu o motivo: %v", alertas)
	}
}

// -------------------------------------------------------------- integracao

func TestRegraDeTaxaChegaAoAlerta(t *testing.T) {
	// A regra so existe porque o agente manda os deltas. Este teste liga as
	// duas pontas: contador que subiu 10 em 7 dias vira alerta critico.
	b := disco(metricas.SmartAtributo{
		ID: 5, Nome: "Reallocated_Sector_Ct", Cru: i(10),
		Delta24h: i(0), Delta7d: i(10), Delta30d: i(10), Base30d: i(0),
		Amostras: 30,
	})
	n, alertas := Avaliar(comBlocos(b), LimiaresPadrao())
	if n != Critico {
		t.Fatalf("nivel = %d, alertas = %v", n, alertas)
	}
	if !strings.Contains(strings.Join(Textos(alertas), " "), "sda") {
		t.Fatalf("alerta sem o disco: %v", alertas)
	}
}

func TestContadorParadoNaoAlerta(t *testing.T) {
	// O mesmo contador em 200, mas sem crescer ha um mes: e um disco velho que
	// funciona, e alertar sobre ele todo dia treina o usuario a ignorar alerta.
	b := disco(metricas.SmartAtributo{
		ID: 5, Nome: "Reallocated_Sector_Ct", Cru: i(200),
		Delta24h: i(0), Delta7d: i(0), Delta30d: i(0), Base30d: i(200),
		Amostras: 180,
	})
	if n, alertas := Avaliar(comBlocos(b), LimiaresPadrao()); n != OK {
		t.Fatalf("nivel = %d, alertas = %v", n, alertas)
	}
}

func TestCaboRuimNaoMandaTrocarODisco(t *testing.T) {
	// Erro de CRC e cabo ou porta. Se o alerta nao disser isso, alguem troca
	// um disco bom e o problema volta na semana seguinte.
	b := disco(metricas.SmartAtributo{
		ID: 199, Nome: "UDMA_CRC_Error_Count", Cru: i(12),
		Delta24h: i(0), Delta7d: i(3), Delta30d: i(3), Base30d: i(9),
		Amostras: 30,
	})
	n, alertas := Avaliar(comBlocos(b), LimiaresPadrao())
	if n != Aviso {
		t.Fatalf("nivel = %d", n)
	}
	junto := strings.Join(Textos(alertas), " ")
	if !strings.Contains(junto, "cabo") {
		t.Fatalf("alerta nao aponta o cabo: %v", alertas)
	}
}

func TestTemperaturaNaoSaiDuplicada(t *testing.T) {
	// Duas camadas sabem avaliar temperatura: lim.TempDisco, que o usuario
	// configura, e a regra do pacote smart, que conhece a diferenca entre HDD
	// e SSD. Uma linha por disco - senao o rodape mostra o mesmo problema duas
	// vezes com numeros diferentes.
	lim := LimiaresPadrao()
	b := disco()
	b.TempC = f(75)
	_, alertas := Avaliar(comBlocos(b), lim)
	quentes := 0
	for _, a := range alertas {
		if strings.Contains(a.Texto, "75") {
			quentes++
		}
	}
	if quentes != 1 {
		t.Fatalf("%d alertas de temperatura: %v", quentes, alertas)
	}
}

func TestThrottleEMaximaHistoricaAparecem(t *testing.T) {
	// Sao os dois sinais termicos que a camada antiga nao tinha como ver: a
	// leitura atual pode estar fria e o disco ainda ter passado da conta.
	b := disco()
	b.TempC = f(40)
	b.Smart.Throttle = true
	b.Smart.TempMaxC = f(72)
	n, alertas := Avaliar(comBlocos(b), LimiaresPadrao())
	if n != Aviso {
		t.Fatalf("nivel = %d, alertas = %v", n, alertas)
	}
	junto := strings.Join(Textos(alertas), " ")
	if !strings.Contains(junto, "throttling") {
		t.Errorf("perdeu o throttling: %v", alertas)
	}
	if !strings.Contains(junto, "72") {
		t.Errorf("perdeu a maxima historica: %v", alertas)
	}
}

func TestInfoNaoViraAlerta(t *testing.T) {
	// A especificacao separa Info de Aviso justamente para haver um degrau que
	// se registra sem acordar ninguem. Promover apagaria a distincao.
	b := disco(metricas.SmartAtributo{
		ID: 232, Nome: "Available_Reservd_Space", Valor: ii(85),
	})
	v, ok := VereditoSmart(b, LimiaresPadrao())
	if !ok {
		t.Fatal("nao avaliou")
	}
	if v.Dispositivo() != smart.Info {
		t.Fatalf("severidade = %d, queria Info", v.Dispositivo())
	}
	if n, alertas := Avaliar(comBlocos(b), LimiaresPadrao()); n != OK {
		t.Fatalf("nivel = %d, alertas = %v", n, alertas)
	}
}

func TestLimiarDoConfigChegaNasRegras(t *testing.T) {
	// A arvore de limiares do smart entra por Limiares, entao o config.json
	// alcanca a especificacao inteira sem codigo novo.
	lim := LimiaresPadrao()
	lim.Smart.Temperatura.SSD.Aviso = 45
	lim.Smart.Temperatura.SSD.Critico = 50
	lim.Smart.Temperatura.SSD.Info = 40

	b := disco()
	b.Smart.TempMaxC = f(52)
	v, _ := VereditoSmart(b, lim)
	if v.Dispositivo() != smart.Aviso {
		t.Fatalf("severidade = %d; o limiar do config nao foi usado", v.Dispositivo())
	}
}

// -------------------------------------------------------------- config.json

func TestArvoreDoSmartVemDoArquivo(t *testing.T) {
	// A especificacao inteira e configuravel, com as mesmas chaves dela. Sem
	// isto o `alertas.smart` documentado no README nao existiria.
	bruto := map[string]any{"alertas": map[string]any{
		"smart": map[string]any{
			"temperature": map[string]any{
				"ssd": map[string]any{"warn": 45.0, "critical": 50.0},
			},
		},
	}}
	lim := LimiaresDe(bruto)
	if lim.Smart.Temperatura.SSD.Aviso != 45 {
		t.Fatalf("aviso = %v", lim.Smart.Temperatura.SSD.Aviso)
	}
	// E o resto do padrao continua de pe, campo a campo.
	if lim.Smart.Temperatura.HDD.Critico != 0 {
		t.Errorf("HDD nao devia vir preenchido do arquivo: %+v", lim.Smart.Temperatura.HDD)
	}
	b := metricas.Bloco{Dev: "sda", Tipo: "hdd",
		Smart: &metricas.Smart{ColetaOK: true, TempMaxC: f(60)}}
	if v, _ := VereditoSmart(b, lim); v.Dispositivo() == smart.OK {
		t.Error("60 C num HDD passou batido; o padrao de HDD se perdeu")
	}
}

func TestSalvarNaoApagaOQueNaoConhece(t *testing.T) {
	// A janela salva o config quando o usuario mexe nos limiares pelo `!`.
	// Se isso reescrevesse a chave `alertas` inteira, a arvore do smart que
	// alguem ajustou a mao sumiria sem aviso.
	c := Config{
		Bruto: map[string]any{"alertas": map[string]any{
			"smart": map[string]any{"wear": map[string]any{"warn_pct": 70.0}},
		}},
		Limiares: LimiaresPadrao(),
	}
	saida := c.ComoBruto()
	alertas, ok := saida["alertas"].(map[string]any)
	if !ok {
		t.Fatalf("alertas = %T", saida["alertas"])
	}
	if _, tem := alertas["smart"]; !tem {
		t.Fatalf("a arvore do smart sumiu ao salvar: %v", alertas)
	}
	if _, tem := alertas["ram"]; !tem {
		t.Fatalf("os limiares normais sumiram: %v", alertas)
	}
}
