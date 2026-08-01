// Package smart avalia a saude de um disco a partir dos atributos SMART.
//
// Implementa a especificacao de thresholds do projeto. O resumo do que ela
// pede, porque cada decisao aqui sai de um destes principios:
//
//  1. ID de atributo entre 165 e 179 e vendor-specific. Interpretar ID cru e
//     bug de correcao garantido - o mesmo 170 e "Grown_Bad_Blocks" num WD e
//     "Available Reserved Space" num Intel. Aqui a chave e o NOME que o
//     smartctl resolve pela drivedb dele, nunca o numero.
//  2. Metrica relativa vence absoluta. "4 blocos ruins" nao quer dizer nada
//     sozinho; 4 com 98% de reserva intacta e ruido, 4 com 10% e urgente.
//  3. Taxa vence valor absoluto. 200 setores parados ha um ano e um disco
//     saudavel; 0 para 12 numa semana e um disco morrendo.
//  4. O limiar do fabricante e autoridade: VALUE <= THRESH e falha declarada
//     pelo proprio drive.
//  5. Ausencia de alerta nao e atestado de saude. Entre 23% e 36% dos discos
//     que falharam nao tinham indicador SMART nenhum (Google 2007,
//     Backblaze). Por isso este pacote nunca diz "disco saudavel", so "sem
//     indicadores".
//
// Nada aqui depende de rede, arquivo ou relogio: e funcao pura sobre a
// leitura, o que torna a especificacao inteira testavel sem disco nenhum.
package smart

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Severidade. Quatro niveis, cada um mapeando para uma ACAO e nao para uma
// cor: OK nenhuma; Info logar sem notificar; Aviso notificar, validar backup
// e planejar troca; Critico substituir e migrar dados.
const (
	OK = iota
	Info
	Aviso
	Critico
)

var NomeNivel = map[int]string{OK: "ok", Info: "info", Aviso: "aviso",
	Critico: "critico"}

// Categorias. Separadas de proposito: um cabo SATA ruim e uma fonte instavel
// produzem sintoma em atributo de disco, e quem mistura as tres troca midia
// boa e recomeca o ciclo com o mesmo problema.
const (
	Dispositivo  = "dispositivo"
	Interconexao = "interconexao"
	Host         = "host"
)

// Motivo distingue dois CRITICOs que pedem coisas diferentes: um bloco
// pendente e "aja hoje"; 96% de vida consumida e "planeje a troca".
const (
	AgirAgora = "agir"
	Planejar  = "planejar"
)

// Papeis mapeia o papel semantico para os nomes que o smartctl usa.
//
// E este mapa que implementa o principio 1. O smartctl ja aplica a drivedb e
// devolve o nome certo para o fabricante; casar por nome e o que evita o
// palpite. Nome fora desta lista nao vira palpite nenhum: e ignorado com
// log, porque atribuir significado errado a um contador vendor-specific
// produz alarme falso ou, pior, silencio falso.
var Papeis = map[string][]string{
	"reserva": {"Available_Reservd_Space", "Available_Reserved_Space",
		"Available_Spare"},
	"realocados": {"Reallocated_Sector_Ct", "Reallocated_Sector_Count",
		"Reallocated_Event_Count"},
	"pendentes":              {"Current_Pending_Sector", "Current_Pending_Sector_Count"},
	"offline_incorrigivel":   {"Offline_Uncorrectable", "Offline_Uncorrectable_Sector_Count"},
	"reportado_incorrigivel": {"Reported_Uncorrect", "Reported_Uncorrectable_Errors"},
	"erro_ponta_a_ponta":     {"End-to-End_Error", "End_to_End_Error"},
	"timeout_comando":        {"Command_Timeout"},
	"falha_programacao": {"Program_Fail_Count", "Program_Fail_Cnt_Total",
		"Program_Fail_Count_Chip"},
	"falha_apagamento": {"Erase_Fail_Count", "Erase_Fail_Count_Total",
		"Erase_Fail_Count_Chip"},
	"crc":              {"UDMA_CRC_Error_Count"},
	"blocos_crescidos": {"Grown_Bad_Blocks", "Runtime_Bad_Block", "Bad_Block_Count"},
	"blocos_total":     {"Total_Bad_Blocks"},
	"desgaste_restante": {"Media_Wearout_Indicator", "SSD_Life_Left",
		"Wear_Leveling_Count", "Percent_Lifetime_Remain"},
	"desgaste_usado": {"Percentage_Used", "Percent_Life_Used"},
	"ciclos_pe":      {"Average_PE_Cycles_TLC", "Ave_Block-Erase_Count"},
	"desligamento_sujo": {"Unexpect_Power_Loss_Ct", "Unexpected_Power_Loss",
		"Unexpected_Power_Loss_Count", "Unsafe_Shutdown_Count",
		"Power-Off_Retract_Count"},
	"ciclos_energia": {"Power_Cycle_Count"},
	"throttle":       {"Temp_Throttle_Status"},
}

