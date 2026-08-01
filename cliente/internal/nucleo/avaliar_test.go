package nucleo

import (
	"strings"
	"testing"

	"sysmon-cliente/internal/metricas"
)

func f(v float64) *float64 { return &v }
func i(v int64) *int64     { return &v }
func b(v bool) *bool       { return &v }

func snap() *metricas.Snapshot {
	return &metricas.Snapshot{IntervaloS: 5}
}

func est(s *metricas.Snapshot) Estado { return Estado{Dados: s} }

func TestOffline(t *testing.T) {
	n, alertas := Avaliar(Estado{Erro: "timeout"}, LimiaresPadrao())
	if n != Offline {
		t.Fatalf("nivel = %d, queria Offline", n)
	}
	if len(alertas) != 1 || alertas[0] != "timeout" {
		t.Fatalf("alertas = %v", alertas)
	}
}

func TestSemDadosNemErroAindaEOffline(t *testing.T) {
	// Estado vazio nao pode virar OK: seria afirmar saude sobre um host do
	// qual nao sabemos nada.
	if n, _ := Avaliar(Estado{}, LimiaresPadrao()); n != Offline {
		t.Fatalf("nivel = %d, queria Offline", n)
	}
}

func TestTemperaturaUsaOCriticoDoSensor(t *testing.T) {
	lim := LimiaresPadrao()
	// Numa CPU com critico 100C, 0.75 significa aviso em 75C.
	if n := NivelTemp(f(76), f(100), lim); n != Aviso {
		t.Errorf("76C com crit 100 = %d, queria Aviso", n)
	}
	if n := NivelTemp(f(91), f(100), lim); n != Critico {
		t.Errorf("91C com crit 100 = %d, queria Critico", n)
	}
	// A mesma temperatura numa CPU com critico 120 ainda esta tranquila -
	// e esse o ponto de usar fracao em vez de numero fixo.
	if n := NivelTemp(f(76), f(120), lim); n != OK {
		t.Errorf("76C com crit 120 = %d, queria OK", n)
	}
	// Sem critico do sensor, cai no par fixo.
	if n := NivelTemp(f(71), nil, lim); n != Aviso {
		t.Errorf("71C sem crit = %d, queria Aviso", n)
	}
}

func TestSensorAusenteNaoAlerta(t *testing.T) {
	if n := NivelTemp(nil, f(100), LimiaresPadrao()); n != OK {
		t.Fatal("temperatura ausente virou alerta")
	}
}

func TestBootFicaDeForaDaAvaliacao(t *testing.T) {
	// /boot enche de kernel antigo e a ESP vive quase cheia: alertar nelas
	// ensina o usuario a ignorar alerta.
	s := snap()
	s.Discos = []metricas.Disco{
		{Mount: "/boot", Percent: 99},
		{Mount: "/boot/efi", Percent: 95},
	}
	if n, a := Avaliar(est(s), LimiaresPadrao()); n != OK {
		t.Fatalf("nivel = %d (%v), queria OK", n, a)
	}
}

func TestDiscoCheioAlerta(t *testing.T) {
	s := snap()
	s.Discos = []metricas.Disco{{Mount: "/", Percent: 95}}
	n, alertas := Avaliar(est(s), LimiaresPadrao())
	if n != Critico {
		t.Fatalf("nivel = %d, queria Critico", n)
	}
	if len(alertas) != 1 || !strings.Contains(alertas[0], "disco / em 95%") {
		t.Fatalf("alertas = %v", alertas)
	}
}

func TestInodesContamMesmoComEspacoSobrando(t *testing.T) {
	// Inode esgotado quebra igual a disco cheio, e o df -h nao mostra.
	s := snap()
	s.Discos = []metricas.Disco{{Mount: "/", Percent: 10, InodesPercent: f(98)}}
	n, alertas := Avaliar(est(s), LimiaresPadrao())
	if n != Critico {
		t.Fatalf("nivel = %d, queria Critico", n)
	}
	if !strings.Contains(strings.Join(alertas, " "), "inodes") {
		t.Fatalf("alertas = %v", alertas)
	}
}

func TestRaidDegradado(t *testing.T) {
	s := snap()
	s.Raid = []metricas.RaidArray{{Nome: "md0", Discos: "[U_]", Degradado: b(true)}}
	if n, _ := Avaliar(est(s), LimiaresPadrao()); n != Critico {
		t.Fatalf("nivel = %d, queria Critico", n)
	}
}

func TestRaidComEstadoDesconhecidoNaoAlerta(t *testing.T) {
	// Degradado nil = o mapa nao pode ser lido. Chutar "degradado" seria
	// alarme falso; chutar "ok" seria pior.
	s := snap()
	s.Raid = []metricas.RaidArray{{Nome: "md0", Degradado: nil}}
	if n, _ := Avaliar(est(s), LimiaresPadrao()); n != OK {
		t.Fatalf("nivel = %d, queria OK", n)
	}
}

