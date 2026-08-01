// Package historico guarda a serie temporal dos contadores SMART.
//
// Existe por causa do principio 3 da especificacao: taxa vence valor
// absoluto. "200 setores realocados" nao diz nada sozinho - parados ha um ano
// e um disco velho que funciona; 0 para 12 numa semana e um disco morrendo.
// Distinguir os dois exige lembrar do passado, e o smartctl so responde sobre
// o presente.
//
// Mora no AGENTE, e nao no cliente, porque o agente e quem tem continuidade:
// o cliente pode ficar semanas fechado, e um historico que so avanca quando
// alguem esta olhando nao serve para detectar degradacao.
//
// A serie e indexada por SERIAL e nao por dev: sda vira sdb quando alguem
// troca a ordem dos cabos, e o historico de um disco nao pode migrar para
// outro por causa disso.
package historico

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"sysmon/internal/metricas"
	"sysmon/internal/smart"
)

// Resolucao do arquivo. Guardar uma amostra por hora durante 180 dias daria
// 4320 pontos por disco, para responder perguntas que so precisam de "quanto
// mudou em 24 h, 7 e 30 dias". Entao a serie e densa onde importa e rala no
// resto: uma amostra por hora nas ultimas 48 h, uma por dia antes disso.
// Sao ~228 pontos por disco, e o arquivo inteiro de um host com quatro discos
// cabe em algumas dezenas de KB.
const (
	JanelaDensa   = 48 * time.Hour
	RetencaoDias  = 180
	IntervaloMin  = 60
	maxAmostrasSe = 400 // trava dura por serie, caso o relogio ande para tras
)

// Amostra e o valor cru de cada contador num instante.
type Amostra struct {
	TS      float64          `json:"ts"`
	Valores map[string]int64 `json:"v"`
}

type serie struct {
	Amostras []Amostra `json:"amostras"`
}

// Arquivo e o historico de um host inteiro, persistido em disco.
//
// Todo erro de IO e degradacao silenciosa e proposital: um historico que nao
// pode ser lido faz o sysmon perder as regras de taxa, e um que nao pode ser
// gravado faz perder as de amanha. Nenhum dos dois pode derrubar a coleta,
// que e o que o host de fato precisa que funcione.
type Arquivo struct {
	caminho   string
	retencao  time.Duration
	intervalo time.Duration

	mu     sync.Mutex
	series map[string]*serie
	erro   error
}

// Abrir le o historico existente. Arquivo ausente ou corrompido comeca vazio -
// perder o passado e ruim, mas nao gravar o futuro e pior.
func Abrir(caminho string) *Arquivo {
	a := &Arquivo{
		caminho:   caminho,
		retencao:  RetencaoDias * 24 * time.Hour,
		intervalo: IntervaloMin * time.Minute,
		series:    map[string]*serie{},
	}
	b, err := os.ReadFile(caminho)
	if err != nil {
		if !os.IsNotExist(err) {
			a.erro = err
		}
		return a
	}
	var lido map[string]*serie
	if err := json.Unmarshal(b, &lido); err != nil {
		a.erro = err
		return a
	}
	for k, v := range lido {
		if v != nil {
			a.series[k] = v
		}
	}
	return a
}

// Erro devolve a ultima falha de leitura, para o agente logar uma vez.
func (a *Arquivo) Erro() error { return a.erro }

// Aplicar registra a leitura de agora e preenche os deltas dos atributos.
//
// E o unico ponto de entrada usado pelo coletor: registrar e consultar juntos
// evitam a janela em que uma amostra e gravada e a resposta sai sem ela.
func (a *Arquivo) Aplicar(s *metricas.Smart, agora time.Time) (mudou bool) {
	if a == nil || s == nil || s.Serial == "" || !s.ColetaOK {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	ts := float64(agora.Unix())
	valores := make(map[string]int64, len(s.Atributos))
	for _, at := range s.Atributos {
		if at.Cru != nil && at.Nome != "" {
			valores[at.Nome] = *at.Cru
		}
	}

	se := a.series[s.Serial]
	if se == nil {
		se = &serie{}
		a.series[s.Serial] = se
	}
	if regrediu(se, valores) {
		// Contador SMART so cresce. Ter diminuido significa que este serial
		// nao e mais a mesma historia - disco reencaixado numa baia que ja
		// teve outro, firmware que zerou a tabela, leitura de outro caminho.
		// Deltas calculados por cima disso sairiam negativos e sem sentido,
		// entao a serie recomeca.
		se.Amostras = nil
		mudou = true
	}
	if ultima := ultima(se); ultima == nil || ts-ultima.TS >= a.intervalo.Seconds() {
		se.Amostras = append(se.Amostras, Amostra{TS: ts, Valores: valores})
		compactar(se, ts, a.retencao.Seconds())
		mudou = true
	}

	for i := range s.Atributos {
		at := &s.Atributos[i]
		if at.Cru == nil || at.Nome == "" {
			continue
		}
		at.Amostras = len(se.Amostras)
		at.Delta24h = delta(se, at.Nome, *at.Cru, ts, 24*3600)
		at.Delta7d = delta(se, at.Nome, *at.Cru, ts, 7*24*3600)
		at.Delta30d = delta(se, at.Nome, *at.Cru, ts, 30*24*3600)
		at.Base30d = valorEm(se, at.Nome, ts-30*24*3600)
	}
	return mudou
}

// Salvar grava atomicamente: o agente nunca pode ser interrompido no meio de
// uma escrita e acordar sem historico nenhum.
func (a *Arquivo) Salvar() error {
	if a == nil || a.caminho == "" {
		return nil
	}
	a.mu.Lock()
	b, err := json.Marshal(a.series)
	a.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.caminho), 0o700); err != nil {
		return err
	}
	tmp := a.caminho + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.caminho)
}