var papelDe = func() map[string]string {
	m := map[string]string{}
	for papel, nomes := range Papeis {
		for _, n := range nomes {
			m[n] = papel
		}
	}
	return m
}()

// PapelDe resolve o nome que o smartctl deu para o papel semantico.
//
// Exportado porque a normalizacao do que vem do agente precisa do MESMO
// vocabulario: manter duas listas de nomes, uma aqui e outra la, garante que
// um dia elas divirjam e um disco reporte desgaste pelo atributo errado.
func PapelDe(nome string) (string, bool) {
	p, ok := papelDe[strings.TrimSpace(nome)]
	return p, ok
}

// ContadoresDeTaxa sao os contadores de degradacao aos quais as regras de
// variacao se aplicam.
var ContadoresDeTaxa = []string{"realocados", "blocos_crescidos", "pendentes",
	"falha_programacao", "falha_apagamento"}

// ---------------------------------------------------------------- entrada
//
// A leitura ja vem normalizada, num formato so para SATA e NVMe - o NVMe nao
// tem tabela de atributos e e traduzido para os mesmos nomes canonicos antes
// de chegar aqui.

// Atributo e uma linha da tabela SMART, com o historico que o coletor mantem.
type Atributo struct {
	ID     int    `json:"id"`
	Nome   string `json:"nome"`
	Valor  *int   `json:"valor"`  // normalizado, 0-100+
	Pior   *int   `json:"pior"`   // pior normalizado ja visto
	Limiar *int   `json:"limiar"` // THRESH do fabricante
	Cru    *int64 `json:"cru"`    // a contagem

	// Variacao, vinda do historico diario guardado no host. Nil = sem
	// baseline; ver Taxa.
	Delta24h *int64 `json:"delta_24h"`
	Delta7d  *int64 `json:"delta_7d"`
	Delta30d *int64 `json:"delta_30d"`
	Base30d  *int64 `json:"base_30d"`
	Amostras int    `json:"amostras"`
}

// Leitura e um disco pronto para avaliar.
type Leitura struct {
	Dev     string `json:"dev"`
	Tipo    string `json:"tipo"` // ssd | hdd | nvme
	Serial  string `json:"serial"`
	Modelo  string `json:"modelo"`
	Familia string `json:"familia"`

	ColetaOK   bool   `json:"coleta_ok"`
	ErroColeta string `json:"erro_coleta"`
	Saude      string `json:"saude"` // ok | falha | ""

	TempC    *float64 `json:"temp_c"`
	TempMaxC *float64 `json:"temp_max_c"`
	Throttle bool     `json:"throttle"`

	PercentualUsado    *float64 `json:"percentual_usado"`
	DesligamentosSujos *int64   `json:"desligamentos_sujos"`
	CiclosEnergia      *int64   `json:"ciclos_energia"`
	NAND               string   `json:"nand"` // slc | mlc | tlc | qlc

	Atributos []Atributo `json:"atributos"`
}

// ---------------------------------------------------------------- achados

