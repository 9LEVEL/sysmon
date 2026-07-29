// O Coletor amostra o host numa goroutine propria e guarda o resultado pronto.
//
// Na v1 a coleta acontecia dentro do handler HTTP, segurando um mutex: um sysfs
// lento travava todas as requisicoes, e as taxas eram calculadas sobre o
// intervalo aleatorio entre dois clientes. Agora o caminho da requisicao nao
// toca em disco - so serializa um struct ja pronto. N clientes custam o mesmo
// que um, e as taxas saem de uma janela fixa e conhecida.
package main

import (
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
	net    map[string]AmostraNet
	io     map[string]AmostraIO
}

type Coletor struct {
	fontes    Fontes
	intervalo time.Duration

	mu       sync.RWMutex
	atual    Snapshot
	falhas   int64
	anterior *amostra
}

func NovoColetor(f Fontes, intervalo time.Duration) *Coletor {
	return &Coletor{fontes: f, intervalo: intervalo}
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

func (c *Coletor) coletar() Snapshot {
	ag := c.bruto()
	ant := c.anterior
	c.anterior = ag

	var dt float64
	if ant != nil {
		dt = ag.t.Sub(ant.t).Seconds()
	}

	temps := c.fontes.Temps()
	s := Snapshot{
		V:          versao,
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
		DiskIO:     c.taxasIO(ant, ag, dt),
		Net:        c.taxasNet(ant, ag, dt),
		Raid:       c.fontes.Raid(),
		Guests:     c.fontes.Guests(),
		Extras:     c.fontes.Extras(),
		PlacaMae:   c.fontes.PlacaMae(),
		IntervaloS: c.intervalo.Seconds(),
	}
	s.CPUModelo, s.CPUMHz = c.fontes.CPUInfo()
	s.Thinpools = Thinpools(s.Extras)

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

func (c *Coletor) taxasNet(ant, ag *amostra, dt float64) []Net {
	nomes := make([]string, 0, len(ag.net))
	for n := range ag.net {
		nomes = append(nomes, n)
	}
	sort.Strings(nomes)

	out := make([]Net, 0, len(nomes))
	for _, nome := range nomes {
		a := ag.net[nome]
		p, tem := AmostraNet{}, false
		if ant != nil {
			p, tem = ant.net[nome]
		}
		up, mbps := c.fontes.NetEstado(nome)
		out = append(out, Net{
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

func (c *Coletor) taxasIO(ant, ag *amostra, dt float64) []DiskIO {
	nomes := make([]string, 0, len(ag.io))
	for n := range ag.io {
		nomes = append(nomes, n)
	}
	sort.Strings(nomes)

	out := make([]DiskIO, 0, len(nomes))
	for _, nome := range nomes {
		a := ag.io[nome]
		p, tem := AmostraIO{}, false
		if ant != nil {
			p, tem = ant.io[nome]
		}
		d := DiskIO{
			Disco:      nome,
			LeituraBps: taxa(a.LidosB, p.LidosB, tem, dt),
			EscritaBps: taxa(a.EscritosB, p.EscritosB, tem, dt),
		}
		// io_ms sobe 1000ms por segundo de parede quando o disco esta 100%
		// ocupado; a razao entre os dois e a taxa de ocupacao.
		if ms := taxa(a.IOms, p.IOms, tem, dt); ms != nil {
			d.UtilPercent = f64(arred(math.Min(*ms/10, 100), 1))
		}
		out = append(out, d)
	}
	return out
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

// Snapshot devolve a ultima foto, carimbada com a idade real do dado.
func (c *Coletor) Snapshot() Snapshot {
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
func (c *Coletor) Saudavel() (bool, Snapshot) {
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
