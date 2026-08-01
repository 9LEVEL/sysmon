// Package nucleo tem o que todos os clientes compartilham: configuracao,
// polling dos agentes e a regra de severidade.
//
// A regra de "isso merece sua atencao" mora aqui, num lugar so. Enquanto ela
// estava duplicada entre telas, o icone da bandeja e a tabela do terminal
// podiam discordar sobre o mesmo host - e discordaram.
package nucleo

import (
	"encoding/json"

	"sysmon/internal/smart"
)

// Severidade. A ordem importa: o pior nivel de uma frota e o maximo, e
// comparacao numerica e o que faz isso funcionar.
const (
	OK = iota
	Aviso
	Critico
	Offline
)

var NomeNivel = map[int]string{
	OK: "ok", Aviso: "aviso", Critico: "critico", Offline: "offline",
}

// Par e (aviso, critico) para uma medida.
type Par struct {
	Aviso   float64 `json:"0"`
	Critico float64 `json:"1"`
}

// Limiares diz onde cada medida vira aviso e onde vira critico.
//
// A fracao de temperatura da CPU e sobre o valor CRITICO que o proprio sensor
// reporta: numa CPU com crit 100C, 0.75 significa aviso em 75C. E o que faz o
// mesmo limiar servir para hardwares com limites diferentes. O par fixo so
// entra quando o sensor nao informa o critico dele.
type Limiares struct {
	TempFrac  Par `json:"temp_frac"`
	TempFixa  Par `json:"temp_fixa"`
	Disco     Par `json:"disco"`
	Inodes    Par `json:"inodes"`
	Thinpool  Par `json:"thinpool"`
	RAM       Par `json:"ram"`
	TempDisco Par `json:"temp_disco"`
	Desgaste  Par `json:"desgaste"`
	PSI       Par `json:"psi"`

	// Particoes de tamanho fixo cujo percentual nao diz nada util: /boot
	// enche de kernel antigo e a ESP vive quase cheia por natureza. Alertar
	// nelas ensina o usuario a ignorar alerta.
	IgnorarMounts []string `json:"ignorar_mounts"`

	// Um unico setor realocado ja e midia se degradando.
	RealocadosAviso int64 `json:"realocados_aviso"`

	// Os limiares da especificacao SMART. Zerado herda o padrao campo a
	// campo, entao da para mexer so na temperatura sem copiar a arvore
	// inteira para o config.json.
	Smart smart.Config `json:"smart"`

	// O que o usuario ja viu e aceitou. Mora aqui, junto dos limiares, porque
	// e a mesma pergunta: o que esta frota considera problema. "89
	// desligamentos sujos no pve, e eu sei disso" e uma resposta a ela, tanto
	// quanto "avise a partir de 90% de RAM".
	//
	// A consequencia pratica de morar aqui e valer em toda parte sem que
	// nenhuma assinatura mude - a janela, o terminal e a bandeja recebem
	// Limiares e passam a respeitar o reconhecimento sozinhos.
	Reconhecidos Reconhecimentos `json:"reconhecidos,omitempty"`

	// Multiplo do intervalo de coleta a partir do qual o dado e velho demais.
	IdadeFator float64 `json:"idade_fator"`
}

// Campo descreve um limiar configuravel para a tela de alertas.
type Campo struct {
	Nome    string
	Rotulo  string
	Unidade string // frac | c | pct
	Ler     func(*Limiares) *Par
}

// Campos e a fonte unica da tela de configuracao de alertas: acrescentar um
// limiar aqui o faz aparecer na interface sem mexer na interface.
var Campos = []Campo{
	{"temp_frac", "temperatura da cpu (fracao do critico do sensor)", "frac",
		func(l *Limiares) *Par { return &l.TempFrac }},
	{"temp_fixa", "temperatura da cpu sem critico no sensor (°C)", "c",
		func(l *Limiares) *Par { return &l.TempFixa }},
	{"temp_disco", "temperatura de disco (°C)", "c",
		func(l *Limiares) *Par { return &l.TempDisco }},
	{"ram", "uso de memoria (%)", "pct",
		func(l *Limiares) *Par { return &l.RAM }},
	{"disco", "uso de filesystem (%)", "pct",
		func(l *Limiares) *Par { return &l.Disco }},
	{"inodes", "uso de inodes (%)", "pct",
		func(l *Limiares) *Par { return &l.Inodes }},
	{"thinpool", "thin pool LVM (%)", "pct",
		func(l *Limiares) *Par { return &l.Thinpool }},
	{"desgaste", "vida consumida do ssd (%)", "pct",
		func(l *Limiares) *Par { return &l.Desgaste }},
	{"psi", "pressao PSI (%)", "pct",
		func(l *Limiares) *Par { return &l.PSI }},
}

