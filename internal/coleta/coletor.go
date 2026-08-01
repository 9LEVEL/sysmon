// O Coletor amostra o host numa goroutine propria e guarda o resultado pronto.
//
// Na v1 a coleta acontecia dentro do handler HTTP, segurando um mutex: um sysfs
// lento travava todas as requisicoes, e as taxas eram calculadas sobre o
// intervalo aleatorio entre dois clientes. Agora o caminho da requisicao nao
// toca em disco - so serializa um struct ja pronto. N clientes custam o mesmo
// que um, e as taxas saem de uma janela fixa e conhecida.
package coleta

import (
	"sysmon/internal/historico"
	"sysmon/internal/metricas"

	"log"
	"math"
	"os"
	"runtime"
	"sort"
	"sync"
	"time"
)

type amostra struct {
	t      time.Time
	idle   int64
	total  int64
	temCPU bool
	net    map[string]metricas.AmostraNet
	io     map[string]metricas.AmostraIO
}

type Coletor struct {
	versao    string
	fontes    Fontes
	intervalo time.Duration

	historico       *historico.Arquivo
	avisouHistorico bool

	mu       sync.RWMutex
	atual    metricas.Snapshot
	falhas   int64
	anterior *amostra
}

// ComHistorico liga a serie temporal dos contadores SMART, sem a qual as
// regras de taxa de internal/smart ficam em "sem dados". E opcional porque o
// mesmo coletor roda no modo local do cliente, onde pode nao haver lugar
// gravavel - ali a coleta ainda vale, so nao aprende com o passado.
func (c *Coletor) ComHistorico(h *historico.Arquivo) *Coletor {
	c.historico = h
	return c
}

// NovoColetor monta o coletor. A versao entra por parametro, e nao por
// variavel global do pacote: o coletor e usado por dois programas - o agente
// e o modo local do cliente - e configuracao global e o que faz o segundo
// herdar em silencio o que o primeiro definiu.
func NovoColetor(f Fontes, intervalo time.Duration, versao string) *Coletor {
	return &Coletor{fontes: f, intervalo: intervalo, versao: versao}
}

// bruto le so os contadores acumulados, que sao a base das taxas.
func (c *Coletor) bruto() *amostra {
	idle, total, ok := c.fontes.CPUBruto()
	return &amostra{
		t: time.Now(), idle: idle, total: total, temCPU: ok,
		net: c.fontes.NetBruto(),
		io:  c.fontes.DiskIOBruto(),
	}
}

func (c *Coletor) coletar() metricas.Snapshot {
	ag := c.bruto()
	ant := c.anterior
	c.anterior = ag

	var dt float64
	if ant != nil {
		dt = ag.t.Sub(ant.t).Seconds()
	}

	temps := c.fontes.Temps()
	extras := c.fontes.Extras()
	s := metricas.Snapshot{
		V:          c.versao,
		TS:         float64(ag.t.UnixNano()) / 1e9,
		Host:       hostname(),
		SO:         c.fontes.SO(),
		UptimeS:    c.fontes.UptimeS(),
		Load:       c.fontes.Load(),
		CPUs:       runtime.NumCPU(),
		CPUPercent: usoCPU(ant, ag),
		Temps:      temps,
		Fans:       c.fontes.Fans(),
		Mem:        c.fontes.Mem(),
		Pressure:   c.fontes.Pressure(),
		Discos:     c.fontes.Discos(c.fontes.DescobrirMounts()),
		Blocos:     c.blocos(ant, ag, dt, extras),
		Net:        c.taxasNet(ant, ag, dt),
		Raid:       c.fontes.Raid(),
		Guests:     c.fontes.Guests(),
		Extras:     extras,
		PlacaMae:   c.fontes.PlacaMae(),
		IntervaloS: c.intervalo.Seconds(),
	}
	s.CPUModelo, s.CPUMHz = c.fontes.CPUInfo()
	s.Thinpools = Thinpools(s.Extras)
	s.Extras = soOsNaoConsumidos(s.Extras)

	if cpu := SensorCPU(temps); cpu != nil {
		s.CPUTemp, s.CPUCrit = f64(cpu.C), cpu.Crit
	}

	// Maior uso primeiro: o disco que vai encher e o que interessa na tela.
	sort.SliceStable(s.Discos, func(i, j int) bool {
		return s.Discos[i].Percent > s.Discos[j].Percent
	})
	return s
}

