package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"sysmon/internal/nucleo"
	"sysmon/internal/terminal"
)

// rodarTerminal e o subcomando `term`.
//
// Dois usos, e a diferenca entre eles guia tudo aqui: olhar rapido por SSH,
// que quer cor e atualizacao continua; e `--once` dentro de cron ou script,
// que quer sair com codigo util e saida limpa para canalizar.
func rodarTerminal(args []string) int {
	fs := flag.NewFlagSet("term", flag.ExitOnError)
	var (
		caminho   = fs.String("config", "", "caminho do config.json")
		umaVez    = fs.Bool("once", false, "imprime uma vez e sai (cron, script)")
		comoJSON  = fs.Bool("json", false, "imprime o estado bruto em JSON")
		intervalo = fs.Float64("intervalo", 0, "segundos entre atualizacoes")
		semCor    = fs.Bool("sem-cor", false, "nao usar cor")
		host      = fs.String("host", "", "so este host")
	)
	fs.Parse(args)

	// No Windows o binario e GUI e nasce sem console; sem isto a tabela
	// sairia para lugar nenhum.
	terminal.PrenderConsole()

	c := nucleo.AcharConfig(*caminho)
	cfg, err := nucleo.CarregarConfig(c)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		return 2
	}
	if aviso := nucleo.AvisarPermissao(c); aviso != "" {
		fmt.Fprintln(os.Stderr, "aviso:", aviso)
	}
	if *host != "" {
		cfg = soUmHost(cfg, *host)
		if len(cfg.Hosts) == 0 {
			fmt.Fprintf(os.Stderr, "erro: host %q nao esta no config\n", *host)
			return 2
		}
	}
	if *intervalo > 0 {
		cfg.Intervalo = *intervalo
	}

	frota := nucleo.NovaFrota(cfg, nil)
	frota.Iniciar()
	defer frota.Parar()
	frota.EsperarPrimeiraLeitura(5 * time.Second)

	o := terminal.PadraoOpcoes()
	if *semCor {
		o.Cor = false
	}

	if *comoJSON {
		return imprimirJSON(frota)
	}
	if *umaVez {
		terminal.Desenhar(os.Stdout, frota.Estados(), cfg.Limiares,
			frota.Alertas(), o)
		// Codigo de saida util para script: 0 tudo bem, 1 ha alerta, 2 ha
		// host fora do ar. E o que permite `sysmon term --once || avisar`.
		return codigoDeSaida(frota)
	}

	// Modo continuo: limpa a tela a cada rodada.
	parar := make(chan os.Signal, 1)
	signal.Notify(parar, os.Interrupt, syscall.SIGTERM)
	tique := time.NewTicker(time.Duration(cfg.Intervalo * float64(time.Second)))
	defer tique.Stop()
	for {
		fmt.Print("\x1b[H\x1b[2J")
		terminal.Desenhar(os.Stdout, frota.Estados(), cfg.Limiares,
			frota.Alertas(), o)
		select {
		case <-tique.C:
		case <-parar:
			fmt.Println()
			return 0
		}
	}
}

func soUmHost(cfg nucleo.Config, nome string) nucleo.Config {
	var restantes []nucleo.Host
	for _, h := range cfg.Hosts {
		if h.Nome == nome {
			restantes = append(restantes, h)
		}
	}
	cfg.Hosts = restantes
	return cfg
}

// codigoDeSaida traduz o estado da frota para o shell.
func codigoDeSaida(f *nucleo.Frota) int {
	switch f.PiorNivel() {
	case nucleo.Offline:
		return 2
	case nucleo.Aviso, nucleo.Critico:
		return 1
	}
	return 0
}

func imprimirJSON(f *nucleo.Frota) int {
	saida := map[string]any{}
	for _, l := range f.Estados() {
		item := map[string]any{"erro": l.Estado.Erro}
		if l.Estado.Dados != nil {
			item["dados"] = l.Estado.Dados
		}
		saida[l.Host.Nome] = item
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(saida); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		return 2
	}
	return codigoDeSaida(f)
}
