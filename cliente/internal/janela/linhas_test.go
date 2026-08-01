package janela

import (
	"fmt"
	"strings"
	"testing"

	"sysmon-cliente/internal/metricas"
	"sysmon-cliente/internal/nucleo"
)

func f(v float64) *float64 { return &v }

func semSerie(string, string) []float64 { return nil }

func leitura(s *metricas.Snapshot) []nucleo.LeituraHost {
	return []nucleo.LeituraHost{{
		Host:   nucleo.Host{Nome: "pve"},
		Estado: nucleo.Estado{Dados: s},
	}}
}

func nomes(linhas []Linha) string {
	var n []string
	for _, l := range linhas {
		n = append(n, l.Nome)
	}
	return strings.Join(n, "|")
}

func TestHostOfflineTemUmaLinhaSo(t *testing.T) {
	// Sem dados nao ha secao para desenhar; repetir "—" em vinte linhas so
	// esconderia a unica informacao que importa.
	l := Montar([]nucleo.LeituraHost{{
		Host:   nucleo.Host{Nome: "pve"},
		Estado: nucleo.Estado{Erro: "conexao recusada"},
	}}, nucleo.LimiaresPadrao(), Visiveis{}, semSerie)

	if len(l) != 1 {
		t.Fatalf("%d linhas, queria 1: %s", len(l), nomes(l))
	}
	if l[0].Valor != "OFFLINE" || l[0].Detalhe != "conexao recusada" {
		t.Fatalf("linha = %+v", l[0])
	}
}

func TestSecaoVaziaNaoAparece(t *testing.T) {
	// Host sem ventoinha nao pode mostrar um cabecalho VENTOINHAS vazio.
	s := &metricas.Snapshot{IntervaloS: 5}
	l := Montar(leitura(s), nucleo.LimiaresPadrao(), Visiveis{}, semSerie)
	if strings.Contains(nomes(l), "VENTOINHAS") {
		t.Fatalf("secao vazia apareceu: %s", nomes(l))
	}
	if strings.Contains(nomes(l), "TEMPERATURA") {
		t.Fatalf("secao vazia apareceu: %s", nomes(l))
	}
}

func TestOcultarSecaoTiraOConteudoJunto(t *testing.T) {
	s := &metricas.Snapshot{IntervaloS: 5, CPUs: 8, CPUPercent: f(50)}
	visivel := Montar(leitura(s), nucleo.LimiaresPadrao(), Visiveis{}, semSerie)
	if !strings.Contains(nomes(visivel), "DESEMPENHO") {
		t.Fatal("secao nao apareceu quando devia")
	}
	escondido := Montar(leitura(s), nucleo.LimiaresPadrao(),
		Visiveis{"sec:DESEMPENHO": true}, semSerie)
	if strings.Contains(nomes(escondido), "cpu") {
		t.Fatalf("esconder a secao deixou o conteudo: %s", nomes(escondido))
	}
}

func TestBootNaoAparece(t *testing.T) {
	// Mesma regra da avaliacao: /boot enche de kernel antigo e alertar nele
	// ensina a ignorar alerta. Na tela vale igual.
	s := &metricas.Snapshot{IntervaloS: 5, Discos: []metricas.Disco{
		{Mount: "/", Percent: 40}, {Mount: "/boot", Percent: 99},
	}}
	l := Montar(leitura(s), nucleo.LimiaresPadrao(), Visiveis{}, semSerie)
	if strings.Contains(nomes(l), "/boot") {
		t.Fatalf("/boot apareceu: %s", nomes(l))
	}
}

func TestCincoDegrausDeCor(t *testing.T) {
	// O ponto dos cinco degraus: 3% e 30% precisam ser cores diferentes.
	// Com so ok/aviso/critico os dois eram iguais e a variacao sumia.
	lim := nucleo.LimiaresPadrao()
	vistas := map[string]bool{}
	for _, pct := range []float64{3, 30, 60, 85, 95} {
		c := CorMagnitude(pct, lim.Disco.Aviso, lim.Disco.Critico)
		vistas[fmt.Sprintf("%v", c)] = true
	}
	if len(vistas) != 5 {
		t.Fatalf("%d cores distintas, queria 5", len(vistas))
	}
}

func TestVentoinhasNaoDancam(t *testing.T) {
	// Mapa em Go nao tem ordem: sem ordenar, as linhas trocariam de lugar a
	// cada quadro e a tela ficaria inutilizavel.
	s := &metricas.Snapshot{IntervaloS: 5, Fans: map[string]int64{
		"hwmon/fan3": 1200, "hwmon/fan1": 900, "hwmon/fan2": 1000,
	}}
	primeiro := nomes(Montar(leitura(s), nucleo.LimiaresPadrao(), Visiveis{}, semSerie))
	for i := 0; i < 20; i++ {
		if n := nomes(Montar(leitura(s), nucleo.LimiaresPadrao(), Visiveis{}, semSerie)); n != primeiro {
			t.Fatalf("ordem mudou entre quadros:\n%s\n%s", primeiro, n)
		}
	}
}

func TestSmartReprovadoFicaVermelhoENoDetalhe(t *testing.T) {
	s := &metricas.Snapshot{IntervaloS: 5, Blocos: []metricas.Bloco{{
		Dev: "sda", Tamanho: 512 << 30,
		Smart: &metricas.Smart{Saude: "falha"},
	}}}
	l := Montar(leitura(s), nucleo.LimiaresPadrao(), Visiveis{}, semSerie)
	for _, x := range l {
		if x.Nome == "sda" {
			if x.Cor != Vermelho {
				t.Errorf("cor = %v, queria vermelho", x.Cor)
			}
			if !strings.Contains(x.Detalhe, "SMART REPROVOU") {
				t.Errorf("detalhe = %q", x.Detalhe)
			}
			return
		}
	}
	t.Fatalf("linha do disco nao apareceu: %s", nomes(l))
}

func TestInterfaceCaidaNaoPoluiATela(t *testing.T) {
	s := &metricas.Snapshot{IntervaloS: 5, Net: []metricas.Net{
		{Iface: "eno1", Up: true}, {Iface: "docker0", Up: false},
	}}
	l := Montar(leitura(s), nucleo.LimiaresPadrao(), Visiveis{}, semSerie)
	if strings.Contains(nomes(l), "docker0") {
		t.Fatalf("interface caida apareceu: %s", nomes(l))
	}
	if !strings.Contains(nomes(l), "eno1") {
		t.Fatalf("interface ativa sumiu: %s", nomes(l))
	}
}