// Achado e uma regra que disparou.
//
// Regra e o identificador estavel usado pelo debounce e pela histerese - e o
// que diz "esta e a mesma condicao de uma hora atras".
type Achado struct {
	Categoria  string
	Severidade int
	Regra      string
	Mensagem   string
	Motivo     string
}

func (a Achado) Nivel() string { return NomeNivel[a.Severidade] }

// Curto e a mensagem sem o conselho, para onde couber uma linha so.
//
// Toda mensagem deste pacote e escrita como "o que aconteceu - o que fazer".
// A interface tem os dois espacos: o alerta do rodape, onde a frase inteira
// cabe e o conselho e o que economiza uma busca na internet, e a linha do
// disco na tabela, onde so cabe o fato. Cortar por numero de caracteres, que
// era o que a tabela fazia, produzia frases terminando no meio - "39 de 90
// desligamentos foram".
func (a Achado) Curto() string {
	if i := strings.Index(a.Mensagem, " - "); i > 0 {
		return a.Mensagem[:i]
	}
	return a.Mensagem
}

// Veredito e o resultado por disco, com uma severidade POR CATEGORIA.
//
// Nao existe severidade unica do disco de proposito: cabo ruim e fonte
// instavel nao sao defeito da midia, e somar tudo num numero so faz o
// operador trocar disco bom.
type Veredito struct {
	Dev      string
	Serial   string
	ColetaOK bool
	Achados  []Achado
	SemDados []string // regras de taxa sem baseline
	SemPapel []string // atributos fora do catalogo, ignorados
}

// Severidade e o MAXIMO das regras da categoria - nunca media nem soma. Um
// CRITICO domina cem OK: a media diluiria exatamente o sinal que importa.
func (v Veredito) Severidade(categoria string) int {
	pior := OK
	for _, a := range v.Achados {
		if a.Categoria == categoria && a.Severidade > pior {
			pior = a.Severidade
		}
	}
	return pior
}

func (v Veredito) Dispositivo() int  { return v.Severidade(Dispositivo) }
func (v Veredito) Interconexao() int { return v.Severidade(Interconexao) }
func (v Veredito) Host() int         { return v.Severidade(Host) }

// Pior devolve o achado mais grave de QUALQUER categoria.
//
// Existe separado de Dispositivo() porque a interface tem uma linha so por
// disco: se o pior problema e o cabo ou a energia do host, e isso que precisa
// aparecer ali, nao "sem indicadores de falha" enquanto o alerta la embaixo
// diz outra coisa.
func (v Veredito) Pior() (Achado, bool) {
	pior, achou := Achado{}, false
	for _, a := range v.Achados {
		if !achou || a.Severidade > pior.Severidade {
			pior, achou = a, true
		}
	}
	return pior, achou
}

// Resumo e a frase para a interface.
//
// Nunca "disco saudavel": ausencia de indicador nao e atestado de saude, e
// prometer isso e o unico erro deste pacote que custaria dados a alguem.
func (v Veredito) Resumo() string {
	if !v.ColetaOK {
		return "coleta falhou - saude desconhecida"
	}
	pior := v.Dispositivo()
	if pior == OK {
		return "sem indicadores de falha"
	}
	for _, a := range v.Achados {
		if a.Severidade == pior {
			return a.Mensagem
		}
	}
	return "sem indicadores de falha"
}

// ------------------------------------------------------------- atributos

// Indexar agrupa os atributos por papel semantico, ignorando o desconhecido.
//
// Um nome fora do catalogo NAO vira palpite: seria interpretar contador
// vendor-specific sem tabela do fabricante, que e a forma mais facil de
// inventar um alarme.
func Indexar(atributos []Atributo) map[string]Atributo {
	out := map[string]Atributo{}
	for _, a := range atributos {
		papel, ok := papelDe[strings.TrimSpace(a.Nome)]
		if !ok {
			continue
		}
		if _, ja := out[papel]; !ja {
			out[papel] = a
		}
	}
	return out
}