// Seriais lista o que ha no historico, para diagnostico.
func (a *Arquivo) Seriais() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, 0, len(a.series))
	for k := range a.series {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ------------------------------------------------------------- serie pura

func ultima(s *serie) *Amostra {
	if len(s.Amostras) == 0 {
		return nil
	}
	return &s.Amostras[len(s.Amostras)-1]
}

// regrediu usa a regra do pacote smart em vez de reimplementar a comparacao:
// "contador SMART so cresce" e uma afirmacao sobre o dominio, e duas copias
// dela e uma copia a mais para discordar.
func regrediu(s *serie, valores map[string]int64) bool {
	u := ultima(s)
	if u == nil {
		return false
	}
	for nome, v := range valores {
		if antes, ok := u.Valores[nome]; ok && smart.ContadorRegrediu(antes, v) {
			return true
		}
	}
	return false
}

// delta responde quanto o contador subiu na janela, ou nil quando o historico
// nao alcanca tao longe. nil e "nao sei", que e diferente de zero - e essa
// diferenca e o caso 10.5 da especificacao.
func delta(s *serie, nome string, atual int64, agora, janelaS float64) *int64 {
	antes := valorEm(s, nome, agora-janelaS)
	if antes == nil {
		return nil
	}
	d := atual - *antes
	return &d
}

// valorEm devolve o valor mais recente ANTERIOR ao instante pedido. Exige que
// a serie de fato alcance aquele ponto: extrapolar para tras produziria taxa
// inventada justo no disco recem-instalado, que e onde ninguem tem dado.
func valorEm(s *serie, nome string, quando float64) *int64 {
	var achado *int64
	for i := range s.Amostras {
		am := &s.Amostras[i]
		if am.TS > quando {
			break
		}
		if v, ok := am.Valores[nome]; ok {
			c := v
			achado = &c
		}
	}
	return achado
}

// compactar aplica a resolucao dupla e a retencao.
func compactar(s *serie, agora, retencaoS float64) {
	corte := agora - retencaoS
	densa := agora - JanelaDensa.Seconds()

	out := s.Amostras[:0]
	for i, am := range s.Amostras {
		if am.TS < corte {
			continue
		}
		ultimoDoDia := true
		if am.TS < densa {
			// Fora da janela densa, so o ultimo ponto de cada dia sobrevive.
			dia := int64(am.TS) / 86400
			for _, seguinte := range s.Amostras[i+1:] {
				if seguinte.TS >= densa {
					break
				}
				if int64(seguinte.TS)/86400 == dia {
					ultimoDoDia = false
					break
				}
			}
		}
		if ultimoDoDia {
			out = append(out, am)
		}
	}
	s.Amostras = out
	if n := len(s.Amostras); n > maxAmostrasSe {
		s.Amostras = s.Amostras[n-maxAmostrasSe:]
	}
}

// CaminhoPadrao escolhe onde gravar.
//
// No agente sob systemd, StateDirectory=sysmon aponta para um diretorio que
// sobrevive a reinicio e pertence ao DynamicUser - e o unico lugar gravavel
// que o hardening deixa. Fora dele (modo local, teste, execucao a mao), o
// cache do usuario serve: o historico e util, mas nunca a ponto de justificar
// pedir privilegio.
func CaminhoPadrao() string {
	if d := os.Getenv("STATE_DIRECTORY"); d != "" {
		// systemd aceita varios diretorios separados por ':'.
		primeiro, _, _ := strings.Cut(d, ":")
		return filepath.Join(primeiro, "smart-historico.json")
	}
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "sysmon", "smart-historico.json")
	}
	return ""
}
