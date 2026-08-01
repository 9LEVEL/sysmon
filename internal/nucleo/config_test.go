package nucleo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"sysmon/internal/metricas"
)

func TestURLSemCaminhoGanhaMetrics(t *testing.T) {
	// O erro mais comum de quem cola o endereco na mao e esquecer /metrics.
	cfg, err := ConfigDe(map[string]any{"hosts": []any{
		map[string]any{"url": "http://10.0.0.9:9109", "token": "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Hosts[0].URL != "http://10.0.0.9:9109/metrics" {
		t.Fatalf("url = %q", cfg.Hosts[0].URL)
	}
}

func TestNomeDerivadoDaURL(t *testing.T) {
	cfg, _ := ConfigDe(map[string]any{"hosts": []any{
		map[string]any{"url": "http://pve.local:9109/metrics"},
		map[string]any{"url": "http://192.168.0.30:9109/metrics"},
	}})
	if cfg.Hosts[0].Nome != "pve" {
		t.Errorf("nome = %q, queria pve", cfg.Hosts[0].Nome)
	}
	// IP inteiro como nome poluiria a tela; o ultimo octeto distingue.
	if cfg.Hosts[1].Nome != "host-30" {
		t.Errorf("nome = %q, queria host-30", cfg.Hosts[1].Nome)
	}
}

func TestNomeRepetidoNaoSomeDaTela(t *testing.T) {
	// O nome e a chave em toda a interface: repetido, um host desapareceria.
	cfg, _ := ConfigDe(map[string]any{"hosts": []any{
		map[string]any{"nome": "pve", "url": "http://a:9109/metrics"},
		map[string]any{"nome": "pve", "url": "http://b:9109/metrics"},
	}})
	if cfg.Hosts[0].Nome == cfg.Hosts[1].Nome {
		t.Fatalf("nomes colidiram: %q", cfg.Hosts[0].Nome)
	}
}

func TestURLInvalidaERecusada(t *testing.T) {
	for _, u := range []string{"", "nao-e-url", "ftp://x/y"} {
		_, err := ConfigDe(map[string]any{"hosts": []any{
			map[string]any{"url": u}}})
		if err == nil {
			t.Errorf("url %q foi aceita", u)
		}
	}
}

func TestSalvarPreservaChavesDesconhecidas(t *testing.T) {
	// Abrir a tela de hosts numa versao antiga nao pode apagar configuracao
	// escrita por uma mais nova.
	dir := t.TempDir()
	caminho := filepath.Join(dir, "config.json")
	orig := map[string]any{
		"hosts":               []any{map[string]any{"url": "http://a:9109/metrics"}},
		"opcao_do_futuro":     "preservar",
		"horas_entre_updates": float64(12),
	}
	if err := SalvarConfig(caminho, orig); err != nil {
		t.Fatal(err)
	}
	cfg, err := CarregarConfig(caminho)
	if err != nil {
		t.Fatal(err)
	}
	volta := cfg.ComoBruto()
	if volta["opcao_do_futuro"] != "preservar" {
		t.Fatalf("chave desconhecida foi perdida: %v", volta)
	}
	if volta["horas_entre_updates"] != float64(12) {
		t.Fatalf("chave desconhecida foi perdida: %v", volta)
	}
}

func TestSalvarProtegeOArquivoComTokens(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("modelo de permissao diferente")
	}
	caminho := filepath.Join(t.TempDir(), "config.json")
	if err := SalvarConfig(caminho, map[string]any{"hosts": []any{}}); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(caminho)
	if st.Mode().Perm()&0o077 != 0 {
		t.Fatalf("config com tokens saiu legivel por outros: %v", st.Mode().Perm())
	}
	if aviso := AvisarPermissao(caminho); aviso != "" {
		t.Fatalf("avisou sobre arquivo ja protegido: %s", aviso)
	}
}

func TestConfigInvalidoTemMensagemParaHumano(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(caminho, []byte("{isso nao e json"), 0o600)
	_, err := CarregarConfig(caminho)
	if err == nil {
		t.Fatal("json quebrado foi aceito")
	}
	if !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("mensagem nao ajuda o usuario: %v", err)
	}
}

// -------------------------------------------------------------- frota

func agenteFalso(t *testing.T, snap metricas.Snapshot, token string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" && r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(snap)
	}))
}

func TestColetaLeOSnapshot(t *testing.T) {
	srv := agenteFalso(t, metricas.Snapshot{Host: "pve", CPUs: 8, IntervaloS: 5}, "segredo")
	defer srv.Close()

	m := NovoMonitor(Host{Nome: "pve", URL: srv.URL, Token: "segredo"},
		time.Second, 2*time.Second, nil)
	m.Buscar(t.Context(), LimiaresPadrao())
	e := m.Estado()
	if e.Erro != "" {
		t.Fatalf("erro: %s", e.Erro)
	}
	if e.Dados.Host != "pve" || e.Dados.CPUs != 8 {
		t.Fatalf("snapshot errado: %+v", e.Dados)
	}
}

