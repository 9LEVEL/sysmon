package coleta

import (
	"testing"

	"sysmon/internal/metricas"
)

func porNome(s *metricas.Smart, nome string) *metricas.SmartAtributo {
	for i := range s.Atributos {
		if s.Atributos[i].Nome == nome {
			return &s.Atributos[i]
		}
	}
	return nil
}

func TestTabelaCompletaChegaInteira(t *testing.T) {
	// As regras de internal/smart casam por NOME e precisam de value, worst,
	// thresh e raw juntos: e a combinacao deles que distingue "o fabricante
	// declarou falha" de "o contador subiu um pouco".
	f := fake(t, map[string]string{
		"/run/sysmon/smart.json": `{"sda":{
			"model_name":"WDC WDS240G2G0A-00JH30",
			"model_family":"Western Digital Blue",
			"serial_number":"WD-ABC123",
			"smart_status":{"passed":true},
			"ata_smart_attributes":{"table":[
				{"id":232,"name":"Available_Reservd_Space","value":98,"worst":98,
				 "thresh":10,"raw":{"value":0}},
				{"id":170,"name":"Grown_Bad_Blocks","value":100,"worst":100,
				 "thresh":0,"raw":{"value":4}}]}}}`,
	})
	s := SmartDe(f.Extras(), "sda")
	if s == nil {
		t.Fatal("nao leu")
	}
	if s.Serial != "WD-ABC123" {
		t.Errorf("serial = %q", s.Serial)
	}
	if s.Familia != "Western Digital Blue" {
		t.Errorf("familia = %q", s.Familia)
	}
	if len(s.Atributos) != 2 {
		t.Fatalf("atributos = %d", len(s.Atributos))
	}
	a := porNome(s, "Available_Reservd_Space")
	if a == nil {
		t.Fatal("perdeu a reserva")
	}
	if a.Valor == nil || *a.Valor != 98 || a.Limiar == nil || *a.Limiar != 10 {
		t.Errorf("value/thresh: %+v", a)
	}
	if a.Pior == nil || *a.Pior != 98 {
		t.Errorf("worst: %+v", a)
	}
}

func TestNomeVendorSpecificChegaSemTraducao(t *testing.T) {
	// O mesmo id 170 e reserva num Intel e bloco crescido num WD. Traduzir por
	// id aqui apagaria a informacao que o smartctl ja resolveu pela drivedb.
	f := fake(t, map[string]string{
		"/run/sysmon/smart.json": `{"sda":{"smart_status":{"passed":true},
			"ata_smart_attributes":{"table":[
				{"id":170,"name":"Available_Reservd_Space","value":95,"raw":{"value":0}}]}}}`,
	})
	s := SmartDe(f.Extras(), "sda")
	if a := porNome(s, "Available_Reservd_Space"); a == nil || a.ID != 170 {
		t.Fatalf("atributos = %+v", s.Atributos)
	}
}

func TestColetaFalhaVemMarcadaComOMotivo(t *testing.T) {
	// Disco atras de controladora RAID: o smartctl responde, mas nao alcanca a
	// midia. Sem marcar isso, o disco fica sem alerta para sempre - que e
	// indistinguivel de saudavel.
	f := fake(t, map[string]string{
		"/run/sysmon/smart.json": `{"sda":{
			"smartctl":{"exit_status":2,"messages":[
				{"string":"Smartctl open device: /dev/sda failed: DELL or MegaRaid controller","severity":"error"}]}}}`,
	})
	s := SmartDe(f.Extras(), "sda")
	if s == nil {
		t.Fatal("nao leu")
	}
	if s.ColetaOK {
		t.Error("exit_status 2 deveria marcar coleta falha")
	}
	if s.ErroColeta == "" {
		t.Error("perdeu a mensagem do smartctl")
	}
}