func usoCPU(ant, ag *amostra) *float64 {
	if ant == nil || !ant.temCPU || !ag.temCPU {
		return nil
	}
	dIdle := ag.idle - ant.idle
	dTotal := ag.total - ant.total
	if dTotal <= 0 || dIdle < 0 {
		return nil
	}
	return f64(arred(100*(1-float64(dIdle)/float64(dTotal)), 1))
}

func (c *Coletor) taxasNet(ant, ag *amostra, dt float64) []metricas.Net {
	nomes := make([]string, 0, len(ag.net))
	for n := range ag.net {
		nomes = append(nomes, n)
	}
	sort.Strings(nomes)

	out := make([]metricas.Net, 0, len(nomes))
	for _, nome := range nomes {
		a := ag.net[nome]
		p, tem := metricas.AmostraNet{}, false
		if ant != nil {
			p, tem = ant.net[nome]
		}
		up, mbps := c.fontes.NetEstado(nome)
		out = append(out, metricas.Net{
			Iface:   nome,
			RXBps:   taxa(a.RX, p.RX, tem, dt),
			TXBps:   taxa(a.TX, p.TX, tem, dt),
			RXTotal: a.RX,
			TXTotal: a.TX,
			Erros:   a.RXErr + a.TXErr,
			Up:      up,
			Mbps:    mbps,
		})
	}
	return out
}

// blocos junta as tres fontes de informacao sobre cada disco fisico: os
// contadores de IO (delta entre amostras), a identidade vinda do sysfs e o
// SMART depositado pelo timer isolado.
func (c *Coletor) blocos(ant, ag *amostra, dt float64, extras map[string]metricas.Extra) []metricas.Bloco {
	nomes := make([]string, 0, len(ag.io))
	for n := range ag.io {
		nomes = append(nomes, n)
	}
	sort.Strings(nomes)

	out := make([]metricas.Bloco, 0, len(nomes))
	for _, nome := range nomes {
		a := ag.io[nome]
		p, tem := metricas.AmostraIO{}, false
		if ant != nil {
			p, tem = ant.io[nome]
		}

		b := c.fontes.InfoBloco(nome)
		b.LeituraBps = taxa(a.LidosB, p.LidosB, tem, dt)
		b.EscritaBps = taxa(a.EscritosB, p.EscritosB, tem, dt)
		// io_ms sobe 1000ms por segundo de parede quando o disco esta 100%
		// ocupado; a razao entre os dois e a taxa de ocupacao.
		if ms := taxa(a.IOms, p.IOms, tem, dt); ms != nil {
			b.UtilPercent = f64(arred(math.Min(*ms/10, 100), 1))
		}
		b.Smart = SmartDe(extras, nome)
		c.anotarHistorico(b.Smart)
		// O sysfs so expoe temperatura de SATA com o modulo drivetemp; o
		// smartctl le pelo proprio protocolo do disco. Preferimos o sysfs
		// quando ha, porque e do ciclo atual, e caimos no smartctl - que e
		// da ultima passagem do timer - em vez de nao mostrar nada.
		if b.TempC == nil && b.Smart != nil {
			b.TempC = b.Smart.TempC
		}
		out = append(out, b)
	}
	return out
}

// consumidos sao os extras que o agente ja traduziu para campo tipado.
var consumidos = []string{"smart", "thinpool"}

// soOsNaoConsumidos tira do payload o que ja foi interpretado.
//
// Extras existe para que um timer novo possa depositar um JSON em
// /run/sysmon/ e chegar aos clientes sem recompilar nada - e isso continua
// valendo para qualquer coletor novo. Mas o smartctl cru sozinho eram 9 KB
// dos 13 KB de uma resposta, repetidos a cada poll de cada cliente, para um
// dado que ninguem le: a tabela ja vai normalizada em blocos[].smart. Numa
// frota de vinte hosts isso e a diferenca entre 80 KB e 260 KB por ciclo.
//
// O JSON cru continua no host, em /run/sysmon/smart.json, para quem precisar
// depurar.
func soOsNaoConsumidos(extras map[string]metricas.Extra) map[string]metricas.Extra {
	if len(extras) == 0 {
		return extras
	}
	out := make(map[string]metricas.Extra, len(extras))
	for k, v := range extras {
		out[k] = v
	}
	for _, k := range consumidos {
		delete(out, k)
	}
	return out
}

