package janela

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"sysmon/internal/tela"
)

// Estado da janela: o que e preferencia de TELA, e nao configuracao.
//
// Vive num arquivo separado do config.json de proposito. O config tem os
// hosts e os limiares, que sao a configuracao do monitoramento e valem para
// qualquer cliente; o que esta escondido na tela e de quem esta olhando.
// Misturar os dois faria uma escolha de aparencia viajar junto com a
// configuracao ao copiar o arquivo para outra maquina.
type estadoJanela struct {
	Oculto      []string `json:"oculto"`
	Topo        bool     `json:"topo"`
	Larg        int      `json:"larg"`
	Alt         int      `json:"alt"`
	ScopeHost   string   `json:"scope_host"`
	ScopeMedida string   `json:"scope_medida"`
}

func (j *Janela) caminhoEstado() string {
	return filepath.Join(filepath.Dir(j.caminho), "sysmon-janela.json")
}

func (j *Janela) carregarEstado() {
	dados, err := os.ReadFile(j.caminhoEstado())
	if err != nil {
		return // sem estado ainda: os padroes valem
	}
	var e estadoJanela
	if err := json.Unmarshal(dados, &e); err != nil {
		return // estado corrompido nao pode impedir a janela de abrir
	}
	j.oculto = tela.Visiveis{}
	for _, c := range e.Oculto {
		j.oculto[c] = true
	}
	j.noTopo = e.Topo
	j.largSalva, j.altSalva = e.Larg, e.Alt
	j.scopeHost, j.scopeMedida = e.ScopeHost, e.ScopeMedida
}

func (j *Janela) salvarEstado() {
	var oculto []string
	for c, v := range j.oculto {
		if v {
			oculto = append(oculto, c)
		}
	}
	// Ordenado para o arquivo nao mudar sozinho entre gravacoes: diff limpo
	// e o que permite versionar isto sem ruido.
	sort.Strings(oculto)

	e := estadoJanela{Oculto: oculto, Topo: j.noTopo,
		Larg: j.largSalva, Alt: j.altSalva,
		ScopeHost: j.scopeHost, ScopeMedida: j.scopeMedida}
	dados, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return
	}
	// Falhar aqui nunca pode atrapalhar: preferencia de tela nao vale uma
	// mensagem de erro, muito menos um programa que nao abre.
	_ = os.WriteFile(j.caminhoEstado(), append(dados, '\n'), 0o600)
}
