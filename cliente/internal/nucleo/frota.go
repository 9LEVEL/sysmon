package nucleo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"sysmon-cliente/internal/metricas"
)

// Monitor consulta um agente em laco, numa goroutine propria.
//
// Faz recuo exponencial em caso de falha: um host desligado nao deve gerar
// tentativa a cada 5s indefinidamente, ainda mais quando ha varios. Sem isso,
// dez hosts fora do ar viram dez conexoes penduradas a cada ciclo.
type Monitor struct {
	Host      Host
	intervalo time.Duration
	timeout   time.Duration
	aoMudar   func(string, Estado)
	cliente   *http.Client

	mu     sync.RWMutex
	estado Estado

	acordar chan struct{}
	parar   chan struct{}
	umaVez  sync.Once
}

const recuoMax = 60 * time.Second

func NovoMonitor(h Host, intervalo, timeout time.Duration,
	aoMudar func(string, Estado)) *Monitor {
	return &Monitor{
		Host: h, intervalo: intervalo, timeout: timeout, aoMudar: aoMudar,
		// Um cliente por monitor, com pool proprio: reaproveitar conexao
		// derruba o custo de TLS e handshake em frota grande.
		cliente: &http.Client{Timeout: timeout},
		acordar: make(chan struct{}, 1),
		parar:   make(chan struct{}),
	}
}

func (m *Monitor) Estado() Estado {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.estado
}

func (m *Monitor) definir(novo Estado, lim Limiares) {
	m.mu.Lock()
	anterior := m.estado
	m.estado = novo
	m.mu.Unlock()
	// So avisa quando a severidade MUDA: notificar a cada coleta seria
	// ruido garantido, e ruido vira alerta ignorado.
	if m.aoMudar != nil && NivelDo(anterior, lim) != NivelDo(novo, lim) {
		m.aoMudar(m.Host.Nome, novo)
	}
}

// Buscar faz uma coleta agora. Nunca levanta: erro vira Estado.Erro.
func (m *Monitor) Buscar(ctx context.Context, lim Limiares) {
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.Host.URL, nil)
	if err != nil {
		m.falhou(fmt.Sprintf("url invalida: %v", err), lim)
		return
	}
	if m.Host.Token != "" {
		req.Header.Set("Authorization", "Bearer "+m.Host.Token)
	}
	req.Header.Set("User-Agent", "sysmon")

	resp, err := m.cliente.Do(req)
	if err != nil {
		m.falhou(resumirErroRede(err), lim)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized ||
		resp.StatusCode == http.StatusForbidden {
		// A causa mais comum e token errado, e a mensagem generica de HTTP
		// nao ajudaria ninguem a consertar.
		m.falhou("token recusado pelo agente", lim)
		return
	}
	if resp.StatusCode != http.StatusOK {
		m.falhou(fmt.Sprintf("HTTP %d", resp.StatusCode), lim)
		return
	}

	// Teto de leitura: um endpoint hostil nao pode consumir a memoria do
	// cliente respondendo para sempre.
	corpo, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		m.falhou(resumirErroRede(err), lim)
		return
	}
	var snap metricas.Snapshot
	if err := json.Unmarshal(corpo, &snap); err != nil {
		m.falhou("resposta nao e um snapshot valido", lim)
		return
	}
	m.definir(Estado{Dados: &snap, Atualizado: agora()}, lim)
}

func (m *Monitor) falhou(msg string, lim Limiares) {
	m.mu.RLock()
	falhas := m.estado.Falhas
	m.mu.RUnlock()
	m.definir(Estado{Erro: msg, Falhas: falhas + 1, Atualizado: agora()}, lim)
}

// espera calcula o proximo intervalo, com recuo exponencial apos falha.
func (m *Monitor) espera() time.Duration {
	f := m.Estado().Falhas
	if f == 0 {
		return m.intervalo
	}
	d := time.Duration(float64(m.intervalo) * math.Pow(2, float64(min(f, 6))))
	if d > recuoMax {
		return recuoMax
	}
	return d
}

func (m *Monitor) Iniciar(ctx context.Context, lim func() Limiares) {
	go func() {
		for {
			m.Buscar(ctx, lim())
			select {
			case <-ctx.Done():
				return
			case <-m.parar:
				return
			case <-m.acordar:
			case <-time.After(m.espera()):
			}
		}
	}()
}

// AtualizarAgora acorda o laco sem esperar o intervalo.
func (m *Monitor) AtualizarAgora() {
	select {
	case m.acordar <- struct{}{}:
	default: // ja ha um pedido na fila; empilhar coletas nao ajuda
	}
}

func (m *Monitor) Parar() { m.umaVez.Do(func() { close(m.parar) }) }

// Frota e o conjunto de hosts monitorados.
type Frota struct {
	mu        sync.RWMutex
	cfg       Config
	monitores []*Monitor
	aoMudar   func(string, Estado)
	ctx       context.Context
	rodando   bool
}

func NovaFrota(cfg Config, aoMudar func(string, Estado)) *Frota {
	f := &Frota{cfg: cfg, aoMudar: aoMudar, ctx: context.Background()}
	f.montar()
	return f
}

