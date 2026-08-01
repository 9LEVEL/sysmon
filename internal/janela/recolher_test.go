package janela

import (
	"testing"

	"sysmon/internal/metricas"
	"sysmon/internal/nucleo"
)

func bancadaDoisHosts(t *testing.T) *bancada {
	t.Helper()
	b := novaBancada(t,
		nucleo.Host{Nome: "pve", URL: "http://a/metrics", Token: "t"},
		nucleo.Host{Nome: "nas", URL: "http://b/metrics", Token: "t"})
	pct := 40.0
	for _, n := range []string{"pve", "nas"} {
		b.j.frota.DefinirEstado(n, nucleo.Estado{Dados: &metricas.Snapshot{
			IntervaloS: 5, Host: n, V: "5.4.0", CPUs: 8,
			CPUModelo: "Intel(R) Core(TM) i7-9700K", CPUPercent: &pct,
			Mem: metricas.Mem{Total: 16 << 30, Usado: 8 << 30, Percent: &pct},
		}})
	}
	b.j.coletar()
	return b
}

func TestCliqueNoHostRecolheOBloco(t *testing.T) {
	// Com dez hosts a arvore nao cabe na tela. Recolher o que ja foi
	// conferido e o que devolve a comparacao de relance.
	b := bancadaDoisHosts(t)
	b.quadro()
	if b.j.recolhidos["PVE"] {
		t.Fatal("nasceu recolhido")
	}
	// A linha do host e a primeira da arvore, logo abaixo do cabecalho e do
	// grafico. Clicar nela alterna.
	b.j.mu.Lock()
	nome := ""
	for _, l := range b.j.linhas {
		if l.Host {
			nome = l.Nome
			break
		}
	}
	b.j.mu.Unlock()
	if nome == "" {
		t.Fatal("nao achou a linha do host")
	}

	c := b.j.clicavelHost(nome)
	if c == nil {
		t.Fatal("sem alvo de clique")
	}
	// O clique real depende da posicao da lista; aqui exercitamos o efeito,
	// que e o que o teste precisa fixar.
	b.j.recolhidos[nome] = true
	b.j.salvarEstado()

	outra := novaBancada(t)
	outra.j.caminho = b.j.caminho
	outra.j.carregarEstado()
	if !outra.j.recolhidos[nome] {
		t.Fatalf("o recolhimento nao sobreviveu ao arquivo: %v", outra.j.recolhidos)
	}
}

func TestMargemEsquerdaEUmPreset(t *testing.T) {
	b := novaBancada(t)
	if b.j.margemEsq != 0 {
		t.Fatalf("padrao = %d", b.j.margemEsq)
	}
	b.j.margemEsq = 10
	b.j.salvarEstado()

	outra := novaBancada(t)
	outra.j.caminho = b.j.caminho
	outra.j.carregarEstado()
	if outra.j.margemEsq != 10 {
		t.Fatalf("margem = %d apos reabrir", outra.j.margemEsq)
	}
}

func TestModeloDoProcessadorVaiParaOCabecalho(t *testing.T) {
	// Ele e identidade da maquina, como o sistema operacional - e nao uma
	// medida que muda. Na linha de cpu ocupava, a cada ciclo, a coluna onde o
	// resto varia.
	b := bancadaDoisHosts(t)
	b.j.mu.Lock()
	defer b.j.mu.Unlock()
	for _, l := range b.j.linhas {
		if l.Host {
			if !contemTexto(l.Detalhe, "i7-9700K") {
				t.Fatalf("cabecalho sem o processador: %q", l.Detalhe)
			}
		}
		if l.Nome == "cpu" && contemTexto(l.Detalhe, "i7") {
			t.Fatalf("continua na linha de cpu: %q", l.Detalhe)
		}
	}
}

func contemTexto(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func TestCliqueRealNoCabecalhoRecolhe(t *testing.T) {
	// Sem o grafico do topo, a primeira linha da arvore comeca logo abaixo do
	// cabecalho - e da para injetar o clique na coordenada exata.
	b := bancadaDoisHosts(t)
	b.j.oculto = map[string]bool{"c:scope": true, "sec:TELA": true}
	b.j.coletar()
	b.quadro()

	y := AltCabec + AltLinhaHost/2
	b.clique(120, y)

	if len(b.j.recolhidos) != 1 {
		t.Fatalf("recolhidos = %v; o clique na linha do host nao pegou",
			b.j.recolhidos)
	}
	// E clicar de novo expande.
	b.clique(120, y)
	if len(b.j.recolhidos) != 0 {
		t.Fatalf("recolhidos = %v; o segundo clique nao expandiu", b.j.recolhidos)
	}
}
