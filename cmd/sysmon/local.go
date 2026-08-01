package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"sysmon/internal/coleta"
	"sysmon/internal/historico"
	"sysmon/internal/nucleo"
	"sysmon/internal/terminal"
)

// rodarLocal e o subcomando `local`: os sensores DESTA maquina, sem rede.
//
// Ate a v4 este modo era um leitor de /proc separado, escrito de novo no
// cliente - uma segunda copia da coleta, que divergia da do agente a cada
// campo novo. Com o modulo unico ele passou a usar exatamente o mesmo
// coletor que o agente usa, entao os dois nunca mais podem discordar sobre
// o que a maquina esta reportando.
//
// Serve para conferir a ferramenta antes de instalar agente nenhum, e para
// um `sysmon local --once` num cron da propria maquina.
func rodarLocal(args []string) int {
	fs := flag.NewFlagSet("local", flag.ExitOnError)
	var (
		umaVez    = fs.Bool("once", false, "imprime uma vez e sai")
		intervalo = fs.Float64("intervalo", 3, "segundos entre atualizacoes")
		semCor    = fs.Bool("sem-cor", false, "nao usar cor")
		raiz      = fs.String("raiz", "", "prefixo para /proc e /sys (teste)")
	)
	fs.Parse(args)

	terminal.PrenderConsole()

	if runtime.GOOS != "linux" {
		// Dizer o que fazer, e nao so que nao da. Quem esta no Windows quer
		// monitorar Linux de qualquer forma - o caminho existe.
		fmt.Fprintf(os.Stderr,
			"o modo local le /proc e /sys, que so existem no Linux.\n"+
				"Neste sistema, use a janela ou `sysmon term` apontando para\n"+
				"os agentes instalados nos hosts.\n")
		return 2
	}

	fontes := coleta.NovasFontes(*raiz)
	c := coleta.NovoColetor(fontes, time.Duration(*intervalo*float64(time.Second)),
		versao).ComHistorico(historico.Abrir(historico.CaminhoPadrao()))

	// A primeira coleta nao tem com o que comparar, entao taxas (cpu, rede,
	// disco) saem vazias. Duas coletas seguidas resolvem, e o custo e um
	// intervalo de espera - barato perto de mostrar "—" onde deveria haver
	// numero.
	c.ColetarAgora()
	if !*umaVez {
		time.Sleep(time.Duration(*intervalo * float64(time.Second)))
	} else {
		time.Sleep(300 * time.Millisecond)
	}

	lim := nucleo.LimiaresPadrao()
	o := terminal.PadraoOpcoes()
	if *semCor {
		o.Cor = false
	}

	desenhar := func() int {
		s := c.ColetarAgora()
		e := nucleo.Estado{Dados: &s}
		leituras := []nucleo.LeituraHost{{
			Host: nucleo.Host{Nome: s.Host}, Estado: e}}
		nivel, alertas := nucleo.Avaliar(e, lim)
		terminal.Desenhar(os.Stdout, leituras, lim, alertas, o)
		switch nivel {
		case nucleo.Offline:
			return 2
		case nucleo.Aviso, nucleo.Critico:
			return 1
		}
		return 0
	}

	if *umaVez {
		return desenhar()
	}

	parar := make(chan os.Signal, 1)
	signal.Notify(parar, os.Interrupt, syscall.SIGTERM)
	tique := time.NewTicker(time.Duration(*intervalo * float64(time.Second)))
	defer tique.Stop()
	for {
		fmt.Print("\x1b[H\x1b[2J")
		desenhar()
		select {
		case <-tique.C:
		case <-parar:
			fmt.Println()
			return 0
		}
	}
}
