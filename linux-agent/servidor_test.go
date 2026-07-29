package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const tokenTeste = "0123456789abcdef0123456789abcdef"

func servidorTeste(t *testing.T) (*Servidor, *Coletor) {
	t.Helper()
	c := NovoColetor(fake(t, map[string]string{
		"/proc/stat":    "cpu  0 0 0 1000 0 0 0 0 0 0\n",
		"/proc/meminfo": "MemTotal: 1000 kB\nMemAvailable: 500 kB\n",
	}), 5*time.Second)
	c.atual = c.coletar()
	return NovoServidor(c, tokenTeste), c
}

func pedir(t *testing.T, s *Servidor, metodo, alvo string, cab map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(metodo, alvo, nil)
	for k, v := range cab {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.Mux().ServeHTTP(w, r)
	return w
}

func TestMetricsExigeToken(t *testing.T) {
	s, _ := servidorTeste(t)

	casos := []struct {
		nome   string
		alvo   string
		cab    map[string]string
		codigo int
	}{
		{"sem token", "/metrics", nil, http.StatusUnauthorized},
		{"bearer errado", "/metrics", map[string]string{"Authorization": "Bearer errado"}, http.StatusUnauthorized},
		{"query errada", "/metrics?token=errado", nil, http.StatusUnauthorized},
		{"prefixo do token certo", "/metrics?token=" + tokenTeste[:8], nil, http.StatusUnauthorized},
		{"bearer certo", "/metrics", map[string]string{"Authorization": "Bearer " + tokenTeste}, http.StatusOK},
		{"query certa", "/metrics?token=" + tokenTeste, nil, http.StatusOK},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			w := pedir(t, s, http.MethodGet, c.alvo, c.cab)
			if w.Code != c.codigo {
				t.Fatalf("esperava %d, veio %d (%s)", c.codigo, w.Code, w.Body.String())
			}
		})
	}
}

func TestMetricsDevolveJSONValido(t *testing.T) {
	s, _ := servidorTeste(t)
	w := pedir(t, s, http.MethodGet, "/metrics",
		map[string]string{"Authorization": "Bearer " + tokenTeste})

	if ct := w.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("content-type: %q", ct)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("metrica nao deve ser cacheada")
	}

	var s2 Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &s2); err != nil {
		t.Fatalf("resposta nao e um Snapshot valido: %v", err)
	}
	if s2.V != versao {
		t.Errorf("versao: veio %q", s2.V)
	}
	if s2.IntervaloS != 5 {
		t.Errorf("intervalo_s deveria vir preenchido, veio %v", s2.IntervaloS)
	}
	// Os campos que o tray consome precisam existir mesmo vazios, para o
	// cliente nao ter que checar null em tudo.
	var cru map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &cru); err != nil {
		t.Fatal(err)
	}
	for _, campo := range []string{"host", "cpu_temp", "cpu_crit", "mem", "discos",
		"temps", "fans", "load", "uptime_s", "idade_s", "net", "diskio"} {
		if _, tem := cru[campo]; !tem {
			t.Errorf("campo %q sumiu do contrato com o cliente", campo)
		}
	}
}

func TestHealthNaoExigeToken(t *testing.T) {
	s, _ := servidorTeste(t)
	w := pedir(t, s, http.MethodGet, "/health", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("esperava 200, veio %d", w.Code)
	}

	var corpo map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &corpo); err != nil {
		t.Fatal(err)
	}
	if corpo["ok"] != true {
		t.Errorf("ok deveria ser true: %v", corpo)
	}
	// O health nao pode vazar metrica para quem nao tem token.
	if _, tem := corpo["cpu_temp"]; tem {
		t.Error("health nao deve expor telemetria")
	}
}

func TestHealthReprovaColetorTravado(t *testing.T) {
	s, c := servidorTeste(t)
	velho := c.atual
	velho.TS = float64(time.Now().Add(-10*time.Minute).UnixNano()) / 1e9
	c.atual = velho

	w := pedir(t, s, http.MethodGet, "/health", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("coletor travado deveria dar 503, veio %d", w.Code)
	}
}

func TestRotaInexistente(t *testing.T) {
	s, _ := servidorTeste(t)
	for _, alvo := range []string{"/", "/exec", "/reboot", "/vm/100/start"} {
		w := pedir(t, s, http.MethodGet, alvo, nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: esperava 404, veio %d", alvo, w.Code)
		}
	}
}

func TestTravessiaDeCaminhoNaoServeNada(t *testing.T) {
	// O ServeMux normaliza o caminho e redireciona; o destino cai no 404.
	// O que importa e que nenhuma tentativa devolva 200 nem conteudo.
	s, _ := servidorTeste(t)
	for _, alvo := range []string{
		"/metrics/../etc/passwd",
		"/../../../etc/shadow",
		"/metrics/./../../root/.ssh/id_rsa",
	} {
		w := pedir(t, s, http.MethodGet, alvo, nil)
		if w.Code == http.StatusOK {
			t.Errorf("%s devolveu 200: %s", alvo, w.Body.String())
		}
		if destino := w.Header().Get("Location"); destino != "" {
			seguinte := pedir(t, s, http.MethodGet, destino, nil)
			if seguinte.Code != http.StatusNotFound {
				t.Errorf("%s -> %s deu %d", alvo, destino, seguinte.Code)
			}
		}
	}
}

func TestSoAceitaLeitura(t *testing.T) {
	s, _ := servidorTeste(t)
	cab := map[string]string{"Authorization": "Bearer " + tokenTeste}
	for _, metodo := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		w := pedir(t, s, metodo, "/metrics", cab)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s deveria ser recusado, veio %d", metodo, w.Code)
		}
	}
}

func TestLimitarConexoesRecusaExcedente(t *testing.T) {
	base, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := LimitarConexoes(base, 2)
	defer ln.Close()

	aceitas := make(chan net.Conn, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			aceitas <- c
		}
	}()

	// Tres clientes, teto de dois: o terceiro e derrubado sem virar goroutine.
	var clientes []net.Conn
	for i := 0; i < 3; i++ {
		c, err := net.Dial("tcp", base.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		clientes = append(clientes, c)
	}
	defer func() {
		for _, c := range clientes {
			c.Close()
		}
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-aceitas:
		case <-time.After(2 * time.Second):
			t.Fatalf("conexao %d deveria ter sido aceita", i+1)
		}
	}
	select {
	case c := <-aceitas:
		c.Close()
		t.Fatal("a terceira conexao passou do limite")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestLimitarConexoesLiberaVaga(t *testing.T) {
	base, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := LimitarConexoes(base, 1)
	defer ln.Close()

	c1, err := net.Dial("tcp", base.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Close()

	servidor, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	// Fechar duas vezes nao pode liberar a vaga duas vezes (o sync.Once cuida).
	servidor.Close()
	servidor.Close()

	c2, err := net.Dial("tcp", base.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()

	pronto := make(chan struct{})
	go func() {
		c, err := ln.Accept()
		if err == nil {
			c.Close()
		}
		close(pronto)
	}()
	select {
	case <-pronto:
	case <-time.After(2 * time.Second):
		t.Fatal("a vaga nao foi liberada apos o Close")
	}
}

func TestValidarToken(t *testing.T) {
	if _, err := validarToken("curto"); err == nil {
		t.Error("token curto deveria ser recusado")
	}
	if _, err := validarToken(tokenTeste); err != nil {
		t.Errorf("token valido recusado: %v", err)
	}
}