// SemPapel lista os atributos que Indexar descartou.
//
// A especificacao pede que atributo fora do catalogo seja ignorado COM
// registro. Registrar aqui e devolver dado, e nao escrever em stderr, porque
// quem chama Indexar e o cliente - e um print no meio de uma tabela de
// terminal estraga a tela sem ajudar ninguem. Quem quiser ver, pede.
func SemPapel(atributos []Atributo) []string {
	var out []string
	for _, a := range atributos {
		if _, ok := papelDe[strings.TrimSpace(a.Nome)]; !ok && a.Nome != "" {
			out = append(out, a.Nome)
		}
	}
	return out
}

func cru(a Atributo, ok bool) int64 {
	if !ok || a.Cru == nil {
		return 0
	}
	return *a.Cru
}

// ------------------------------------------------------------- avaliacao

// Avaliar julga um disco. Funcao pura: mesma leitura, mesmo veredito.
func Avaliar(l Leitura, cfg Config) Veredito {
	cfg = cfg.ComPadroes()
	v := Veredito{Dev: l.Dev, Serial: l.Serial, ColetaOK: true}

	// Coleta falha e estado proprio. Confundir com OK e o erro que faz um
	// disco atras de controladora RAID, invisivel ao smartctl, aparecer como
	// saudavel para sempre.
	if !l.ColetaOK {
		msg := l.ErroColeta
		if msg == "" {
			msg = "nao consegui ler o SMART deste disco"
		}
		v.ColetaOK = false
		v.Achados = append(v.Achados, Achado{Dispositivo, Aviso, "coleta", msg, AgirAgora})
		return v
	}

	p := Indexar(l.Atributos)
	v.SemPapel = SemPapel(l.Atributos)

	// O drive se declarando em falha vence qualquer interpretacao nossa.
	if l.Saude == "falha" {
		v.Achados = append(v.Achados, Achado{Dispositivo, Critico,
			"saude:autoteste", "o proprio drive reprovou no autoteste SMART",
			AgirAgora})
	}

	v.Achados = append(v.Achados, reserva(p, cfg)...)
	v.Achados = append(v.Achados, blocosSemReserva(p, cfg, l.Tipo)...)
	v.Achados = append(v.Achados, imediatos(p, cfg)...)
	v.Achados = append(v.Achados, interconexao(p)...)
	v.Achados = append(v.Achados, desgaste(p, l, cfg)...)
	v.Achados = append(v.Achados, temperatura(l, cfg)...)
	v.Achados = append(v.Achados, saudeDoHost(p, l, cfg)...)

	for _, papel := range ContadoresDeTaxa {
		a, ok := p[papel]
		if !ok {
			continue
		}
		achados, teveHistorico := Taxa(papel, a, cfg)
		if teveHistorico {
			v.Achados = append(v.Achados, achados...)
		} else if cru(a, true) > 0 {
			// So reporta falta de historico onde ela faz diferenca: num
			// contador ainda zerado nao ha o que comparar.
			v.SemDados = append(v.SemDados, papel)
		}
	}
	sort.SliceStable(v.Achados, func(i, j int) bool {
		return v.Achados[i].Severidade > v.Achados[j].Severidade
	})
	return v
}

// ------------------------------------------------- 2. reserva disponivel
func reserva(p map[string]Atributo, cfg Config) []Achado {
	a, ok := p["reserva"]
	if !ok || a.Valor == nil {
		return nil
	}
	v := *a.Valor
	c := cfg.Reserva

	// O limiar do fabricante e autoridade (principio 4). A margem dispara
	// ANTES dele para dar janela de substituicao, em vez de avisar no
	// instante em que o drive ja se declarou em falha.
	if a.Limiar != nil && *a.Limiar > 0 {
		lim := *a.Limiar
		if v <= lim {
			return []Achado{{Dispositivo, Critico, "reserva:limiar_fabricante",
				fmt.Sprintf("reserva em %d - no ou abaixo do limite do fabricante "+
					"(%d) - o drive declarou falha", v, lim), AgirAgora}}
		}
		if v <= lim+c.MargemAcimaDoLimiar {
			return []Achado{{Dispositivo, Critico, "reserva:margem",
				fmt.Sprintf("reserva em %d - a %d pontos do limite do fabricante (%d)",
					v, v-lim, lim), AgirAgora}}
		}
	}

	switch {
	case v >= c.OKMin:
		return nil
	case v >= c.InfoMin:
		return []Achado{{Dispositivo, Info, "reserva:info",
			fmt.Sprintf("reserva de blocos em %d%%", v), AgirAgora}}
	case v >= c.AvisoMin:
		return []Achado{{Dispositivo, Aviso, "reserva:warn",
			fmt.Sprintf("reserva de blocos em %d%%", v), AgirAgora}}
	}
	return []Achado{{Dispositivo, Critico, "reserva:critico",
		fmt.Sprintf("reserva de blocos em %d%%", v), AgirAgora}}
}

