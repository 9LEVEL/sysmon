// Formato do JSON servido em /metrics. Os nomes dos campos sao o contrato
// publico com o tray do Windows e com o sysmon-dash: mudar um nome aqui quebra
// os clientes. Ponteiros sao usados de proposito onde "nao medido" e diferente
// de zero - um sensor ausente vira null, nao 0 graus.
package main

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
type Smart struct {
	Saude           string   `json:"saude"`            // ok | falha | ""
	DesgastePercent *float64 `json:"desgaste_percent"` // vida util ja consumida
	SpareRestante   *float64 `json:"spare_restante"`   // NVMe available_spare
	HorasLigado     *int64   `json:"horas_ligado"`
	Realocados      *int64   `json:"realocados"`  // setores realocados (SATA)
	ErrosMidia      *int64   `json:"erros_midia"` // media_errors (NVMe)
	IdadeS          *float64 `json:"idade_s"`     // do snapshot, nao do agente
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