func TestUmSetorRealocadoJaAvisa(t *testing.T) {
	s := snap()
	s.Blocos = []metricas.Bloco{{Dev: "sda", Smart: &metricas.Smart{Realocados: i(1)}}}
	n, alertas := Avaliar(est(s), LimiaresPadrao())
	if n != Aviso {
		t.Fatalf("nivel = %d, queria Aviso", n)
	}
	if !strings.Contains(strings.Join(alertas, " "), "realocados") {
		t.Fatalf("alertas = %v", alertas)
	}
}

func TestSmartReprovadoECritico(t *testing.T) {
	s := snap()
	s.Blocos = []metricas.Bloco{{Dev: "sda", Smart: &metricas.Smart{Saude: "falha"}}}
	if n, _ := Avaliar(est(s), LimiaresPadrao()); n != Critico {
		t.Fatalf("nivel = %d, queria Critico", n)
	}
}

func TestPressaoPSI(t *testing.T) {
	s := snap()
	s.Pressure = map[string]map[string]float64{"io": {"some_avg60": 75}}
	n, alertas := Avaliar(est(s), LimiaresPadrao())
	if n != Critico {
		t.Fatalf("nivel = %d, queria Critico", n)
	}
	if !strings.Contains(strings.Join(alertas, " "), "IO") {
		t.Fatalf("alertas = %v", alertas)
	}
}

func TestColetaParadaAvisa(t *testing.T) {
	// O agente responde, mas a goroutine de coleta dele travou: o dado na
	// tela esta velho e nada indicaria isso.
	s := snap()
	s.IdadeS = 120
	n, alertas := Avaliar(est(s), LimiaresPadrao())
	if n != Aviso {
		t.Fatalf("nivel = %d, queria Aviso", n)
	}
	if !strings.Contains(strings.Join(alertas, " "), "coleta parada") {
		t.Fatalf("alertas = %v", alertas)
	}
}

func TestIdadeNormalNaoAvisa(t *testing.T) {
	s := snap()
	s.IdadeS = 6 // intervalo 5, fator 4 => limite 30
	if n, _ := Avaliar(est(s), LimiaresPadrao()); n != OK {
		t.Fatal("idade normal virou alerta")
	}
}

func TestNivelEOMaximoNaoASoma(t *testing.T) {
	// Varios avisos nao viram critico, e um critico domina os avisos.
	s := snap()
	s.Discos = []metricas.Disco{
		{Mount: "/a", Percent: 85}, {Mount: "/b", Percent: 85},
		{Mount: "/c", Percent: 85},
	}
	if n, _ := Avaliar(est(s), LimiaresPadrao()); n != Aviso {
		t.Fatalf("tres avisos viraram %d", n)
	}
	s.Discos = append(s.Discos, metricas.Disco{Mount: "/d", Percent: 95})
	if n, _ := Avaliar(est(s), LimiaresPadrao()); n != Critico {
		t.Fatalf("com um critico junto = %d", n)
	}
}

func TestLimiaresConfiguraveis(t *testing.T) {
	lim := LimiaresPadrao()
	lim.Disco = Par{50, 60}
	s := snap()
	s.Discos = []metricas.Disco{{Mount: "/", Percent: 55}}
	if n, _ := Avaliar(est(s), lim); n != Aviso {
		t.Fatalf("nivel = %d, queria Aviso com limiar em 50", n)
	}
}

func TestLimiaresDeIgnoraEntradaQuebrada(t *testing.T) {
	// Quem edita config.json a mao erra, e errar nao pode custar a tela.
	lim := LimiaresDe(map[string]any{"alertas": map[string]any{
		"disco":       []any{"muito", "cheio"}, // texto onde devia ser numero
		"ram":         []any{float64(60), float64(70)},
		"inexistente": []any{float64(1), float64(2)},
	}})
	padrao := LimiaresPadrao()
	if lim.Disco != padrao.Disco {
		t.Errorf("par quebrado nao voltou ao padrao: %v", lim.Disco)
	}
	if lim.RAM != (Par{60, 70}) {
		t.Errorf("par valido nao foi lido: %v", lim.RAM)
	}
}

func TestComoMapaFechaOCiclo(t *testing.T) {
	// O que a tela de alertas grava tem que ser lido de volta igual.
	orig := LimiaresPadrao()
	orig.PSI = Par{33, 66}
	bruto := map[string]any{"alertas": map[string]any{}}
	for nome, par := range orig.ComoMapa() {
		bruto["alertas"].(map[string]any)[nome] = []any{par[0], par[1]}
	}
	if volta := LimiaresDe(bruto); volta.PSI != orig.PSI {
		t.Fatalf("ida e volta perdeu o valor: %v", volta.PSI)
	}
}
