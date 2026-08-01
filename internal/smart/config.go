package smart

// Configuracao dos limiares.
//
// A especificacao descreve isto em YAML. Aqui e uma struct com as mesmas
// chaves e a mesma forma: o projeto inteiro nao tem parser de YAML, e trazer
// um custaria uma dependencia para ler um arquivo que quase ninguem edita.
// Converter de um YAML com esta forma para esta struct e trivial se um dia
// fizer falta.
//
// Campo zerado herda o padrao. E o que permite mexer so no limiar de
// temperatura sem copiar a arvore inteira para o config.json.

type Par struct {
	Aviso   int64 `json:"warn"`
	Critico int64 `json:"critical"`
}

type ConfigReserva struct {
	OKMin               int `json:"ok_min_normalized"`
	InfoMin             int `json:"info_min_normalized"`
	AvisoMin            int `json:"warn_min_normalized"`
	MargemAcimaDoLimiar int `json:"critical_margin_above_vendor_thresh"`
}

type ConfigCrescimento struct {
	Aviso7d          int64   `json:"warn_count_7d"`
	Critico24h       int64   `json:"critical_count_24h"`
	Critico7d        int64   `json:"critical_count_7d"`
	DobrarJanelaDias int     `json:"critical_double_window_days"`
	DobrarBaseMinima int64   `json:"critical_double_min_base"`
	FatorAceleracao  float64 `json:"warn_acceleration_factor"`
}

type ConfigRazao struct {
	Info    float64 `json:"info"`
	Aviso   float64 `json:"warn"`
	Critico float64 `json:"critical"`
}

type ConfigDesgaste struct {
	Info       float64          `json:"info_pct"`
	Aviso      float64          `json:"warn_pct"`
	Critico    float64          `json:"critical_pct"`
	CiclosNAND map[string]int64 `json:"nand_cycles_default"`
}

type ConfigFaixaTemp struct {
	Info         float64 `json:"info"`
	Aviso        float64 `json:"warn"`
	Critico      float64 `json:"critical"`
	CriticoBaixo float64 `json:"critical_low"`
}

type ConfigTemperatura struct {
	SSD          ConfigFaixaTemp `json:"ssd"`
	HDD          ConfigFaixaTemp `json:"hdd"`
	OffsetMaxima int             `json:"historic_max_severity_offset"`
}

type ConfigRuido struct {
	LeiturasParaSubir int   `json:"hysteresis_raise_consecutive_reads"`
	MargemParaLimpar  int   `json:"hysteresis_clear_margin"`
	DebounceAviso     int   `json:"debounce_hours_warn"`
	DebounceCritico   int   `json:"debounce_hours_critical"`
	RetencaoDias      int   `json:"history_retention_days"`
	IntervaloMinutos  int   `json:"poll_interval_minutes"`
	_                 int64 // reservado
}

type Config struct {
	Reserva         ConfigReserva     `json:"reserve_space"`
	Crescimento     ConfigCrescimento `json:"growth_rate"`
	Imediatos       map[string]Par    `json:"immediate"`
	RazaoBlocos     ConfigRazao       `json:"bad_blocks_ratio"`
	RealocadosBruto Par               `json:"reallocated_raw"`
	Desgaste        ConfigDesgaste    `json:"wear"`
	Temperatura     ConfigTemperatura `json:"temperature"`
	SaudeHost       ConfigRazao       `json:"host_health"`
	Ruido           ConfigRuido       `json:"noise_control"`
}

