package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"sysmon/internal/banco"
	"sysmon/internal/nucleo"
)

func TestMigraOsAceitesDoConfigParaOBanco(t *testing.T) {
	dir := t.TempDir()
	caminho := filepath.Join(dir, "config.json")
	antigo := map[string]any{
		"hosts": []any{
			map[string]any{"nome": "pve", "url": "http://a/metrics", "token": "t1"},
			map[string]any{"nome": "nas", "url": "http://b/metrics", "token": "t2"},
		},
		"reconhecidos": map[string]any{
			"smart:sda:host:desligamento_sujo": map[string]any{
				"valor": "89/267/33", "quando": 1234.0, "texto": "pve: sda"},
		},
	}
	dados, _ := json.Marshal(antigo)
	if err := os.WriteFile(caminho, dados, 0o600); err != nil {
		t.Fatal(err)
	}

	// A frota com um host so, como no `term --host pve`: a migracao nao pode
	// regravar o arquivo a partir dela.
	cfg, err := nucleo.CarregarConfig(caminho)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Hosts = cfg.Hosts[:1]
	f := nucleo.NovaFrota(cfg, nil)

	b := abrirBanco(caminho, f)
	if b == nil {
		t.Fatal("o banco nao abriu")
	}
	defer b.Fechar()

	rec, err := b.Reconhecidos()
	if err != nil || rec["smart:sda:host:desligamento_sujo"].Quando != 1234 {
		t.Fatalf("nao importou com a data original: %+v %v", rec, err)
	}
	if len(f.Cfg().Limiares.Reconhecidos) != 1 {
		t.Fatal("a frota nao recebeu os aceites do banco")
	}

	depois, err := nucleo.CarregarConfig(caminho)
	if err != nil {
		t.Fatal(err)
	}
	if _, tem := depois.Bruto["reconhecidos"]; tem {
		t.Fatal("os aceites continuam no config.json")
	}
	if len(depois.Hosts) != 2 || depois.Hosts[1].Token != "t2" {
		t.Fatalf("a migracao mexeu nos hosts: %+v", depois.Hosts)
	}
	if _, err := os.Stat(filepath.Join(dir, banco.NomeArquivo)); err != nil {
		t.Fatal("o banco nao ficou ao lado do config:", err)
	}
}
