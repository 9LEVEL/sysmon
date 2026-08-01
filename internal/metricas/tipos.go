// Formato do JSON servido em /metrics. Os nomes dos campos sao o contrato
// publico entre o agente e os clientes: mudar um nome quebra os clientes.
//
// Ate a v5 este arquivo existia em duas copias - uma no agente, outra no
// cliente - com um teste comparando as tags JSON para que nao divergissem em
// silencio. Com um modulo so, a copia deixou de existir e o teste tambem: os
// dois lados passaram a ler a MESMA definicao, que e a unica garantia que
// nao depende de ninguem lembrar de rodar nada. Ponteiros sao usados de proposito onde "nao medido" e diferente
// de zero - um sensor ausente vira null, nao 0 graus.
package metricas

import "encoding/json"

type Sensor struct {
	Chip  string   `json:"chip"`
	Label string   `json:"label"`
	C     float64  `json:"c"`
	Crit  *float64 `json:"crit"`
	Max   *float64 `json:"max"`
}

type Mem struct {
	Total       int64    `json:"total"`
	Usado       int64    `json:"usado"`
	Percent     *float64 `json:"percent"`
	Cache       int64    `json:"cache"`
	SwapTotal   int64    `json:"swap_total"`
	SwapUsado   int64    `json:"swap_usado"`
	SwapPercent *float64 `json:"swap_percent"`
}

type Disco struct {
	Mount         string   `json:"mount"`
	Total         int64    `json:"total"`
	Usado         int64    `json:"usado"`
	Percent       float64  `json:"percent"`
	InodesPercent *float64 `json:"inodes_percent"`
}

// Bloco e um disco fisico inteiro (sda, nvme0n1), nao um ponto de montagem.
// Disco.Mount responde "quanto do filesystem esta cheio"; Bloco responde "qual
// hardware e esse, quao quente esta e quanto de vida sobrou".
type Bloco struct {
	Dev        string `json:"dev"`
	Modelo     string `json:"modelo"`
	Fabricante string `json:"fabricante"`
	Tipo       string `json:"tipo"` // nvme | ssd | hdd
	Tamanho    int64  `json:"tamanho"`

	TempC *float64 `json:"temp_c"` // NVMe direto; SATA so com o modulo drivetemp

	LeituraBps  *float64 `json:"leitura_bps"`
	EscritaBps  *float64 `json:"escrita_bps"`
	UtilPercent *float64 `json:"util_percent"`

	// Vem do timer isolado do smartctl; nil quando smartmontools nao esta
	// instalado ou o timer nunca rodou.
	Smart *Smart `json:"smart"`
}

// Smart normaliza o que smartctl reporta para NVMe e para SATA, que usam
// vocabularios completamente diferentes. Campo nil = aquele disco nao expoe.
//
// Os campos do primeiro bloco existem desde a v1 e sao o resumo que a tabela
// mostra. Os do segundo entraram na v5.1 para alimentar as regras de
// internal/smart, que precisam da tabela inteira e da variacao no tempo -
// contagem parada e contagem crescendo sao coisas diferentes, e nenhum
// resumo de um numero so distingue as duas.
type Smart struct {
	Saude           string   `json:"saude"`            // ok | falha | ""
	DesgastePercent *float64 `json:"desgaste_percent"` // vida util ja consumida
	SpareRestante   *float64 `json:"spare_restante"`   // NVMe available_spare
	HorasLigado     *int64   `json:"horas_ligado"`
	Realocados      *int64   `json:"realocados"`  // setores realocados (SATA)
	ErrosMidia      *int64   `json:"erros_midia"` // media_errors (NVMe)
	IdadeS          *float64 `json:"idade_s"`     // do snapshot, nao do agente

	// ColetaOK falso e o caso do disco atras de um controlador RAID, em que o
	// smartctl responde mas nao alcanca a midia. Isso precisa ser um estado
	// proprio: sem ele o disco fica "sem alerta" para sempre, que e
	// indistinguivel de "saudavel".
	ColetaOK   bool   `json:"coleta_ok"`
	ErroColeta string `json:"erro_coleta,omitempty"`

	// Serial e a identidade que sobrevive a troca de baia - dev muda, serial
	// nao. E por ele que o historico e indexado.
	Serial  string `json:"serial,omitempty"`
	Familia string `json:"familia,omitempty"`

	// O smartctl le a temperatura por SCT/SMART, que funciona no SATA sem o
	// modulo drivetemp carregado - onde o sysfs nao tem nada a dizer.
	TempC              *float64 `json:"temp_c,omitempty"`
	TempMaxC           *float64 `json:"temp_max_c,omitempty"`
	Throttle           bool     `json:"throttle,omitempty"`
	DesligamentosSujos *int64   `json:"desligamentos_sujos,omitempty"`
	CiclosEnergia      *int64   `json:"ciclos_energia,omitempty"`

	Atributos []SmartAtributo `json:"atributos,omitempty"`
}