// Padrao devolve os limiares da especificacao.
func Padrao() Config {
	return Config{
		Reserva: ConfigReserva{OKMin: 90, InfoMin: 80, AvisoMin: 50,
			MargemAcimaDoLimiar: 10},
		Crescimento: ConfigCrescimento{Aviso7d: 3, Critico24h: 5, Critico7d: 10,
			DobrarJanelaDias: 30, DobrarBaseMinima: 4, FatorAceleracao: 3},
		Imediatos: map[string]Par{
			"current_pending_sector": {Aviso: 1, Critico: 10},
			"offline_uncorrectable":  {Critico: 1},
			"reported_uncorrect":     {Critico: 1},
			"end_to_end_error":       {Critico: 1},
			"command_timeout":        {Aviso: 10, Critico: 100},
			"program_fail_count":     {Aviso: 1},
			"erase_fail_count":       {Aviso: 1},
		},
		RazaoBlocos:     ConfigRazao{Info: 0.05, Aviso: 0.20, Critico: 0.50},
		RealocadosBruto: Par{Aviso: 9, Critico: 41},
		Desgaste: ConfigDesgaste{Info: 70, Aviso: 85, Critico: 95,
			CiclosNAND: map[string]int64{"slc": 50000, "mlc": 3000,
				"tlc": 1000, "qlc": 500}},
		Temperatura: ConfigTemperatura{
			SSD:          ConfigFaixaTemp{Info: 50, Aviso: 60, Critico: 70},
			HDD:          ConfigFaixaTemp{Info: 40, Aviso: 45, Critico: 55, CriticoBaixo: 15},
			OffsetMaxima: -1,
		},
		SaudeHost: ConfigRazao{Info: 0.05, Aviso: 0.15, Critico: 0.30},
		Ruido: ConfigRuido{LeiturasParaSubir: 2, MargemParaLimpar: 5,
			DebounceAviso: 24, DebounceCritico: 6, RetencaoDias: 180,
			IntervaloMinutos: 60},
	}
}