// LimiaresPadrao sao os valores que ja valiam antes de isso ser configuravel.
func LimiaresPadrao() Limiares {
	return Limiares{
		TempFrac:        Par{0.75, 0.90},
		TempFixa:        Par{70, 85},
		Disco:           Par{80, 90},
		Inodes:          Par{90, 97},
		Thinpool:        Par{80, 90},
		RAM:             Par{90, 97},
		TempDisco:       Par{60, 70},
		Desgaste:        Par{80, 90},
		PSI:             Par{40, 70},
		IgnorarMounts:   []string{"/boot", "/boot/efi"},
		RealocadosAviso: 1,
		IdadeFator:      4,
	}
}

// LimiaresDe le do config, ignorando entrada malformada em vez de quebrar.
//
// Config quebrado nao pode derrubar o monitoramento: um par invalido volta ao
// padrao e o resto segue. Quem edita config.json a mao erra, e errar nao pode
// custar a tela inteira.
func LimiaresDe(bruto map[string]any) Limiares {
	lim := LimiaresPadrao()
	alertas, _ := bruto["alertas"].(map[string]any)
	for _, c := range Campos {
		par, ok := alertas[c.Nome].([]any)
		if !ok || len(par) < 2 {
			continue
		}
		a, okA := numero(par[0])
		cr, okC := numero(par[1])
		if !okA || !okC {
			continue
		}
		*c.Ler(&lim) = Par{a, cr}
	}
	// A arvore do smart nao cabe no formato [aviso, critico] dos outros
	// limiares: sao dezenas de campos aninhados. Em vez de inventar um segundo
	// formato aqui, ela volta para JSON e entra pelas tags do proprio pacote -
	// que sao as mesmas da especificacao.
	if sub, ok := alertas["smart"]; ok {
		if b, err := json.Marshal(sub); err == nil {
			var cfg smart.Config
			if err := json.Unmarshal(b, &cfg); err == nil {
				lim.Smart = cfg
			}
		}
	}

	// Os reconhecimentos ficam na RAIZ do config, e nao dentro de "alertas":
	// alertas sao a regra, reconhecimento e a excecao a ela, e misturar os
	// dois num mapa so tornaria o arquivo ilegivel a mao.
	if sub, ok := bruto["reconhecidos"].(map[string]any); ok {
		lim.Reconhecidos = Reconhecimentos{}
		for chave, v := range sub {
			m, ok := v.(map[string]any)
			if !ok {
				continue
			}
			r := Reconhecido{}
			if s, ok := m["valor"].(string); ok {
				r.Valor = s
			}
			if f, ok := numero(m["quando"]); ok {
				r.Quando = f
			}
			if s, ok := m["texto"].(string); ok {
				r.Texto = s
			}
			// Sem valor nao ha o que comparar; guardar seria silenciar para
			// sempre, que e o oposto do que o recurso faz.
			if r.Valor != "" {
				lim.Reconhecidos[chave] = r
			}
		}
	}

	if ign, ok := bruto["ignorar_mounts"].([]any); ok {
		lista := make([]string, 0, len(ign))
		for _, x := range ign {
			if s, ok := x.(string); ok {
				lista = append(lista, s)
			}
		}
		lim.IgnorarMounts = lista
	}
	return lim
}

// ComoMapa devolve os limiares no formato do config.json.
func (l Limiares) ComoMapa() map[string][]float64 {
	out := make(map[string][]float64, len(Campos))
	for _, c := range Campos {
		p := *c.Ler(&l)
		out[c.Nome] = []float64{p.Aviso, p.Critico}
	}
	return out
}

func numero(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}
