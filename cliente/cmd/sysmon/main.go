// Comando sysmon - o cliente.
//
// Um binario para a maquina de onde voce olha. Consulta os agentes por HTTP,
// avalia e mostra numa janela nativa. Sem servidor, sem banco, sem runtime
// instalado: e o que separa esta ferramenta de um Zabbix e de um script.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"gioui.org/app"

	"sysmon-cliente/internal/janela"
	"sysmon-cliente/internal/nucleo"
)

// Versao e sobrescrita no build: -ldflags "-X main.versao=..."
var versao = "dev"

func main() {
	var (
		caminho = flag.String("config", "", "caminho do config.json")
		oculto  = flag.Bool("oculto", false, "abrir minimizado na bandeja")
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

	fmt.Printf("sysmon %s   %d host(s)\n", versao, len(cfg.Hosts))
	frota.EsperarPrimeiraLeitura(2500 * time.Millisecond)

	j := janela.Nova(frota, c, versao)
	go func() {
		if err := j.Rodar(*oculto); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}
