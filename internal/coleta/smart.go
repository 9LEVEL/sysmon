package coleta

// Traducao da saida do smartctl para o contrato de fio.
//
// NVMe e SATA reportam saude com vocabularios completamente diferentes: um tem
// percentage_used e available_spare, o outro tem uma tabela de atributos
// numerados por fabricante. A traducao mora aqui, e nao no shell, porque aqui
// da para testar - o sysmon-smart.sh so precisa produzir JSON valido.
//
// O NVMe e traduzido para os MESMOS nomes canonicos da tabela ATA
// (Available_Spare, Power_Cycle_Count, ...) de proposito: assim
// internal/smart avalia os dois com as mesmas regras, em vez de manter duas
// arvores de decisao que divergem a cada limiar novo.

import (
	"encoding/json"
	"fmt"

	"sysmon/internal/metricas"
	"sysmon/internal/smart"
)

// docSmart e o subconjunto da saida do `smartctl -j -H -A -i` que o projeto
// usa. Campo ausente vira nil e cada regra decide o que fazer com isso; nada
// aqui inventa valor default.
type docSmart struct {
	Smartctl struct {
		ExitStatus int `json:"exit_status"`
		Messages   []struct {
			String   string `json:"string"`
			Severity string `json:"severity"`
		} `json:"messages"`
	} `json:"smartctl"`

	ModelName    string `json:"model_name"`
	ModelFamily  string `json:"model_family"`
	SerialNumber string `json:"serial_number"`

	SmartStatus struct {
		Passed *bool `json:"passed"`
	} `json:"smart_status"`

	PowerOnTime struct {
		Hours *int64 `json:"hours"`
	} `json:"power_on_time"`
	PowerCycleCount *int64 `json:"power_cycle_count"`

	Temperature struct {
		Current     *float64 `json:"current"`
		LifetimeMax *float64 `json:"lifetime_max"`
	} `json:"temperature"`

	NVMe *struct {
		PercentageUsed        *float64 `json:"percentage_used"`
		AvailableSpare        *float64 `json:"available_spare"`
		AvailableSpareThresh  *float64 `json:"available_spare_threshold"`
		MediaErrors           *int64   `json:"media_errors"`
		UnsafeShutdowns       *int64   `json:"unsafe_shutdowns"`
		PowerCycles           *int64   `json:"power_cycles"`
		PowerOnHours          *int64   `json:"power_on_hours"`
		Temperature           *float64 `json:"temperature"`
		WarningTempTime       *int64   `json:"warning_temp_time"`
		CriticalCompTime      *int64   `json:"critical_comp_time"`
		CriticalWarning       *int64   `json:"critical_warning"`
		NumErrLogEntries      *int64   `json:"num_err_log_entries"`
		DataUnitsWritten      *int64   `json:"data_units_written"`
		PercentUsedDeprecated *float64 `json:"percent_used"`
	} `json:"nvme_smart_health_information_log"`

	ATA struct {
		Table []struct {
			ID     int    `json:"id"`
			Name   string `json:"name"`
			Value  *int   `json:"value"`  // normalizado, 0-253
			Worst  *int   `json:"worst"`  // pior normalizado ja visto
			Thresh *int   `json:"thresh"` // o limite do fabricante
			Raw    struct {
				Value *int64 `json:"value"`
			} `json:"raw"`
		} `json:"table"`
	} `json:"ata_smart_attributes"`
}

// SmartDe normaliza a saida do smartctl que o timer gravou em extras.
func SmartDe(extras map[string]metricas.Extra, dev string) *metricas.Smart {
	bloco, ok := extras["smart"]
	if !ok {
		return nil
	}
	var todos map[string]json.RawMessage
	if err := json.Unmarshal(bloco.Dados, &todos); err != nil {
		return nil
	}
	bruto, ok := todos[dev]
	if !ok {
		return nil
	}

	var doc docSmart
	if err := json.Unmarshal(bruto, &doc); err != nil {
		return nil
	}

	s := &metricas.Smart{
		IdadeS:  bloco.IdadeS,
		Serial:  doc.SerialNumber,
		Familia: doc.ModelFamily,
	}
	s.ColetaOK, s.ErroColeta = coletaFalhou(doc)

	if doc.SmartStatus.Passed != nil {
		s.Saude = "falha"
		if *doc.SmartStatus.Passed {
			s.Saude = "ok"
		}
	}
	s.HorasLigado = doc.PowerOnTime.Hours
	s.CiclosEnergia = doc.PowerCycleCount
	s.TempC = doc.Temperature.Current
	s.TempMaxC = doc.Temperature.LifetimeMax

	if doc.NVMe != nil {
		smartNVMe(s, doc)
		return s
	}
	smartATA(s, doc)
	return s
}

// coletaFalhou distingue "o disco tem problema" de "nao consegui perguntar".
//
// Os tres primeiros bits do exit_status do smartctl sao erro de execucao
// (linha de comando, abrir o dispositivo, comando SMART recusado); do bit 3
// em diante sao achados sobre o disco, que e justamente o que queremos ver.
// Confundir os dois grupos faz o disco atras de um controlador RAID ficar
// eternamente sem alerta - indistinguivel de saudavel.
func coletaFalhou(doc docSmart) (ok bool, erro string) {
	if doc.Smartctl.ExitStatus&0x07 != 0 {
		if m := primeiraMensagem(doc); m != "" {
			return false, m
		}
		return false, fmt.Sprintf("smartctl saiu com status %d",
			doc.Smartctl.ExitStatus)
	}
	if len(doc.ATA.Table) == 0 && doc.NVMe == nil && doc.SmartStatus.Passed == nil {
		if m := primeiraMensagem(doc); m != "" {
			return false, m
		}
		return false, "smartctl nao devolveu atributos; se o disco esta atras " +
			"de um controlador RAID, e preciso o -d certo (ex.: -d megaraid,0)"
	}
	return true, ""
}