// estatico diz se o contador esta parado, com historico que sustente isso.
//
// Existe para o principio 3, que a especificacao coloca acima dos limiares
// concretos: um disco estavel em 200 setores realocados ha dois anos esta
// saudavel, enquanto um que foi de 0 a 12 numa semana esta morrendo. Sem
// isto, a tabela de contagem bruta condenaria o primeiro.
func estatico(a Atributo) bool {
	if a.Amostras < 2 {
		return false // sem baseline nao se afirma que esta parado
	}
	for _, d := range []*int64{a.Delta24h, a.Delta7d, a.Delta30d} {
		if d != nil && *d != 0 {
			return false
		}
	}
	return true
}

// tetoSeParado limita a INFO a contagem bruta de um contador parado.
func tetoSeParado(a Atributo, achados []Achado) []Achado {
	if !estatico(a) {
		return achados
	}
	out := make([]Achado, 0, len(achados))
	for _, x := range achados {
		if x.Severidade > Info {
			x.Severidade = Info
		}
		x.Mensagem += " (parados, sem crescimento no historico)"
		out = append(out, x)
	}
	return out
}

// blocosSemReserva e o fallback da secao 2.1: so quando NAO ha reserva.
func blocosSemReserva(p map[string]Atributo, cfg Config, tipo string) []Achado {
	if _, tem := p["reserva"]; tem {
		return nil
	}
	crescidos, temC := p["blocos_crescidos"]
	total, temT := p["blocos_total"]
	if temC && temT && crescidos.Cru != nil && total.Cru != nil {
		// Proporcao contra os blocos ruins de fabrica: 4 num drive com 199
		// de fabrica e outra coisa que 4 num drive com 8.
		base := *total.Cru - *crescidos.Cru
		if base < 1 {
			base = 1
		}
		razao := float64(*crescidos.Cru) / float64(base)
		c := cfg.RazaoBlocos
		var sev int
		switch {
		case razao > c.Critico:
			sev = Critico
		case razao > c.Aviso:
			sev = Aviso
		case razao > c.Info:
			sev = Info
		default:
			return nil
		}
		return tetoSeParado(crescidos, []Achado{{Dispositivo, sev, "blocos:razao",
			fmt.Sprintf("%d blocos crescidos, %.0f%% da reserva de fabrica",
				*crescidos.Cru, razao*100), AgirAgora}})
	}

	if strings.ToLower(tipo) == "hdd" {
		a, ok := p["realocados"]
		n := cru(a, ok)
		if n <= 0 {
			return nil
		}
		c := cfg.RealocadosBruto
		sev := Info
		switch {
		case n >= c.Critico:
			sev = Critico
		case n >= c.Aviso:
			sev = Aviso
		}
		// Disco com QUALQUER setor realocado falha muito mais que um com
		// zero, mas a maioria ainda sobrevive meses. Nao vale acordar
		// ninguem pelo valor isolado - quem acorda e a regra de taxa.
		return tetoSeParado(a, []Achado{{Dispositivo, sev, "realocados:bruto",
			fmt.Sprintf("%d setores realocados", n), AgirAgora}})
	}
	return nil
}

// ------------------------------------------------------------- 3. taxa

