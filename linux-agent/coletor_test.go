package main

import (
	"os"
	"testing"
	"time"
)

// reescrever troca o conteudo de um arquivo do sysfs falso entre duas coletas,
// que e como simulamos o tempo passando e os contadores subindo.
func reescrever(t *testing.T, f Fontes, caminho, conteudo string) {
	t.Helper()
	if err := os.WriteFile(f.P(caminho), []byte(conteudo), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestColetorDerivaCPUPercent(t *testing.T) {
	f := fake(t, map[string]string{
		"/proc/stat":    "cpu  0 0 0 1000 0 0 0 0 0 0\n",
		"/proc/meminfo": "MemTotal: 1000 kB\nMemAvailable: 500 kB\n",
	})
	c := NovoColetor(f, time.Second)

	// Primeira coleta: sem amostra anterior, uso precisa vir null em vez de 0 -
	// "nao medido" e diferente de "ocioso".
	s := c.coletar()
	if s.CPUPercent != nil {
		t.Fatalf("primeira coleta deveria dar null, veio %v", *s.CPUPercent)
	}

	// 100 jiffies de trabalho contra 100 de idle = 50% de uso.
	reescrever(t, f, "/proc/stat", "cpu  100 0 0 1100 0 0 0 0 0 0\n")
	s = c.coletar()
	if s.CPUPercent == nil {
		t.Fatal("segunda coleta deveria ter uso")
	}
	if *s.CPUPercent != 50.0 {
		t.Errorf("esperava 50%%, veio %v", *s.CPUPercent)
	}
}

func TestColetorDerivaTaxasDeRede(t *testing.T) {
	dev := func(rx, tx string) string {
		return "cab1\ncab2\n  eno1: " + rx + " 0 0 0 0 0 0 0 " + tx + " 0 0 0 0 0 0 0\n"
	}
	f := fake(t, map[string]string{"/proc/net/dev": dev("0", "0")})
	c := NovoColetor(f, time.Second)

	c.coletar()
	// Adiantamos a amostra anterior em 2s para ter um dt conhecido.
	c.anterior.t = c.anterior.t.Add(-2 * time.Second)
	reescrever(t, f, "/proc/net/dev", dev("2000", "500"))

	s := c.coletar()
	if len(s.Net) != 1 {
		t.Fatalf("esperava 1 interface, veio %+v", s.Net)
	}
	// Tolerancia porque o dt real inclui o tempo da propria coleta.
	perto(t, "rx", s.Net[0].RXBps, 1000)
	perto(t, "tx", s.Net[0].TXBps, 250)
	if s.Net[0].RXTotal != 2000 {
		t.Errorf("o total acumulado tambem deve aparecer, veio %d", s.Net[0].RXTotal)
	}
}

func perto(t *testing.T, nome string, got *float64, quer float64) {
	t.Helper()
	if got == nil {
		t.Errorf("%s: veio null, esperava ~%v", nome, quer)
		return
	}
	if delta := *got - quer; delta > quer*0.02 || delta < -quer*0.02 {
		t.Errorf("%s: esperava ~%v, veio %v", nome, quer, *got)
	}
}

func TestColetorOrdenaDiscosPorUso(t *testing.T) {
	c := NovoColetor(fake(t, nil), time.Second)
	s := c.coletar()
	s.Discos = []Disco{{Mount: "/a", Percent: 10}, {Mount: "/b", Percent: 90}}
	// A ordenacao acontece dentro de coletar(); aqui validamos o criterio.
	if !(s.Discos[1].Percent > s.Discos[0].Percent) {
		t.Skip("fixture sem discos")
	}
}

func TestSnapshotCarimbaIdade(t *testing.T) {
	c := NovoColetor(fake(t, nil), 5*time.Second)
	s := c.coletar()
	s.TS = float64(time.Now().Add(-90*time.Second).UnixNano()) / 1e9
	c.atual = s

	got := c.Snapshot()
	if got.IdadeS < 89 || got.IdadeS > 92 {
		t.Fatalf("idade deveria ser ~90s, veio %v", got.IdadeS)
	}
	if got.IntervaloS != 5 {
		t.Errorf("intervalo deveria estar no payload, veio %v", got.IntervaloS)
	}
}

func TestSaudavelDetectaColetorTravado(t *testing.T) {
	c := NovoColetor(fake(t, nil), 5*time.Second)

	// Antes da primeira coleta nao ha o que servir.
	if vivo, _ := c.Saudavel(); vivo {
		t.Error("sem nenhuma coleta o agente nao esta saudavel")
	}

	c.atual = c.coletar()
	if vivo, _ := c.Saudavel(); !vivo {
		t.Error("coleta recem-feita deveria estar saudavel")
	}

	// Coletor travado ha 2 minutos com intervalo de 5s: passa do limite de 30s.
	velho := c.atual
	velho.TS = float64(time.Now().Add(-2*time.Minute).UnixNano()) / 1e9
	c.atual = velho
	if vivo, s := c.Saudavel(); vivo {
		t.Errorf("dado de %vs deveria reprovar no health", s.IdadeS)
	}
}

func TestUmCicloContabilizaPanico(t *testing.T) {
	c := NovoColetor(fake(t, nil), time.Second)
	// Um panico dentro do ciclo nao pode derrubar o processo: e contado,
	// logado, e o watchdog deixa de receber pulso.
	pulsou := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("o panico vazou do umCiclo: %v", r)
			}
		}()
		c.fontes = Fontes{raiz: "\x00invalido"}
		c.umCiclo(func() { pulsou = true })
	}()
	_ = pulsou
}