// anotarHistorico preenche os deltas e grava quando a serie de fato avancou.
//
// A gravacao acontece no maximo uma vez por hora por disco (e o intervalo de
// amostragem), entao esta no caminho da coleta sem custar IO por ciclo. Falha
// de escrita e logada uma vez e ignorada: um agente que para de responder
// porque nao conseguiu gravar historico seria pior que um sem historico.
func (c *Coletor) anotarHistorico(s *metricas.Smart) {
	if c.historico == nil || s == nil {
		return
	}
	if !c.historico.Aplicar(s, time.Now()) {
		return
	}
	if err := c.historico.Salvar(); err != nil && !c.avisouHistorico {
		c.avisouHistorico = true
		log.Printf("AVISO: nao consegui gravar o historico SMART (%v); as "+
			"regras de taxa ficarao sem dados", err)
	}
}

// Iniciar faz duas coletas rapidas em sequencia para que as taxas ja saiam
// preenchidas na primeira resposta, em vez de virem null ate o segundo ciclo,
// e so depois entra no ritmo normal.
func (c *Coletor) Iniciar(parar <-chan struct{}, pulso func()) {
	c.coletar()
	time.Sleep(400 * time.Millisecond)
	s := c.coletar()

	c.mu.Lock()
	c.atual = s
	c.mu.Unlock()

	go c.rodar(parar, pulso)
}

func (c *Coletor) rodar(parar <-chan struct{}, pulso func()) {
	tic := time.NewTicker(c.intervalo)
	defer tic.Stop()
	for {
		select {
		case <-parar:
			return
		case <-tic.C:
			c.umCiclo(pulso)
		}
	}
}

// umCiclo isola o panico de uma coleta. Um /proc malformado num host especifico
// nao pode matar o agente nos outros; a falha e contada e exposta no /health.
func (c *Coletor) umCiclo(pulso func()) {
	defer func() {
		if r := recover(); r != nil {
			c.mu.Lock()
			c.falhas++
			n := c.falhas
			c.mu.Unlock()
			log.Printf("coletor: panico na coleta #%d: %v", n, r)
		}
	}()

	s := c.coletar()
	c.mu.Lock()
	c.atual = s
	c.falhas = 0
	c.mu.Unlock()

	// So avisa o watchdog do systemd depois de uma coleta boa: se a coleta
	// parar de funcionar, o systemd reinicia o servico sozinho em vez de
	// deixar um agente vivo servindo dado congelado.
	if pulso != nil {
		pulso()
	}
}

// ColetarAgora faz uma coleta sincrona e guarda o resultado.
//
// Existe exportada porque dois usos precisam dela sem o laco: o modo local
// do cliente, que coleta uma vez e desenha, e os testes do servidor, que
// precisam de um snapshot pronto antes da primeira requisicao.
func (c *Coletor) ColetarAgora() metricas.Snapshot {
	s := c.coletar()
	c.mu.Lock()
	c.atual = s
	c.mu.Unlock()
	return s
}

// DefinirSnapshot substitui a foto atual. So para teste: e o unico jeito de
// exercitar "o coletor travou e o dado esta velho" sem esperar de verdade.
func (c *Coletor) DefinirSnapshot(s metricas.Snapshot) {
	c.mu.Lock()
	c.atual = s
	c.mu.Unlock()
}

// Snapshot devolve a ultima foto, carimbada com a idade real do dado.
func (c *Coletor) Snapshot() metricas.Snapshot {
	c.mu.RLock()
	s := c.atual
	s.ColetorFalhas = c.falhas
	c.mu.RUnlock()

	if s.TS > 0 {
		s.IdadeS = arred(float64(time.Now().UnixNano())/1e9-s.TS, 1)
	}
	return s
}

// Saudavel: a coletora ainda esta produzindo amostras. Da folga de quatro
// intervalos antes de reclamar, para nao acusar falha num pico de IO.
func (c *Coletor) Saudavel() (bool, metricas.Snapshot) {
	s := c.Snapshot()
	limite := math.Max(4*c.intervalo.Seconds(), 30)
	return s.TS > 0 && s.IdadeS < limite, s
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "?"
	}
	return h
}
