// Servidor HTTP read-only. Nao existe rota de escrita, e nenhum dado vindo do
// cliente chega perto do sistema de arquivos: o unico parametro aceito e o
// token, e ele so e comparado.
package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	maxConexoes    = 32
	timeoutLeitura = 10 * time.Second
	timeoutEscrita = 15 * time.Second
	timeoutOcioso  = 30 * time.Second
	maxCabecalho   = 8 << 10
	atrasoAuthRuim = 500 * time.Millisecond
)

type Servidor struct {
	coletor *Coletor
	token   [32]byte
}

func NovoServidor(c *Coletor, token string) *Servidor {
	return &Servidor{coletor: c, token: sha256.Sum256([]byte(token))}
}

// autorizado compara os digests, nao as strings: assim o tempo de comparacao
// nao depende nem do conteudo nem do comprimento do token enviado.
func (s *Servidor) autorizado(r *http.Request) bool {
	var enviado string
	if cab := r.Header.Get("Authorization"); strings.HasPrefix(cab, "Bearer ") {
		enviado = strings.TrimPrefix(cab, "Bearer ")
	} else {
		enviado = r.URL.Query().Get("token")
	}
	d := sha256.Sum256([]byte(enviado))
	return subtle.ConstantTimeCompare(d[:], s.token[:]) == 1
}

func (s *Servidor) escreverJSON(w http.ResponseWriter, codigo int, v any) {
	corpo, err := json.Marshal(v)
	if err != nil {
		log.Printf("falha ao serializar resposta: %v", err)
		http.Error(w, `{"erro":"serializacao"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(codigo)
	_, _ = w.Write(corpo)
}

// health nao exige token: e o que permite um healthcheck externo saber que o
// agente esta de pe sem carregar credencial. Nao devolve nenhuma metrica, so a
// saude do proprio coletor.
func (s *Servidor) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		s.escreverJSON(w, http.StatusMethodNotAllowed, map[string]any{"erro": "so GET"})
		return
	}
	vivo, snap := s.coletor.Saudavel()
	codigo := http.StatusOK
	if !vivo {
		codigo = http.StatusServiceUnavailable
	}
	s.escreverJSON(w, codigo, map[string]any{
		"ok":      vivo,
		"v":       versao,
		"idade_s": snap.IdadeS,
		"falhas":  snap.ColetorFalhas,
	})
}

func (s *Servidor) metrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		s.escreverJSON(w, http.StatusMethodNotAllowed, map[string]any{"erro": "so GET"})
		return
	}
	if !s.autorizado(r) {
		// Atraso fixo em toda falha de auth: torna forca bruta pela rede
		// impraticavel sem precisar guardar estado por IP.
		time.Sleep(atrasoAuthRuim)
		log.Printf("401 de %s", origem(r))
		s.escreverJSON(w, http.StatusUnauthorized, map[string]any{"erro": "token invalido"})
		return
	}
	s.escreverJSON(w, http.StatusOK, s.coletor.Snapshot())
}

func (s *Servidor) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/metrics", s.metrics)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.escreverJSON(w, http.StatusNotFound, map[string]any{"erro": "rota inexistente"})
	})
	return mux
}

func (s *Servidor) HTTP(endereco string) *http.Server {
	return &http.Server{
		Addr:    endereco,
		Handler: s.Mux(),
		// Sem estes timeouts, uma conexao que abre e nunca fala segura recursos
		// indefinidamente. Este agente roda no mesmo kernel das suas VMs.
		ReadHeaderTimeout: timeoutLeitura,
		ReadTimeout:       timeoutLeitura,
		WriteTimeout:      timeoutEscrita,
		IdleTimeout:       timeoutOcioso,
		MaxHeaderBytes:    maxCabecalho,
		ErrorLog:          log.Default(),
	}
}

func origem(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ------------------------------------------------------------- limitador

// limitListener recusa conexoes acima do teto em vez de enfileirar. Preferimos
// derrubar o excesso a deixar a fila crescer consumindo memoria do host.
type limitListener struct {
	net.Listener
	vagas chan struct{}
}

func LimitarConexoes(l net.Listener, n int) net.Listener {
	return &limitListener{Listener: l, vagas: make(chan struct{}, n)}
}

func (l *limitListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		select {
		case l.vagas <- struct{}{}:
			return &limitConn{Conn: c, vagas: l.vagas}, nil
		default:
			log.Printf("recusando %s: limite de %d conexoes", c.RemoteAddr(), cap(l.vagas))
			_ = c.Close()
		}
	}
}

type limitConn struct {
	net.Conn
	vagas chan struct{}
	uma   sync.Once
}

func (c *limitConn) Close() error {
	err := c.Conn.Close()
	c.uma.Do(func() { <-c.vagas })
	return err
}