// Taxa aplica as regras de variacao. Devolve (achados, tinhaHistorico).
//
// Sao as mais importantes da especificacao: e a taxa que separa um disco
// estavel ha dois anos de um disco morrendo esta semana. Sem historico elas
// NAO devolvem OK - devolvem "sem dados", porque afirmar saude sem baseline
// seria mentira.
func Taxa(papel string, a Atributo, cfg Config) ([]Achado, bool) {
	if a.Amostras < 2 {
		return nil, false
	}
	c := cfg.Crescimento
	rotulo := strings.ReplaceAll(papel, "_", " ")
	var out []Achado

	if a.Delta24h != nil && *a.Delta24h >= c.Critico24h {
		out = append(out, Achado{Dispositivo, Critico, "taxa:" + papel + ":24h",
			fmt.Sprintf("%d novos em %s nas ultimas 24h", *a.Delta24h, rotulo),
			AgirAgora})
	}
	if a.Delta7d != nil && *a.Delta7d > 0 {
		d7 := *a.Delta7d
		switch {
		case d7 >= c.Critico7d:
			out = append(out, Achado{Dispositivo, Critico, "taxa:" + papel + ":7d",
				fmt.Sprintf("%d novos em %s em 7 dias", d7, rotulo), AgirAgora})
		case d7 >= c.Aviso7d:
			out = append(out, Achado{Dispositivo, Aviso, "taxa:" + papel + ":7d",
				fmt.Sprintf("%d novos em %s em 7 dias", d7, rotulo), AgirAgora})
		default:
			out = append(out, Achado{Dispositivo, Info, "taxa:" + papel + ":mexeu",
				fmt.Sprintf("%s subiu %d em 7 dias", rotulo, d7), AgirAgora})
		}
	}

	// Dobrou na janela. A guarda de base evita que 1 -> 2 vire alarme, que e
	// o ruido classico de contador pequeno.
	if a.Base30d != nil && a.Cru != nil && *a.Base30d >= c.DobrarBaseMinima &&
		*a.Cru >= *a.Base30d*2 {
		out = append(out, Achado{Dispositivo, Critico, "taxa:" + papel + ":dobrou",
			fmt.Sprintf("%s dobrou em %d dias (%d -> %d)", rotulo,
				c.DobrarJanelaDias, *a.Base30d, *a.Cru), AgirAgora})
	}

	// Aceleracao: a semana atual contra a media semanal do mes anterior.
	if a.Delta7d != nil && a.Delta30d != nil && *a.Delta7d > 0 &&
		*a.Delta30d > *a.Delta7d {
		mediaSemanal := float64(*a.Delta30d-*a.Delta7d) / 3.0
		if mediaSemanal > 0 && float64(*a.Delta7d) > mediaSemanal*c.FatorAceleracao {
			out = append(out, Achado{Dispositivo, Aviso, "taxa:" + papel + ":acelerou",
				fmt.Sprintf("%s acelerou - %d em 7 dias contra %.1f/semana no mes "+
					"anterior", rotulo, *a.Delta7d, mediaSemanal), AgirAgora})
		}
	}
	return out, true
}

// --------------------------------------------------- 4. escalacao direta