func primeiraMensagem(doc docSmart) string {
	for _, m := range doc.Smartctl.Messages {
		if m.String != "" {
			return m.String
		}
	}
	return ""
}

func smartNVMe(s *metricas.Smart, doc docSmart) {
	n := doc.NVMe
	s.DesgastePercent = n.PercentageUsed
	if s.DesgastePercent == nil {
		s.DesgastePercent = n.PercentUsedDeprecated
	}
	s.SpareRestante = n.AvailableSpare
	s.ErrosMidia = n.MediaErrors
	if s.HorasLigado == nil {
		s.HorasLigado = n.PowerOnHours
	}
	if s.CiclosEnergia == nil {
		s.CiclosEnergia = n.PowerCycles
	}
	s.DesligamentosSujos = n.UnsafeShutdowns
	if s.TempC == nil {
		s.TempC = n.Temperature
	}

	// O NVMe nao expoe "esta em throttling" como booleano. Expoe ha quantos
	// minutos passou de cada limite termico - qualquer valor acima de zero
	// significa que ja houve reducao de desempenho por temperatura.
	s.Throttle = maiorQueZero(n.WarningTempTime) || maiorQueZero(n.CriticalCompTime)
	if n.CriticalWarning != nil && *n.CriticalWarning&0x02 != 0 {
		s.Throttle = true
	}

	// A tabela sintetica: os mesmos nomes canonicos da ATA, para que as
	// regras nao precisem saber qual e o transporte.
	add := func(id int, nome string, valor *int, limiar *int, cru *int64) {
		if valor == nil && cru == nil {
			return
		}
		s.Atributos = append(s.Atributos, metricas.SmartAtributo{
			ID: id, Nome: nome, Valor: valor, Limiar: limiar, Cru: cru})
	}
	add(0, "Available_Spare", pct(n.AvailableSpare), pct(n.AvailableSpareThresh), nil)
	// Reported_Uncorrect e o nome ATA de "erro que o disco nao corrigiu e
	// reportou ao host". media_errors e exatamente isso no vocabulario NVMe.
	add(0, "Reported_Uncorrect", nil, nil, n.MediaErrors)
	add(0, "Power_Cycle_Count", nil, nil, n.PowerCycles)
	add(0, "Unsafe_Shutdown_Count", nil, nil, n.UnsafeShutdowns)
}

func smartATA(s *metricas.Smart, doc docSmart) {
	s.Atributos = make([]metricas.SmartAtributo, 0, len(doc.ATA.Table))
	for _, a := range doc.ATA.Table {
		s.Atributos = append(s.Atributos, metricas.SmartAtributo{
			ID: a.ID, Nome: a.Name, Valor: a.Value, Pior: a.Worst,
			Limiar: a.Thresh, Cru: a.Raw.Value})

		// Os tres que a ATA padroniza. Vem antes porque sao exatos: o papel
		// "realocados" tambem aceita Reallocated_Event_Count, que conta outra
		// coisa, e para o resumo queremos o setor realocado mesmo.
		switch a.ID {
		case 5: // Reallocated_Sector_Ct
			if s.Realocados == nil {
				s.Realocados = a.Raw.Value
			}
		case 9: // Power_On_Hours
			if s.HorasLigado == nil {
				s.HorasLigado = a.Raw.Value
			}
		case 12: // Power_Cycle_Count
			if s.CiclosEnergia == nil {
				s.CiclosEnergia = a.Raw.Value
			}
		}

		// O resto por NOME, sempre que houver um papel. Os ids nao sao confiaveis
		// fora dos poucos que a ATA padroniza: neste WD Blue de teste, o
		// Media_Wearout_Indicator e o id 230 e o 233 e NAND_GB_Written_TLC.
		// Ler o 233 como desgaste, que era o que este codigo fazia, reporta
		// o normalizado do contador errado - e num disco gasto isso vira
		// "0% usado" com a mesma cara de resposta certa.
		if papel, ok := smart.PapelDe(a.Name); ok {
			switch papel {
			case "desgaste_restante":
				// O normalizado conta a vida RESTANTE de 100 a 0; o desgaste
				// e o complemento.
				if a.Value != nil && s.DesgastePercent == nil {
					s.DesgastePercent = f64(float64(100 - *a.Value))
				}
			case "desgaste_usado":
				if a.Value != nil && s.DesgastePercent == nil {
					s.DesgastePercent = f64(float64(*a.Value))
				}
			case "reserva":
				if a.Value != nil && s.SpareRestante == nil {
					s.SpareRestante = f64(float64(*a.Value))
				}
			case "desligamento_sujo":
				if s.DesligamentosSujos == nil {
					s.DesligamentosSujos = a.Raw.Value
				}
			case "ciclos_energia":
				if s.CiclosEnergia == nil {
					s.CiclosEnergia = a.Raw.Value
				}
			case "realocados":
				if s.Realocados == nil {
					s.Realocados = a.Raw.Value
				}
			case "throttle":
				s.Throttle = a.Raw.Value != nil && *a.Raw.Value > 0
			}
		}
	}
}

func maiorQueZero(v *int64) bool { return v != nil && *v > 0 }

func pct(v *float64) *int {
	if v == nil {
		return nil
	}
	n := int(*v)
	return &n
}
