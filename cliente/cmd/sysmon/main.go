// Comando sysmon - o cliente.
//
// Um binario para a maquina de onde voce olha. Consulta os agentes por HTTP,
// avalia e mostra numa janela nativa, com o icone da bandeja mudando de cor
// pelo pior host. Sem servidor, sem banco, sem runtime instalado: e o que
// separa esta ferramenta de um Zabbix e de um script.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"gioui.org/app"

	"sysmon-cliente/internal/atualizar"
	"sysmon-cliente/internal/bandeja"
	"sysmon-cliente/internal/janela"
	"sysmon-cliente/internal/nucleo"
)

// Versao e sobrescrita no build: -ldflags "-X main.versao=..."
var versao = "dev"

func main() {
	// Subcomando antes das flags: `sysmon term --once` tem que funcionar, e
	// o flag padrao pararia no primeiro argumento que nao comeca com traco.
	if len(os.Args) > 1 && os.Args[1] == "term" {
		os.Exit(rodarTerminal(os.Args[2:]))
	}

	var (
		caminho = flag.String("config", "", "caminho do config.json")
		oculto  = flag.Bool("oculto", false, "abrir minimizado na bandeja")
		semTray = flag.Bool("sem-bandeja", false, "nao criar o icone da bandeja")
		semUp   = flag.Bool("sem-update", false, "nao verificar atualizacao")
		mostrar = flag.Bool("version", false, "imprime a versao e sai")
	)
	flag.Parse()

	if *mostrar {
		fmt.Println(versao)
		return
	}

	c := nucleo.AcharConfig(*caminho)
	cfg, err := nucleo.CarregarConfig(c)
	if err != nil {
		// Sem configuracao valida NAO morremos: a janela sobe com a frota
		// vazia e abre na tela de configuracao. Morrer aqui era o pior caso -
		// sem console, a mensagem ia para lugar nenhum e o usuario via apenas
		// um programa que nao abre.
		fmt.Fprintf(os.Stderr, "sem configuracao ainda (%v)\n", err)
		cfg, _ = nucleo.ConfigDe(nil)
	}
	if aviso := nucleo.AvisarPermissao(c); aviso != "" {
		fmt.Fprintf(os.Stderr, "aviso: %s\n", aviso)
	}

	frota := nucleo.NovaFrota(cfg, nil)
	frota.Iniciar()
	defer frota.Parar()

	j := janela.Nova(frota, c, versao)

	// Atualizacao do proprio binario. O primeiro passo e apagar a sobra da
	// troca anterior: e o unico momento em que ela com certeza nao esta mais
	// em uso.
	parar := make(chan struct{})
	defer close(parar)
	if !*semUp {
		horas := 6.0
		if v, ok := cfg.Bruto["horas_entre_updates"].(float64); ok {
			horas = v
		}
		at := atualizar.Novo(versao, time.Duration(horas*float64(time.Hour)))
		at.LimparAntigo()
		at.Iniciar(parar)
		j.Atual = at
	}

	var bnd bandeja.Bandeja
	if !*semTray && bandeja.Disponivel() {
		bnd, err = bandeja.Iniciar(bandeja.Acoes{
			// Tudo passa pela fila da janela: a bandeja roda noutra thread,
			// e a interface do Gio so pode ser tocada pela dela.
			Mostrar:   func() { j.Pedir("mostrar") },
			Atualizar: func() { j.Pedir("atualizar") },
			Topo:      func() { j.Pedir("topo") },
			Sair:      func() { j.Pedir("sair") },
			NoTopo:    j.NoTopo,
		})
		if err != nil {
			// Bandeja e um extra: sem ela a janela funciona igual, e morrer
			// aqui deixaria o usuario sem nada.
			fmt.Fprintf(os.Stderr, "bandeja indisponivel (%v); seguindo so "+
				"com a janela\n", err)
		} else {
			j.NaBandeja = true
			j.AoMudarNivel = bnd.Estado
			j.AoAlertar = bnd.Notificar
			defer bnd.Fechar()
			fmt.Println("bandeja ativa; fechar a janela nao encerra - use " +
				"Sair no icone")
		}
	}

	fmt.Printf("sysmon %s   %d host(s)\n", versao, len(cfg.Hosts))
	frota.EsperarPrimeiraLeitura(2500 * time.Millisecond)

	go func() {
		err := j.Rodar(*oculto)
		if bnd != nil {
			bnd.Fechar()
		}
		frota.Parar()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}