// imediatos sao os contadores que indicam erro JA visivel ao host.
//
// Aqui a contagem bruta importa e a margem e estreita: nao e degradacao
// silenciosa, e dado que ja pode ter sido perdido.
func imediatos(p map[string]Atributo, cfg Config) []Achado {
	regras := []struct{ papel, chave, texto string }{
		{"pendentes", "current_pending_sector",
			"setores pendentes (suspeitos, ainda nao realocados)"},
		{"offline_incorrigivel", "offline_uncorrectable",
			"setores incorrigiveis - perda de dado confirmada"},
		{"reportado_incorrigivel", "reported_uncorrect",
			"erros incorrigiveis reportados"},
		{"erro_ponta_a_ponta", "end_to_end_error",
			"erro ponta a ponta - corrupcao no caminho interno de dados"},
		{"timeout_comando", "command_timeout", "timeouts de comando"},
		{"falha_programacao", "program_fail_count", "falhas de programacao"},
		{"falha_apagamento", "erase_fail_count", "falhas de apagamento"},
	}
	var out []Achado
	for _, r := range regras {
		a, ok := p[r.papel]
		n := cru(a, ok)
		if n <= 0 {
			continue
		}
		lim := cfg.Imediatos[r.chave]
		switch {
		case lim.Critico > 0 && n >= lim.Critico:
			out = append(out, Achado{Dispositivo, Critico, "imediato:" + r.papel,
				fmt.Sprintf("%d %s", n, r.texto), AgirAgora})
		case lim.Aviso > 0 && n >= lim.Aviso:
			out = append(out, Achado{Dispositivo, Aviso, "imediato:" + r.papel,
				fmt.Sprintf("%d %s", n, r.texto), AgirAgora})
		}
	}
	return out
}

// interconexao trata o CRC do UDMA, que NAO e falha de disco.
//
// E erro de transmissao no barramento: cabo mal encaixado, cabo ruim,
// backplane, controladora. Vai para categoria propria porque quem le isso
// como defeito de midia troca um disco perfeitamente bom e continua com o
// problema.
func interconexao(p map[string]Atributo) []Achado {
	a, ok := p["crc"]
	n := cru(a, ok)
	if n <= 0 {
		return nil
	}
	if a.Delta7d != nil && *a.Delta7d > 0 {
		return []Achado{{Interconexao, Aviso, "interconexao:crc",
			fmt.Sprintf("%d novos erros de CRC no barramento - cabo, conector "+
				"ou controladora, nao a midia", *a.Delta7d), AgirAgora}}
	}
	return []Achado{{Interconexao, Info, "interconexao:crc_estatico",
		fmt.Sprintf("%d erros de CRC acumulados, sem novos - o contador nunca "+
			"zera sozinho; provavelmente incidente ja resolvido", n), AgirAgora}}
}

// -------------------------------------------------------------- 5. desgaste

// PercentualUsado devolve a vida consumida, na ordem de preferencia da spec.
//
// Raw empacotado (do tipo 0x1b2017001b20, que nao e inteiro decimal) e
// DESCARTADO: interpretar isso sem tabela do fabricante e chute.
func PercentualUsado(p map[string]Atributo, l Leitura, cfg Config) (float64, bool) {
	if l.PercentualUsado != nil {
		return *l.PercentualUsado, true
	}
	if a, ok := p["desgaste_usado"]; ok && a.Cru != nil && *a.Cru >= 0 && *a.Cru <= 100 {
		return float64(*a.Cru), true
	}
	// Indicadores que contam vida RESTANTE de 100 a 0.
	if a, ok := p["desgaste_restante"]; ok && a.Valor != nil &&
		*a.Valor >= 0 && *a.Valor <= 100 {
		return float64(100 - *a.Valor), true
	}
	if a, ok := p["ciclos_pe"]; ok && a.Cru != nil {
		if nominais, ok := cfg.Desgaste.CiclosNAND[strings.ToLower(l.NAND)]; ok &&
			nominais > 0 {
			return 100 * float64(*a.Cru) / float64(nominais), true
		}
	}
	return 0, false
}

func desgaste(p map[string]Atributo, l Leitura, cfg Config) []Achado {
	pct, ok := PercentualUsado(p, l, cfg)
	if !ok {
		return nil
	}
	c := cfg.Desgaste
	var sev int
	switch {
	case pct > c.Critico:
		sev = Critico
	case pct >= c.Aviso:
		sev = Aviso
	case pct >= c.Info:
		sev = Info
	default:
		return nil
	}
	// Motivo Planejar: desgaste alto nao e falha iminente. Um SSD em 95%
	// tipicamente ainda funciona e falha de forma previsivel, virando
	// read-only. E outro tipo de urgencia que um setor pendente.
	return []Achado{{Dispositivo, sev, "desgaste",
		fmt.Sprintf("%.0f%% da vida util consumida", pct), Planejar}}
}