func TestAchadoNoDiscoNaoEFalhaDeColeta(t *testing.T) {
	// Do bit 3 para cima o exit_status fala do DISCO, nao da execucao: e
	// exatamente o caso que queremos ver, e nao pode virar "nao consegui ler".
	f := fake(t, map[string]string{
		"/run/sysmon/smart.json": `{"sda":{
			"smartctl":{"exit_status":8},
			"smart_status":{"passed":false},
			"ata_smart_attributes":{"table":[
				{"id":5,"name":"Reallocated_Sector_Ct","value":50,"raw":{"value":800}}]}}}`,
	})
	s := SmartDe(f.Extras(), "sda")
	if !s.ColetaOK {
		t.Fatalf("virou falha de coleta: %q", s.ErroColeta)
	}
	if s.Saude != "falha" {
		t.Errorf("saude = %q", s.Saude)
	}
}

func TestRespostaVaziaNaoEColetaOK(t *testing.T) {
	f := fake(t, map[string]string{
		"/run/sysmon/smart.json": `{"sda":{}}`,
	})
	s := SmartDe(f.Extras(), "sda")
	if s.ColetaOK {
		t.Fatal("resposta sem atributo nenhum passou como coleta boa")
	}
	if s.ErroColeta == "" {
		t.Fatal("sem explicacao do que fazer")
	}
}

func TestNVMeViraOsMesmosNomesDaATA(t *testing.T) {
	// O NVMe nao tem tabela de atributos. Traduzir para os nomes canonicos e o
	// que permite uma arvore de regras so - em vez de duas que divergem a cada
	// limiar novo.
	f := fake(t, map[string]string{
		"/run/sysmon/smart.json": `{"nvme0n1":{
			"serial_number":"S4X1",
			"smart_status":{"passed":true},
			"nvme_smart_health_information_log":{
				"percentage_used":7,"available_spare":100,
				"available_spare_threshold":10,"media_errors":3,
				"unsafe_shutdowns":39,"power_cycles":90,
				"temperature":41,"warning_temp_time":0}}}`,
	})
	s := SmartDe(f.Extras(), "nvme0n1")
	if a := porNome(s, "Available_Spare"); a == nil || a.Valor == nil ||
		*a.Valor != 100 || a.Limiar == nil || *a.Limiar != 10 {
		t.Errorf("reserva: %+v", a)
	}
	if a := porNome(s, "Reported_Uncorrect"); a == nil || a.Cru == nil || *a.Cru != 3 {
		t.Errorf("media_errors deveria virar erro incorrigivel: %+v", a)
	}
	if s.DesligamentosSujos == nil || *s.DesligamentosSujos != 39 {
		t.Errorf("desligamentos sujos: %v", s.DesligamentosSujos)
	}
	if s.CiclosEnergia == nil || *s.CiclosEnergia != 90 {
		t.Errorf("ciclos: %v", s.CiclosEnergia)
	}
	if s.TempC == nil || *s.TempC != 41 {
		t.Errorf("temp: %v", s.TempC)
	}
	if s.Throttle {
		t.Error("warning_temp_time 0 nao e throttling")
	}
}

func TestThrottleNVMeVemDoTempoAcimaDoLimite(t *testing.T) {
	// O NVMe nao expoe um booleano; expoe ha quantos minutos passou do limite.
	f := fake(t, map[string]string{
		"/run/sysmon/smart.json": `{"nvme0n1":{"smart_status":{"passed":true},
			"nvme_smart_health_information_log":{"warning_temp_time":12}}}`,
	})
	if s := SmartDe(f.Extras(), "nvme0n1"); !s.Throttle {
		t.Fatal("nao detectou throttling")
	}
}

