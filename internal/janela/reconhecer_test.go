package janela

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"sysmon/internal/metricas"
	"sysmon/internal/nucleo"
)

// bancadaComAlerta monta uma janela com um host cujo RAID esta degradado -
// alerta critico, reconhecivel, com valor estavel (o mapa de discos).
func bancadaComAlerta(t *testing.T) *bancada {
	t.Helper()
	b := novaBancada(t, nucleo.Host{Nome: "pve", URL: "http://x/metrics", Token: "t"})
	deg := true
	s := &metricas.Snapshot{IntervaloS: 5, Host: "pve", V: "5.2.0",
		Raid: []metricas.RaidArray{{Nome: "md0", Estado: "ativo",
			Discos: "U_", Degradado: &deg}}}
	b.j.frota.DefinirEstado("pve", nucleo.Estado{Dados: s})
	b.j.coletar()
	return b
}

func alertasDaJanela(b *bancada) []nucleo.Alerta {
	b.j.mu.Lock()
	defer b.j.mu.Unlock()
	return append([]nucleo.Alerta(nil), b.j.alertas...)
}

func TestAceitarUmAlertaLimpaORodapeEACor(t *testing.T) {
	// O pedido inteiro num teste: aceitar esconde o aviso e a cor volta ao
	// normal, porque o nivel e recalculado do que sobrou.
	b := bancadaComAlerta(t)
	if a := alertasDaJanela(b); len(a) != 1 {
		t.Fatalf("esperava 1 alerta, veio %+v", a)
	}
	b.j.mu.Lock()
	corAntes := b.j.corResumo
	b.j.mu.Unlock()
	if corAntes != nucleo.Critico {
		t.Fatalf("cor = %d, queria Critico", corAntes)
	}

	b.j.abrirReconhecer()
	d := b.j.dlgReconhecer
	if len(d.linhas) != 1 || d.linhas[0].botao == nil {
		t.Fatalf("a tela nao ofereceu o aceite: %+v", d.linhas)
	}
	rec := b.j.copiaReconhecidos()
	rec[d.linhas[0].a.Chave] = nucleo.Reconhecido{Valor: d.linhas[0].a.Valor}
	b.j.gravarReconhecidos(d, rec)

	if a := alertasDaJanela(b); len(a) != 0 {
		t.Fatalf("o alerta continua no rodape: %+v", a)
	}
	b.j.mu.Lock()
	corDepois := b.j.corResumo
	b.j.mu.Unlock()
	if corDepois != nucleo.OK {
		t.Fatalf("cor = %d; nao voltou ao normal", corDepois)
	}
}

func TestPiorarVoltaAAlertarMesmoAceito(t *testing.T) {
	// U_ aceito nao aceita __: um disco a menos e um evento novo.
	b := bancadaComAlerta(t)
	b.j.abrirReconhecer()
	d := b.j.dlgReconhecer
	rec := b.j.copiaReconhecidos()
	rec[d.linhas[0].a.Chave] = nucleo.Reconhecido{Valor: d.linhas[0].a.Valor}
	b.j.gravarReconhecidos(d, rec)
	if len(alertasDaJanela(b)) != 0 {
		t.Fatal("nao silenciou")
	}

	deg := true
	b.j.frota.DefinirEstado("pve", nucleo.Estado{Dados: &metricas.Snapshot{
		IntervaloS: 5, Host: "pve", V: "5.2.0",
		Raid: []metricas.RaidArray{{Nome: "md0", Estado: "ativo",
			Discos: "__", Degradado: &deg}}}})
	b.j.coletar()

	a := alertasDaJanela(b)
	if len(a) != 1 {
		t.Fatalf("nao voltou a alertar: %+v", a)
	}
	if !strings.Contains(a[0].Texto, "__") {
		t.Fatalf("alerta = %q", a[0].Texto)
	}
}

func TestAceiteSobreviveAoArquivo(t *testing.T) {
	// Uma aceitacao que some no proximo arranque e pior que uma que nao
	// aconteceu: o usuario acredita ter resolvido.
	b := bancadaComAlerta(t)
	b.j.abrirReconhecer()
	d := b.j.dlgReconhecer
	rec := b.j.copiaReconhecidos()
	rec[d.linhas[0].a.Chave] = nucleo.Reconhecido{Valor: d.linhas[0].a.Valor}
	b.j.gravarReconhecidos(d, rec)
	if d.erro != "" {
		t.Fatalf("erro ao gravar: %s", d.erro)
	}

	dados, err := os.ReadFile(b.j.caminho)
	if err != nil {
		t.Fatal(err)
	}
	var bruto map[string]any
	if err := json.Unmarshal(dados, &bruto); err != nil {
		t.Fatal(err)
	}
	m, ok := bruto["reconhecidos"].(map[string]any)
	if !ok || len(m) != 1 {
		t.Fatalf("config.json = %s", dados)
	}
	// E o config releito devolve o mesmo reconhecimento.
	cfg, err := nucleo.ConfigDe(bruto)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Limiares.Reconhecidos) != 1 {
		t.Fatalf("releitura perdeu: %+v", cfg.Limiares.Reconhecidos)
	}
}

func TestOFixoNaoOfereceAceite(t *testing.T) {
	// CPU alta nao e reconhecivel: o valor muda sozinho no ciclo seguinte, e a
	// resposta certa e o limiar. A tela mostra a linha, mas sem botao.
	b := novaBancada(t, nucleo.Host{Nome: "pve", URL: "http://x/metrics", Token: "t"})
	cem := 100.0
	quente := 95.0
	b.j.frota.DefinirEstado("pve", nucleo.Estado{Dados: &metricas.Snapshot{
		IntervaloS: 5, Host: "pve", V: "5.2.0",
		CPUTemp: &quente, CPUCrit: &cem}})
	b.j.coletar()
	b.j.abrirReconhecer()

	d := b.j.dlgReconhecer
	if len(d.linhas) == 0 {
		t.Fatal("nao listou o alerta de temperatura")
	}
	for _, l := range d.linhas {
		if !l.fixo {
			t.Fatalf("ofereceu aceite para %q", l.a.Chave)
		}
		if l.botao != nil {
			t.Fatalf("desenhou botao para %q", l.a.Chave)
		}
	}
}

func TestBotaoAcendeQuandoHaAceitos(t *testing.T) {
	// Silencio precisa ser visivel: o botao aceso e a linha no rodape sao o
	// que impede "aceitei" de virar "esqueci".
	b := bancadaComAlerta(t)
	if s := b.j.resumoReconhecidos(); s != "" {
		t.Fatalf("resumo = %q sem nada aceito", s)
	}
	b.j.abrirReconhecer()
	d := b.j.dlgReconhecer
	rec := b.j.copiaReconhecidos()
	rec[d.linhas[0].a.Chave] = nucleo.Reconhecido{Valor: d.linhas[0].a.Valor}
	b.j.gravarReconhecidos(d, rec)

	if s := b.j.resumoReconhecidos(); !strings.Contains(s, "1 alerta aceito") {
		t.Fatalf("resumo = %q", s)
	}
	if !strings.Contains(textoReconhecer(b.j), "1 alerta aceito") {
		t.Fatalf("dica = %q", textoReconhecer(b.j))
	}
}