// ----------------------------------------------------------- 6. temperatura
func temperatura(l Leitura, cfg Config) []Achado {
	c := cfg.Temperatura.SSD
	hdd := strings.ToLower(l.Tipo) == "hdd"
	if hdd {
		c = cfg.Temperatura.HDD
	}

	faixa := func(t float64) int {
		if hdd && t < c.CriticoBaixo {
			return Critico
		}
		switch {
		case t >= c.Critico:
			return Critico
		case t >= c.Aviso:
			return Aviso
		case t >= c.Info:
			return Info
		}
		return OK
	}

	var out []Achado
	if l.TempC != nil {
		if sev := faixa(*l.TempC); sev != OK {
			out = append(out, Achado{Dispositivo, sev, "temp",
				fmt.Sprintf("%.0f C", *l.TempC), AgirAgora})
		}
	}
	// O maximo historico conta um nivel abaixo: um pico de 58 C ha seis
	// meses e registro, nao emergencia de agora.
	if l.TempMaxC != nil {
		if sev := faixa(*l.TempMaxC) + cfg.Temperatura.OffsetMaxima; sev > OK {
			out = append(out, Achado{Dispositivo, sev, "temp:maxima",
				fmt.Sprintf("maxima ja registrada de %.0f C", *l.TempMaxC),
				AgirAgora})
		}
	}
	if l.Throttle {
		out = append(out, Achado{Dispositivo, Aviso, "temp:throttle",
			"throttling termico ativo", AgirAgora})
	}
	return out
}

// ------------------------------------------- 7. saude do host, nao do disco

// saudeDoHost avalia o desligamento sujo. Categoria propria porque a acao e
// outra: estes achados costumam ser a CAUSA dos blocos ruins, e trocar o
// disco sem tratar a fonte reinicia o ciclo com midia nova.
func saudeDoHost(p map[string]Atributo, l Leitura, cfg Config) []Achado {
	sujos, ciclos := l.DesligamentosSujos, l.CiclosEnergia
	if sujos == nil {
		if a, ok := p["desligamento_sujo"]; ok && a.Cru != nil {
			sujos = a.Cru
		}
	}
	if ciclos == nil {
		if a, ok := p["ciclos_energia"]; ok && a.Cru != nil {
			ciclos = a.Cru
		}
	}
	if sujos == nil || ciclos == nil {
		return nil
	}
	den := *ciclos
	if den < 1 {
		den = 1
	}
	razao := float64(*sujos) / float64(den)
	c := cfg.SaudeHost
	var sev int
	switch {
	case razao > c.Critico:
		sev = Critico
	case razao >= c.Aviso:
		sev = Aviso
	case razao >= c.Info:
		sev = Info
	default:
		return nil
	}
	msg := fmt.Sprintf("%d de %d desligamentos foram inesperados (%.0f%%)",
		*sujos, *ciclos, razao*100)
	if sev >= Aviso {
		// SSD de consumo nao tem capacitor de PLP: cada corte durante
		// escrita pode custar blocos e corromper metadado de filesystem. A
		// acao e nobreak ou investigar o host - nao trocar a midia.
		msg += " - considere nobreak; a midia nao e a causa"
	}
	return []Achado{{Host, sev, "host:desligamento_sujo", msg, Planejar}}
}

// AvaliarFrota julga varios discos de uma vez.
func AvaliarFrota(discos []Leitura, cfg Config) []Veredito {
	out := make([]Veredito, 0, len(discos))
	for _, d := range discos {
		out = append(out, Avaliar(d, cfg))
	}
	return out
}

// ContadorRegrediu diz se um contador DIMINUIU, o que nao e melhora.
//
// Contadores SMART so crescem. Cair significa disco trocado na mesma baia,
// firmware bugado ou erro de parsing - os tres pedem baseline nova e um
// registro de anomalia, nunca um suspiro de alivio.
func ContadorRegrediu(anterior, atual int64) bool { return atual < anterior }

var _ = math.Abs