// SmartAtributo e uma linha da tabela do smartctl mais o que o historico do
// agente sabe sobre ela. Os deltas sao nil enquanto nao houver duas amostras
// separadas pela janela - e ausencia de dado, nao zero.
type SmartAtributo struct {
	ID     int    `json:"id"`
	Nome   string `json:"nome"`
	Valor  *int   `json:"valor,omitempty"`  // normalizado, 0-253
	Pior   *int   `json:"pior,omitempty"`   // pior normalizado ja visto
	Limiar *int   `json:"limiar,omitempty"` // o limite do fabricante
	Cru    *int64 `json:"cru,omitempty"`

	Delta24h *int64 `json:"d24h,omitempty"`
	Delta7d  *int64 `json:"d7d,omitempty"`
	Delta30d *int64 `json:"d30d,omitempty"`
	Base30d  *int64 `json:"base30d,omitempty"`
	Amostras int    `json:"amostras,omitempty"`
}

type Net struct {
	Iface   string   `json:"iface"`
	RXBps   *float64 `json:"rx_bps"`
	TXBps   *float64 `json:"tx_bps"`
	RXTotal int64    `json:"rx_total"`
	TXTotal int64    `json:"tx_total"`
	Erros   int64    `json:"erros"`
	Up      bool     `json:"up"`
	Mbps    *int64   `json:"mbps"`
}

type RaidArray struct {
	Nome      string `json:"nome"`
	Estado    string `json:"estado"`
	Discos    string `json:"discos"`    // mapa [UU_]: U presente, _ faltando
	Degradado *bool  `json:"degradado"` // null quando o mapa nao pode ser lido
}

type Thinpool struct {
	Nome        string   `json:"nome"`
	DataPercent float64  `json:"data_percent"`
	MetaPercent float64  `json:"meta_percent"`
	IdadeS      *float64 `json:"idade_s"` // do snapshot do lvs, nao do agente
}

type Guests struct {
	Qemu int `json:"qemu"`
	LXC  int `json:"lxc"`
}

type SO struct {
	Nome   string `json:"nome"`
	ID     string `json:"id"`
	Kernel string `json:"kernel"`
}

// Extra e um bloco depositado por um timer auxiliar em /run/sysmon/*.json.
// Dados fica opaco de proposito: o agente repassa sem interpretar, o que
// permite adicionar coletores novos sem recompilar nada.
type Extra struct {
	Dados  json.RawMessage `json:"dados"`
	IdadeS *float64        `json:"_idade_s"`
}

// Snapshot e uma foto completa do host.
type Snapshot struct {
	V       string  `json:"v"`
	TS      float64 `json:"ts"`
	Host    string  `json:"host"`
	SO      SO      `json:"so"`
	UptimeS int64   `json:"uptime_s"`

	Load       [3]float64 `json:"load"`
	CPUs       int        `json:"cpus"`
	CPUModelo  string     `json:"cpu_modelo"`
	CPUMHz     *int64     `json:"cpu_mhz"`
	CPUPercent *float64   `json:"cpu_percent"`
	CPUTemp    *float64   `json:"cpu_temp"`
	CPUCrit    *float64   `json:"cpu_crit"`

	Temps    []Sensor                      `json:"temps"`
	Fans     map[string]int64              `json:"fans"`
	Mem      Mem                           `json:"mem"`
	Pressure map[string]map[string]float64 `json:"pressure"`

	Discos    []Disco     `json:"discos"` // filesystems montados
	Blocos    []Bloco     `json:"blocos"` // discos fisicos
	Net       []Net       `json:"net"`
	Raid      []RaidArray `json:"raid"`
	Thinpools []Thinpool  `json:"thinpools"`

	Guests   *Guests          `json:"guests"`
	Extras   map[string]Extra `json:"extras"`
	PlacaMae string           `json:"placa_mae"`

	// Saude do proprio agente. IdadeS crescendo alem de IntervaloS significa
	// que a goroutine coletora travou e o dado abaixo esta velho.
	IdadeS        float64 `json:"idade_s"`
	IntervaloS    float64 `json:"intervalo_s"`
	ColetorFalhas int64   `json:"coletor_falhas"`
}

// Amostras brutas guardadas entre coletas para derivar taxas.
type AmostraNet struct {
	RX, RXPkt, RXErr int64
	TX, TXPkt, TXErr int64
}

type AmostraIO struct {
	LidosB, EscritosB, IOms int64
}