func (f *Frota) montar() {
	f.monitores = f.monitores[:0]
	for _, h := range f.cfg.Hosts {
		f.monitores = append(f.monitores, NovoMonitor(h,
			time.Duration(f.cfg.Intervalo*float64(time.Second)),
			time.Duration(f.cfg.Timeout*float64(time.Second)), f.aoMudar))
	}
}

func (f *Frota) Cfg() Config {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.cfg
}

func (f *Frota) limiares() Limiares { return f.Cfg().Limiares }

func (f *Frota) Iniciar() {
	f.mu.Lock()
	f.rodando = true
	monitores := append([]*Monitor(nil), f.monitores...)
	f.mu.Unlock()
	for _, m := range monitores {
		m.Iniciar(f.ctx, f.limiares)
	}
}

// Trocar substitui a configuracao sem reiniciar o programa.
//
// E o que permite salvar pela tela e ver o resultado na hora, em vez de
// pedir para fechar e abrir.
func (f *Frota) Trocar(cfg Config) {
	f.mu.Lock()
	antigos := f.monitores
	f.cfg = cfg
	f.monitores = nil
	f.montar()
	novos := append([]*Monitor(nil), f.monitores...)
	rodando := f.rodando
	f.mu.Unlock()

	if rodando {
		for _, m := range novos {
			m.Iniciar(f.ctx, f.limiares)
		}
	}
	for _, m := range antigos {
		m.Parar()
	}
}

func (f *Frota) Parar() {
	f.mu.RLock()
	monitores := append([]*Monitor(nil), f.monitores...)
	f.mu.RUnlock()
	for _, m := range monitores {
		m.Parar()
	}
}

func (f *Frota) AtualizarAgora() {
	f.mu.RLock()
	monitores := append([]*Monitor(nil), f.monitores...)
	f.mu.RUnlock()
	for _, m := range monitores {
		m.AtualizarAgora()
	}
}

// LeituraHost e um host com o estado dele, na ordem do config.
type LeituraHost struct {
	Host   Host
	Estado Estado
}

func (f *Frota) Estados() []LeituraHost {
	f.mu.RLock()
	monitores := append([]*Monitor(nil), f.monitores...)
	f.mu.RUnlock()
	out := make([]LeituraHost, 0, len(monitores))
	for _, m := range monitores {
		out = append(out, LeituraHost{Host: m.Host, Estado: m.Estado()})
	}
	return out
}

// EsperarPrimeiraLeitura da um tempo para a primeira rodada antes de a tela
// aparecer vazia - o que pareceria "nenhum host responde".
func (f *Frota) EsperarPrimeiraLeitura(limite time.Duration) {
	fim := time.Now().Add(limite)
	for time.Now().Before(fim) {
		pronto := true
		for _, l := range f.Estados() {
			if l.Estado.Atualizado == 0 {
				pronto = false
				break
			}
		}
		if pronto {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (f *Frota) PiorNivel() int {
	lim := f.limiares()
	pior := OK
	for _, l := range f.Estados() {
		if n := NivelDo(l.Estado, lim); n > pior {
			pior = n
		}
	}
	return pior
}

// Alertas devolve todos os alertas da frota, ja prefixados com o host.
func (f *Frota) Alertas() []string {
	lim := f.limiares()
	var out []string
	for _, l := range f.Estados() {
		_, alertas := Avaliar(l.Estado, lim)
		for _, a := range alertas {
			out = append(out, l.Host.Nome+": "+a)
		}
	}
	return out
}

// TestarHost faz uma consulta unica, para o botao Testar da tela de hosts.
func TestarHost(bruta, token string, timeout time.Duration) (bool, string) {
	cfg, err := ConfigDe(map[string]any{"hosts": []any{
		map[string]any{"url": bruta, "token": token}}})
	if err != nil {
		return false, err.Error()
	}
	m := NovoMonitor(cfg.Hosts[0], timeout, timeout, nil)
	m.Buscar(context.Background(), LimiaresPadrao())
	e := m.Estado()
	if e.Erro != "" {
		return false, e.Erro
	}
	if e.Dados == nil {
		return false, "sem dados"
	}
	nome := e.Dados.Host
	if nome == "" {
		nome = "sem nome"
	}
	return true, fmt.Sprintf("ok - %s, %d cpus", nome, e.Dados.CPUs)
}

// resumirErroRede troca a mensagem verbosa do net/http por algo acionavel.
//
// "Get \"http://...\": dial tcp ...: connect: connection refused" nao ajuda
// ninguem; "conexao recusada" cabe na tela e diz a mesma coisa.
func resumirErroRede(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "context deadline exceeded"), strings.Contains(s, "Client.Timeout"):
		return "sem resposta (timeout)"
	case strings.Contains(s, "connection refused"):
		return "conexao recusada"
	case strings.Contains(s, "no such host"), strings.Contains(s, "server misbehaving"):
		return "nome nao resolve"
	case strings.Contains(s, "network is unreachable"), strings.Contains(s, "no route to host"):
		return "host inalcancavel"
	case strings.Contains(s, "certificate"):
		return "certificado TLS recusado"
	}
	return s
}

func agora() float64 { return float64(time.Now().UnixNano()) / 1e9 }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