// ComPadroes preenche o que veio zerado, CAMPO A CAMPO.
//
// A heranca precisa ser por campo e nao por sub-arvore: quem escreve
// {"smart":{"temperature":{"ssd":{"warn":55}}}} no config.json quer mudar um
// numero, nao ficar sem os limiares de HDD e sem o resto da faixa de SSD.
// Herdar em bloco faria exatamente isso, em silencio.
//
// O preco e que zero nao pode ser um valor escolhido - nao ha como distinguir
// "nao preenchi" de "quis zero" num int vindo de JSON. Nenhum limiar desta
// especificacao tem zero como valor util, com uma excecao anotada abaixo.
func (c Config) ComPadroes() Config {
	p := Padrao()

	c.Reserva.OKMin = ouI(c.Reserva.OKMin, p.Reserva.OKMin)
	c.Reserva.InfoMin = ouI(c.Reserva.InfoMin, p.Reserva.InfoMin)
	c.Reserva.AvisoMin = ouI(c.Reserva.AvisoMin, p.Reserva.AvisoMin)
	c.Reserva.MargemAcimaDoLimiar = ouI(c.Reserva.MargemAcimaDoLimiar,
		p.Reserva.MargemAcimaDoLimiar)

	c.Crescimento.Aviso7d = ou64(c.Crescimento.Aviso7d, p.Crescimento.Aviso7d)
	c.Crescimento.Critico24h = ou64(c.Crescimento.Critico24h, p.Crescimento.Critico24h)
	c.Crescimento.Critico7d = ou64(c.Crescimento.Critico7d, p.Crescimento.Critico7d)
	c.Crescimento.DobrarJanelaDias = ouI(c.Crescimento.DobrarJanelaDias,
		p.Crescimento.DobrarJanelaDias)
	c.Crescimento.DobrarBaseMinima = ou64(c.Crescimento.DobrarBaseMinima,
		p.Crescimento.DobrarBaseMinima)
	c.Crescimento.FatorAceleracao = ouF(c.Crescimento.FatorAceleracao,
		p.Crescimento.FatorAceleracao)

	// Mapa herda chave a chave: mexer no limiar de command_timeout nao pode
	// desligar a regra de setor pendente.
	//
	// O resultado e um mapa NOVO. Escrever no do chamador funcionaria - os
	// defaults sao sempre os mesmos - mas deixaria Avaliar, que e documentada
	// como funcao pura, alterando a configuracao que recebeu.
	imediatos := make(map[string]Par, len(p.Imediatos))
	for k, v := range p.Imediatos {
		atual, tem := c.Imediatos[k]
		if !tem {
			imediatos[k] = v
			continue
		}
		imediatos[k] = Par{Aviso: ou64(atual.Aviso, v.Aviso),
			Critico: ou64(atual.Critico, v.Critico)}
	}
	for k, v := range c.Imediatos {
		if _, tem := imediatos[k]; !tem {
			imediatos[k] = v
		}
	}
	c.Imediatos = imediatos

	c.RazaoBlocos = razaoComPadrao(c.RazaoBlocos, p.RazaoBlocos)
	c.SaudeHost = razaoComPadrao(c.SaudeHost, p.SaudeHost)

	c.RealocadosBruto.Aviso = ou64(c.RealocadosBruto.Aviso, p.RealocadosBruto.Aviso)
	c.RealocadosBruto.Critico = ou64(c.RealocadosBruto.Critico, p.RealocadosBruto.Critico)

	c.Desgaste.Info = ouF(c.Desgaste.Info, p.Desgaste.Info)
	c.Desgaste.Aviso = ouF(c.Desgaste.Aviso, p.Desgaste.Aviso)
	c.Desgaste.Critico = ouF(c.Desgaste.Critico, p.Desgaste.Critico)
	ciclos := make(map[string]int64, len(p.Desgaste.CiclosNAND))
	for k, v := range p.Desgaste.CiclosNAND {
		ciclos[k] = v
	}
	for k, v := range c.Desgaste.CiclosNAND {
		if v != 0 {
			ciclos[k] = v
		}
	}
	c.Desgaste.CiclosNAND = ciclos

	c.Temperatura.SSD = faixaComPadrao(c.Temperatura.SSD, p.Temperatura.SSD)
	c.Temperatura.HDD = faixaComPadrao(c.Temperatura.HDD, p.Temperatura.HDD)
	// A excecao: o offset da maxima historica e negativo por natureza (o pico
	// de seis meses atras conta um degrau abaixo do de agora), entao zero aqui
	// so pode ser "nao preenchi".
	c.Temperatura.OffsetMaxima = ouI(c.Temperatura.OffsetMaxima,
		p.Temperatura.OffsetMaxima)

	c.Ruido.LeiturasParaSubir = ouI(c.Ruido.LeiturasParaSubir, p.Ruido.LeiturasParaSubir)
	c.Ruido.MargemParaLimpar = ouI(c.Ruido.MargemParaLimpar, p.Ruido.MargemParaLimpar)
	c.Ruido.DebounceAviso = ouI(c.Ruido.DebounceAviso, p.Ruido.DebounceAviso)
	c.Ruido.DebounceCritico = ouI(c.Ruido.DebounceCritico, p.Ruido.DebounceCritico)
	c.Ruido.RetencaoDias = ouI(c.Ruido.RetencaoDias, p.Ruido.RetencaoDias)
	c.Ruido.IntervaloMinutos = ouI(c.Ruido.IntervaloMinutos, p.Ruido.IntervaloMinutos)

	return c
}

func razaoComPadrao(c, p ConfigRazao) ConfigRazao {
	return ConfigRazao{Info: ouF(c.Info, p.Info), Aviso: ouF(c.Aviso, p.Aviso),
		Critico: ouF(c.Critico, p.Critico)}
}

func faixaComPadrao(c, p ConfigFaixaTemp) ConfigFaixaTemp {
	return ConfigFaixaTemp{
		Info: ouF(c.Info, p.Info), Aviso: ouF(c.Aviso, p.Aviso),
		Critico: ouF(c.Critico, p.Critico),
		// CriticoBaixo so existe para HDD; zero no SSD e ausencia legitima e o
		// padrao tambem e zero, entao o resultado e o mesmo.
		CriticoBaixo: ouF(c.CriticoBaixo, p.CriticoBaixo),
	}
}

func ouI(v, padrao int) int {
	if v == 0 {
		return padrao
	}
	return v
}
func ou64(v, padrao int64) int64 {
	if v == 0 {
		return padrao
	}
	return v
}
func ouF(v, padrao float64) float64 {
	if v == 0 {
		return padrao
	}
	return v
}
