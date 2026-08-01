// sysmon-agent - agente de telemetria read-only para hosts Linux.
//
//	GET /metrics   -> JSON: temperaturas, load, RAM, discos, rede, IO, PSI...
//	GET /health    -> saude do coletor (sem autenticacao, para healthcheck)
//
// Autenticacao: header  Authorization: Bearer <token>  ou  ?token=<token>
//
// Nao executa comandos, nao aceita escrita, nao tem dependencia externa.
// Compila para um binario estatico unico: instalar num host novo e copiar um
// arquivo, sem depender do Python de sistema de cada distribuicao.
//
//	export SYSMON_TOKEN="$(openssl rand -hex 24)"
//	./sysmon-agent --host 10.0.0.5 --port 9109
package main

import (
	"sysmon/internal/coleta"
	"sysmon/internal/historico"

	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// versao e sobrescrita no build: -ldflags "-X main.versao=..."
var versao = "5.6.0"

func main() {
	host := flag.String("host", "127.0.0.1",
		"IP de bind. Use o IP da LAN ou do tunel, nunca 0.0.0.0 exposto")
	porta := flag.Int("port", 9109, "porta TCP")
	intervalo := flag.Duration("intervalo", 5*time.Second,
		"tempo entre coletas (minimo 1s)")
	mounts := flag.String("mounts", "",
		"pontos de montagem separados por virgula (vazio = descobrir do /proc/mounts)")
	netIgnorar := flag.String("net-ignorar", strings.Join(coleta.NetIgnorarPadrao, ","),
		"prefixos de interface de rede a ignorar")
	mostrarVersao := flag.Bool("version", false, "imprime a versao e sai")
	flag.Parse()

	if *mostrarVersao {
		fmt.Println(versao)
		return
	}

	log.SetFlags(0) // o journald ja carimba data/hora

	token, err := lerToken()
	if err != nil {
		log.Fatalf("ERRO: %v", err)
	}
	if *intervalo < time.Second {
		log.Fatal("ERRO: --intervalo abaixo de 1s so gera IO no host sem trazer informacao nova.")
	}
	if *host == "0.0.0.0" || *host == "::" {
		log.Println("AVISO: bind em todas as interfaces. O transporte e HTTP puro " +
			"e o token viaja em texto claro - garanta que ha firewall na frente.")
	}

	fontes := coleta.Fontes{
		NetIgnorar:  listar(*netIgnorar),
		MountsFixos: listar(*mounts),
	}
	// O historico e o que separa "200 setores realocados" de "200 setores
	// realocados que eram 190 semana passada". Sem ele o smartctl so responde
	// sobre o presente, e metade das regras de saude de disco fica cega.
	hist := historico.Abrir(historico.CaminhoPadrao())
	if err := hist.Erro(); err != nil {
		log.Printf("AVISO: historico SMART ilegivel (%v); recomecando do zero", err)
	}
	coletor := coleta.NovoColetor(fontes, *intervalo, versao).ComHistorico(hist)

	// Contexto cancelado no primeiro SIGTERM (o que o systemd manda ao parar).
	ctx, parar := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer parar()

	wd := novoWatchdog()
	coletor.Iniciar(ctx.Done(), wd.pulso)

	srv := NovoServidor(coletor, token).HTTP(net.JoinHostPort(*host, fmt.Sprint(*porta)))

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		log.Fatalf("ERRO: nao consegui escutar em %s: %v", srv.Addr, err)
	}
	ln = LimitarConexoes(ln, maxConexoes)

	log.Printf("sysmon-agent %s em http://%s/metrics (coleta a cada %s)",
		versao, srv.Addr, *intervalo)
	notificarSystemd("READY=1")

	erros := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			erros <- err
		}
	}()

	select {
	case err := <-erros:
		log.Fatalf("ERRO: servidor caiu: %v", err)
	case <-ctx.Done():
		log.Println("encerrando")
	}

	notificarSystemd("STOPPING=1")
	desligar, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelar()
	if err := srv.Shutdown(desligar); err != nil {
		log.Printf("shutdown forcado: %v", err)
	}
}

// lerToken aceita o token pelo ambiente ou por arquivo. O arquivo permite usar
// LoadCredential= do systemd, que nunca expoe o segredo no ambiente do processo.
func lerToken() (string, error) {
	if caminho := os.Getenv("SYSMON_TOKEN_FILE"); caminho != "" {
		b, err := os.ReadFile(caminho)
		if err != nil {
			return "", fmt.Errorf("SYSMON_TOKEN_FILE: %w", err)
		}
		return validarToken(strings.TrimSpace(string(b)))
	}
	return validarToken(strings.TrimSpace(os.Getenv("SYSMON_TOKEN")))
}

func validarToken(t string) (string, error) {
	if len(t) < 16 {
		return "", errors.New("defina SYSMON_TOKEN (ou SYSMON_TOKEN_FILE) " +
			"com pelo menos 16 caracteres; gere com: openssl rand -hex 24")
	}
	return t, nil
}

func listar(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ------------------------------------------------------------- systemd

// watchdog conversa com o systemd pelo protocolo sd_notify. Se o coletor parar
// de produzir amostras, o pulso cessa e o systemd reinicia o servico sozinho -
// e a diferenca entre "o agente morreu" (o systemd ja resolvia) e "o agente
// esta vivo servindo dado congelado" (que antes ninguem detectava).
type watchdog struct{ intervalo time.Duration }

func novoWatchdog() *watchdog {
	usec := os.Getenv("WATCHDOG_USEC")
	if usec == "" {
		return &watchdog{}
	}
	// WATCHDOG_PID protege contra herdar a variavel num processo filho.
	if pid := os.Getenv("WATCHDOG_PID"); pid != "" && pid != fmt.Sprint(os.Getpid()) {
		return &watchdog{}
	}
	var us int64
	if _, err := fmt.Sscan(usec, &us); err != nil || us <= 0 {
		return &watchdog{}
	}
	return &watchdog{intervalo: time.Duration(us) * time.Microsecond / 2}
}

func (w *watchdog) pulso() {
	if w.intervalo > 0 {
		notificarSystemd("WATCHDOG=1")
	}
}

func notificarSystemd(estado string) {
	caminho := os.Getenv("NOTIFY_SOCKET")
	if caminho == "" {
		return
	}
	if strings.HasPrefix(caminho, "@") { // socket abstrato
		caminho = "\x00" + caminho[1:]
	}
	c, err := net.DialUnix("unixgram", nil,
		&net.UnixAddr{Name: caminho, Net: "unixgram"})
	if err != nil {
		return
	}
	defer c.Close()
	_, _ = c.Write([]byte(estado))
}
