package nucleo

import (
	"testing"

	"sysmon/internal/metricas"
)

func TestTrocarNaoApagaAsLeituras(t *testing.T) {
	// Salvar um limiar ou aceitar um alerta reconstroi os monitores. Se o
	// estado nao fosse carregado junto, a frota inteira piscaria "sem dados"
	// ate a proxima coleta - a tela apagando tudo bem no momento em que o
	// usuario esta mexendo nela.
	bruto := map[string]any{"hosts": []any{
		map[string]any{"nome": "pve", "url": "http://a/metrics", "token": "t"},
		map[string]any{"nome": "nas", "url": "http://b/metrics", "token": "t"},
	}}
	cfg, err := ConfigDe(bruto)
	if err != nil {
		t.Fatal(err)
	}
	f := NovaFrota(cfg, nil)
	f.DefinirEstado("pve", Estado{Dados: &metricas.Snapshot{Host: "pve", IntervaloS: 5}})
	f.DefinirEstado("nas", Estado{Dados: &metricas.Snapshot{Host: "nas", IntervaloS: 5}})

	// Uma mudanca que nao mexe em host nenhum.
	nova := cfg
	nova.Limiares.Reconhecidos = Reconhecimentos{"x": {Valor: "1"}}
	f.Trocar(nova)

	for _, l := range f.Estados() {
		if l.Estado.Dados == nil {
			t.Errorf("%s perdeu a leitura ao trocar a configuracao", l.Host.Nome)
		}
	}
}

func TestUrlNovaNaoHerdaALeituraAntiga(t *testing.T) {
	// Se a url mudou, e outra maquina. Herdar a leitura seria mostrar dado de
	// um host como se fosse de outro - pior que mostrar "sem dados".
	cfg, err := ConfigDe(map[string]any{"hosts": []any{
		map[string]any{"nome": "pve", "url": "http://a/metrics", "token": "t"}}})
	if err != nil {
		t.Fatal(err)
	}
	f := NovaFrota(cfg, nil)
	f.DefinirEstado("pve", Estado{Dados: &metricas.Snapshot{Host: "pve", IntervaloS: 5}})

	trocada, err := ConfigDe(map[string]any{"hosts": []any{
		map[string]any{"nome": "pve", "url": "http://OUTRO/metrics", "token": "t"}}})
	if err != nil {
		t.Fatal(err)
	}
	f.Trocar(trocada)

	if f.Estados()[0].Estado.Dados != nil {
		t.Fatal("herdou a leitura de outra url")
	}
}
