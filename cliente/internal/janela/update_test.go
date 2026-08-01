package janela

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"sysmon-cliente/internal/atualizar"
)

// githubFalso serve um release com um binario plausivel desta plataforma.
func githubFalso(t *testing.T, tag string, corpo []byte) string {
	t.Helper()
	srv := httptest.NewUnstartedServer(nil)
	srv.Start()
	t.Cleanup(srv.Close)
	nome := atualizar.NomeDoAtivo(runtime.GOOS, runtime.GOARCH)

	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "assets": []any{
			map[string]string{"name": nome, "browser_download_url": srv.URL + "/bin"},
			map[string]string{"name": atualizar.Somas, "browser_download_url": srv.URL + "/somas"},
		}})
	})
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) { w.Write(corpo) })
	mux.HandleFunc("/somas", func(w http.ResponseWriter, r *http.Request) {
		s := sha256.Sum256(corpo)
		fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(s[:]), nome)
	})
	srv.Config.Handler = mux
	return srv.URL + "/latest"
}

func binPlausivel() []byte {
	if runtime.GOOS == "windows" {
		return append([]byte{'M', 'Z', 0, 0}, []byte("NOVO")...)
	}
	return append([]byte{0x7f, 'E', 'L', 'F'}, []byte("NOVO")...)
}

func TestBotaoDeUpdateEncontraEAnuncia(t *testing.T) {
	b := novaBancada(t)
	anterior := atualizar.API
	atualizar.API = githubFalso(t, "v9.9.9", binPlausivel())
	defer func() { atualizar.API = anterior }()

	at := atualizar.Novo("1.0.0", 0)
	// Um binario de mentira ao lado, para o atualizador se considerar
	// suportado sem tocar no binario do teste.
	exe := filepath.Join(t.TempDir(), "sysmon")
	os.WriteFile(exe, []byte("ANTIGO"), 0o755)
	b.j.Atual = at

	// Clicar no ⭳ dispara a busca; ela roda em goroutine.
	b.clique(iconeX(b.tam.X, 5), 19)
	prazo := time.Now().Add(5 * time.Second)
	for time.Now().Before(prazo) && !at.Estado().Pronta && at.Estado().Erro == "" {
		time.Sleep(20 * time.Millisecond)
	}
	e := at.Estado()
	if !e.Pronta {
		t.Fatalf("nao ficou pronta: %+v", e)
	}
	if e.Disponivel != "v9.9.9" {
		t.Fatalf("versao = %q", e.Disponivel)
	}

	// E o rodape tem que anunciar, senao a versao nova espera para sempre
	// sem ninguem saber.
	if txt := b.j.textoUpdate(); !strings.Contains(txt, "9.9.9") ||
		!strings.Contains(txt, "pronta") {
		t.Fatalf("rodape = %q", txt)
	}
}

func TestSemAtualizadorNaoQuebraNemPoluiORodape(t *testing.T) {
	b := novaBancada(t)
	b.j.Atual = nil
	b.clique(iconeX(b.tam.X, 5), 19) // nao pode entrar em panico
	if txt := b.j.textoUpdate(); txt != "" {
		t.Fatalf("rodape = %q", txt)
	}
}