func TestTokenErradoDizOQueFazer(t *testing.T) {
	// "HTTP 401" nao ajuda ninguem a consertar; o nome do problema ajuda.
	srv := agenteFalso(t, metricas.Snapshot{}, "certo")
	defer srv.Close()

	m := NovoMonitor(Host{URL: srv.URL, Token: "errado"}, time.Second,
		2*time.Second, nil)
	m.Buscar(t.Context(), LimiaresPadrao())
	if !strings.Contains(m.Estado().Erro, "token") {
		t.Fatalf("erro = %q", m.Estado().Erro)
	}
}

func TestHostForaDoArNaoDerrubaNada(t *testing.T) {
	m := NovoMonitor(Host{URL: "http://127.0.0.1:1/metrics"}, time.Second,
		time.Second, nil)
	m.Buscar(t.Context(), LimiaresPadrao())
	e := m.Estado()
	if e.Erro == "" {
		t.Fatal("host inalcancavel nao virou erro")
	}
	if e.Falhas != 1 {
		t.Fatalf("falhas = %d", e.Falhas)
	}
	if n, _ := Avaliar(e, LimiaresPadrao()); n != Offline {
		t.Fatalf("nivel = %d, queria Offline", n)
	}
}

func TestRecuoExponencial(t *testing.T) {
	// Dez hosts fora do ar nao podem gerar dez conexoes a cada ciclo.
	m := NovoMonitor(Host{URL: "http://127.0.0.1:1/metrics"}, time.Second,
		time.Millisecond, nil)
	primeira := m.espera()
	for i := 0; i < 3; i++ {
		m.Buscar(t.Context(), LimiaresPadrao())
	}
	if m.espera() <= primeira {
		t.Fatalf("sem recuo: %v -> %v", primeira, m.espera())
	}
	for i := 0; i < 20; i++ {
		m.Buscar(t.Context(), LimiaresPadrao())
	}
	if m.espera() > recuoMax {
		t.Fatalf("recuo passou do teto: %v", m.espera())
	}
}

func TestAvisaSoQuandoASeveridadeMuda(t *testing.T) {
	// Notificar a cada coleta seria ruido garantido, e ruido vira alerta
	// ignorado em duas semanas. Notifica na TRANSICAO, e so nela.
	srv := agenteFalso(t, metricas.Snapshot{Host: "pve", IntervaloS: 5}, "")
	var avisos int
	m := NovoMonitor(Host{URL: srv.URL + "/metrics"}, time.Second,
		500*time.Millisecond, func(string, Estado) { avisos++ })

	m.Buscar(t.Context(), LimiaresPadrao()) // desconhecido -> OK
	if avisos != 1 {
		t.Fatalf("subir para OK nao avisou: %d", avisos)
	}
	m.Buscar(t.Context(), LimiaresPadrao()) // OK -> OK, calado
	if avisos != 1 {
		t.Fatalf("coleta sem mudanca avisou: %d", avisos)
	}

	srv.Close()
	for i := 0; i < 3; i++ { // OK -> Offline, e depois calado
		m.Buscar(t.Context(), LimiaresPadrao())
	}
	if avisos != 2 {
		t.Fatalf("avisos = %d, queria 2 (uma subida, uma queda)", avisos)
	}
}

func TestPrimeiraFalhaNoArranqueNaoAvisa(t *testing.T) {
	// Subir com dez hosts fora do ar nao pode gerar dez notificacoes: o
	// estado inicial ja e "nao sei nada", que e o mesmo Offline.
	var avisos int
	m := NovoMonitor(Host{URL: "http://127.0.0.1:1/metrics"}, time.Second,
		200*time.Millisecond, func(string, Estado) { avisos++ })
	m.Buscar(t.Context(), LimiaresPadrao())
	if avisos != 0 {
		t.Fatalf("avisos = %d, queria 0", avisos)
	}
}

func TestFrotaTrocaConfigSemReiniciar(t *testing.T) {
	srv := agenteFalso(t, metricas.Snapshot{Host: "novo", IntervaloS: 5}, "")
	defer srv.Close()

	cfg, _ := ConfigDe(map[string]any{"hosts": []any{}})
	f := NovaFrota(cfg, nil)
	if len(f.Estados()) != 0 {
		t.Fatal("frota vazia nao esta vazia")
	}
	nova, _ := ConfigDe(map[string]any{"hosts": []any{
		map[string]any{"nome": "novo", "url": srv.URL + "/metrics"}}})
	f.Trocar(nova)
	if len(f.Estados()) != 1 {
		t.Fatalf("apos trocar, %d hosts", len(f.Estados()))
	}
	if f.Cfg().Hosts[0].Nome != "novo" {
		t.Fatal("config nao trocou")
	}
	f.Parar()
}

func TestTestarHostDaRespostaUtil(t *testing.T) {
	srv := agenteFalso(t, metricas.Snapshot{Host: "pve", CPUs: 4}, "")
	defer srv.Close()

	ok, msg := TestarHost(srv.URL, "", 2*time.Second)
	if !ok || !strings.Contains(msg, "pve") {
		t.Fatalf("ok=%v msg=%q", ok, msg)
	}
	ok, msg = TestarHost("http://127.0.0.1:1", "", 500*time.Millisecond)
	if ok {
		t.Fatal("host morto passou no teste")
	}
	if strings.Contains(msg, "dial tcp") {
		t.Fatalf("mensagem crua do net/http vazou para a tela: %q", msg)
	}
}