func TestDesligamentoSujoSATASaiPeloNome(t *testing.T) {
	// O id varia por fabricante (192 num, 174 noutro); o nome nao.
	f := fake(t, map[string]string{
		"/run/sysmon/smart.json": `{"sda":{"smart_status":{"passed":true},
			"ata_smart_attributes":{"table":[
				{"id":174,"name":"Unexpect_Power_Loss_Ct","value":100,"raw":{"value":39}},
				{"id":12,"name":"Power_Cycle_Count","value":100,"raw":{"value":90}}]}}}`,
	})
	s := SmartDe(f.Extras(), "sda")
	if s.DesligamentosSujos == nil || *s.DesligamentosSujos != 39 {
		t.Errorf("sujos: %v", s.DesligamentosSujos)
	}
	if s.CiclosEnergia == nil || *s.CiclosEnergia != 90 {
		t.Errorf("ciclos: %v", s.CiclosEnergia)
	}
}

func TestTemperaturaDoSmartctlServeDeReserva(t *testing.T) {
	// SATA sem o modulo drivetemp nao tem temperatura no sysfs; o smartctl le
	// pelo protocolo do proprio disco.
	f := fake(t, map[string]string{
		"/run/sysmon/smart.json": `{"sda":{"smart_status":{"passed":true},
			"temperature":{"current":38,"lifetime_max":65}}}`,
	})
	s := SmartDe(f.Extras(), "sda")
	if s.TempC == nil || *s.TempC != 38 {
		t.Errorf("temp = %v", s.TempC)
	}
	if s.TempMaxC == nil || *s.TempMaxC != 65 {
		t.Errorf("maxima = %v", s.TempMaxC)
	}
}

func TestDesgasteSaiPeloNomeENaoPeloID(t *testing.T) {
	// Tabela real de um WDC WDS240G2G0A: o Media_Wearout_Indicator e o id 230,
	// e o 233 e NAND_GB_Written_TLC. Ler "o id 233" como desgaste - que era o
	// que este codigo fazia - reporta o normalizado do contador errado, e num
	// disco gasto isso sai como "0% usado" com cara de resposta certa.
	f := fake(t, map[string]string{
		"/run/sysmon/smart.json": `{"sda":{"smart_status":{"passed":true},
			"ata_smart_attributes":{"table":[
				{"id":230,"name":"Media_Wearout_Indicator","value":40,"raw":{"value":29824638786336}},
				{"id":233,"name":"NAND_GB_Written_TLC","value":100,"raw":{"value":28560}}]}}}`,
	})
	s := SmartDe(f.Extras(), "sda")
	if s.DesgastePercent == nil || *s.DesgastePercent != 60 {
		t.Fatalf("desgaste = %v, queria 60 (100-40 do atributo certo)",
			s.DesgastePercent)
	}
}

func TestNomeDoDesligamentoSujoDesteWD(t *testing.T) {
	// Este WD reporta "Unexpected_Power_Loss", sem o sufixo _Count nem _Ct.
	// Sem o nome exato, a regra de saude do host nunca dispara no disco de
	// exemplo da propria especificacao.
	f := fake(t, map[string]string{
		"/run/sysmon/smart.json": `{"sda":{"smart_status":{"passed":true},
			"ata_smart_attributes":{"table":[
				{"id":174,"name":"Unexpected_Power_Loss","value":100,"raw":{"value":89}},
				{"id":12,"name":"Power_Cycle_Count","value":100,"raw":{"value":206}}]}}}`,
	})
	s := SmartDe(f.Extras(), "sda")
	if s.DesligamentosSujos == nil || *s.DesligamentosSujos != 89 {
		t.Fatalf("desligamentos sujos = %v, queria 89", s.DesligamentosSujos)
	}
}

func TestThrottleSATASaiDoAtributo(t *testing.T) {
	f := fake(t, map[string]string{
		"/run/sysmon/smart.json": `{"sda":{"smart_status":{"passed":true},
			"ata_smart_attributes":{"table":[
				{"id":244,"name":"Temp_Throttle_Status","value":0,"raw":{"value":3}}]}}}`,
	})
	if s := SmartDe(f.Extras(), "sda"); !s.Throttle {
		t.Fatal("nao detectou throttling")
	}
}
