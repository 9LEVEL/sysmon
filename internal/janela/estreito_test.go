package janela

import (
	"testing"

	"sysmon/internal/metricas"
	"sysmon/internal/nucleo"
	"sysmon/internal/tela"
)

// A ferramenta e usada encostada na lateral da tela, como um widget - uns 2/5
// da largura. Estes testes fixam o que quebrava nessa largura.

func bancadaEstreita(t *testing.T) *bancada {
	t.Helper()
	b := novaBancada(t, nucleo.Host{Nome: "pve", URL: "http://x/metrics", Token: "t"})
	b.tam.X = MinLarg // 470: o minimo que a janela aceita
	quente, cem := 92.0, 100.0
	pct := 61.2
	b.j.frota.DefinirEstado("pve", nucleo.Estado{Dados: &metricas.Snapshot{
		IntervaloS: 5, Host: "pve", V: "5.3.0",
		CPUTemp: &quente, CPUCrit: &cem, CPUPercent: &pct,
		Mem: metricas.Mem{Total: 16 << 30, Usado: 10 << 30, Percent: &pct},
		SO:  metricas.SO{Nome: "Debian GNU/Linux 12"},
	}})
	b.j.coletar()
	return b
}

func TestOsDialogosDesenhamNaLarguraMinima(t *testing.T) {
	// Nao ha como afirmar "esta bonito" num teste, mas da para afirmar que
	// nada estoura: o Gio entra em panico com constraint negativa, que e o
	// que uma largura calculada por subtracao produz quando a janela encolhe
	// mais do que o codigo esperava.
	for _, c := range []struct {
		nome  string
		abrir func(*Janela)
	}{
		{"hosts", (*Janela).abrirHosts},
		{"exibir", (*Janela).abrirExibir},
		{"alertas", (*Janela).abrirAlertas},
		{"reconhecer", (*Janela).abrirReconhecer},
	} {
		t.Run(c.nome, func(t *testing.T) {
			b := bancadaEstreita(t)
			c.abrir(b.j)
			// Tres quadros: o Gio so entrega eventos no seguinte, e e no
			// terceiro que a lista ja tem tamanho medido.
			b.quadro()
			b.quadro()
			b.quadro()
			if b.j.dialogo == semDialogo {
				t.Fatal("o dialogo fechou sozinho")
			}
		})
	}
}

func TestEscapeFechaODialogo(t *testing.T) {
	// Modal que so fecha por um botao especifico e uma armadilha em janela
	// estreita, que e onde os botoes ficam mais apertados - justamente quando
	// mais se quer sair.
	b := bancadaEstreita(t)
	b.j.abrirReconhecer()
	b.quadro()
	b.escape()
	if b.j.dialogo != semDialogo {
		t.Fatalf("dialogo = %d; o ESC nao fechou", b.j.dialogo)
	}
}

func TestColunasNaoSeSobrepoemNaJanelaEstreita(t *testing.T) {
	// A linha do host tem nome a esquerda, detalhe no meio e valor a direita.
	// Em 470px o detalhe ("10.0G de 16.0G · Debian GNU/Linux 12") passava por
	// cima do valor ("92C · cpu 61% · ram 61%") e as duas informacoes ficavam
	// ilegiveis - bem na largura em que a ferramenta e mais usada.
	b := bancadaEstreita(t)
	b.quadro()

	var host tela.Linha
	b.j.mu.Lock()
	for _, l := range b.j.linhas {
		if l.Host {
			host = l
			break
		}
	}
	b.j.mu.Unlock()
	if host.Nome == "" {
		t.Fatal("nao achou a linha do host")
	}

	g := b.gtx()
	xDet := Margem + ColNome
	xValor := b.tam.X - Margem
	fim := xValor - b.j.Medir(g, host.Valor, CorpoHost, true) - 12
	cortado := b.j.cortarPara(g, host.Detalhe, fim-xDet)
	if larg := b.j.Medir(g, cortado, 12, true); xDet+larg > fim {
		t.Fatalf("o detalhe (%dpx) invade a coluna do valor (limite %d)",
			xDet+larg, fim)
	}
}
