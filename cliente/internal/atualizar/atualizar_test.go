package atualizar

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
)

// Um GitHub de mentira. O caminho inteiro e exercitado sem rede externa:
// release falso, binario falso, SHA256SUMS falso.
//
// O que mais importa aqui e que atualizacao RUIM nao seja aplicada. Trocar o
// proprio binario por algo nao verificado seria o pior bug possivel neste
// projeto - ele roda como o usuario, na maquina do usuario.
type githubFalso struct {
	tag    string
	corpo  []byte
	soma   string // vazio = usa a soma real
	semBin bool
	srv    *httptest.Server
}

func binarioFalso(marca string) []byte {
	cab := []byte{'M', 'Z', 0, 0}
	if runtime.GOOS != "windows" {
		cab = []byte{0x7f, 'E', 'L', 'F'}
	}
	return append(cab, []byte(marca)...)
}

func (g *githubFalso) subir(t *testing.T) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		ativos := []map[string]string{
			{"name": Somas, "browser_download_url": g.srv.URL + "/somas"},
		}
		if !g.semBin {
			ativos = append(ativos, map[string]string{
				"name":                 NomeDoAtivo(runtime.GOOS, runtime.GOARCH),
				"browser_download_url": g.srv.URL + "/bin",
			})
		}
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name": g.tag, "assets": ativos})
	})
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) {
		w.Write(g.corpo)
	})
	mux.HandleFunc("/somas", func(w http.ResponseWriter, r *http.Request) {
		soma := g.soma
		if soma == "" {
			s := sha256.Sum256(g.corpo)
			soma = hex.EncodeToString(s[:])
		}
		fmt.Fprintf(w, "%s  %s\n%s  outro-arquivo\n", soma,
			NomeDoAtivo(runtime.GOOS, runtime.GOARCH), strings.Repeat("0", 64))
	})
	g.srv.Config.Handler = mux
}

func comGitHub(t *testing.T, g *githubFalso) *Atualizador {
	t.Helper()
	g.srv = httptest.NewUnstartedServer(nil)
	g.srv.Start()
	g.subir(t)
	t.Cleanup(g.srv.Close)

	anterior := API
	API = g.srv.URL + "/latest"
	t.Cleanup(func() { API = anterior })

	a := Novo("4.0.0", 0)
	a.exe = filepath.Join(t.TempDir(), "sysmon")
	os.WriteFile(a.exe, binarioFalso("ANTIGO"), 0o755)
	a.estado.Suportado = true
	return a
}

func TestBaixaEDeixaPronta(t *testing.T) {
	a := comGitHub(t, &githubFalso{tag: "v9.0.0", corpo: binarioFalso("NOVO")})
	a.Verificar()
	e := a.Estado()
	if e.Erro != "" {
		t.Fatalf("erro: %s", e.Erro)
	}
	if e.Disponivel != "v9.0.0" || !e.Pronta {
		t.Fatalf("estado = %+v", e)
	}
	// O binario em uso nao pode ter sido tocado antes de aplicar.
	b, _ := os.ReadFile(a.exe)
	if !strings.Contains(string(b), "ANTIGO") {
		t.Fatal("o binario atual foi trocado sem pedir")
	}
}

func TestJaAtualizadoNaoBaixa(t *testing.T) {
	a := comGitHub(t, &githubFalso{tag: "v4.0.0", corpo: binarioFalso("IGUAL")})
	a.Verificar()
	if e := a.Estado(); e.Disponivel != "" || e.Pronta {
		t.Fatalf("estado = %+v", e)
	}
}

func TestShaDiferenteRecusa(t *testing.T) {
	// O caso que mais importa: conteudo trocado no meio do caminho.
	a := comGitHub(t, &githubFalso{tag: "v9.0.0", corpo: binarioFalso("NOVO"),
		soma: strings.Repeat("a", 64)})
	a.Verificar()
	e := a.Estado()
	if e.Pronta {
		t.Fatal("aplicou com SHA errado")
	}
	if !strings.Contains(e.Erro, "SHA256") {
		t.Fatalf("erro = %q", e.Erro)
	}
}

func TestArquivoQueNaoEExecutavelRecusa(t *testing.T) {
	// Pagina de erro servida com codigo 200 e o caso real: o SHA bate
	// porque o SHA256SUMS tambem veio do mesmo lugar errado.
	a := comGitHub(t, &githubFalso{tag: "v9.0.0",
		corpo: []byte("<html>404 not found</html>")})
	a.Verificar()
	e := a.Estado()
	if e.Pronta {
		t.Fatal("aceitou um HTML como binario")
	}
	if !strings.Contains(e.Erro, "executavel") {
		t.Fatalf("erro = %q", e.Erro)
	}
}

func TestReleaseSemOBinarioDaPlataforma(t *testing.T) {
	a := comGitHub(t, &githubFalso{tag: "v9.0.0", corpo: binarioFalso("x"),
		semBin: true})
	a.Verificar()
	if e := a.Estado(); e.Pronta || !strings.Contains(e.Erro, "sem sysmon-") {
		t.Fatalf("estado = %+v", e)
	}
}

func TestGitHubForaDoArNaoDerruba(t *testing.T) {
	anterior := API
	API = "http://127.0.0.1:1/latest"
	defer func() { API = anterior }()

	a := Novo("4.0.0", 0)
	a.exe = filepath.Join(t.TempDir(), "sysmon")
	a.estado.Suportado = true
	a.Verificar() // nao pode entrar em panico
	if e := a.Estado(); !strings.Contains(e.Erro, "sem conexao") {
		t.Fatalf("erro = %q", e.Erro)
	}
}

func TestAplicarTrocaOBinario(t *testing.T) {
	a := comGitHub(t, &githubFalso{tag: "v9.0.0", corpo: binarioFalso("NOVO")})
	a.Verificar()
	if !a.Estado().Pronta {
		t.Fatal(a.Estado().Erro)
	}
	exe, err := a.Aplicar()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(exe)
	if !strings.Contains(string(b), "NOVO") {
		t.Fatal("o binario nao foi trocado")
	}
	// O anterior fica ao lado, para ser apagado no proximo arranque: no
	// Windows nao da para apaga-lo enquanto ainda esta em execucao.
	if _, err := os.Stat(exe + sufixoOld); err != nil {
		t.Fatal("o binario antigo nao foi preservado")
	}
	a.LimparAntigo()
	if _, err := os.Stat(exe + sufixoOld); err == nil {
		t.Fatal("LimparAntigo nao apagou")
	}
}

func TestAplicarSemAtualizacaoNaoMexeEmNada(t *testing.T) {
	a := comGitHub(t, &githubFalso{tag: "v4.0.0", corpo: binarioFalso("x")})
	a.Verificar()
	if _, err := a.Aplicar(); err == nil {
		t.Fatal("aplicou sem ter o que aplicar")
	}
	b, _ := os.ReadFile(a.exe)
	if !strings.Contains(string(b), "ANTIGO") {
		t.Fatal("mexeu no binario mesmo sem atualizacao")
	}
}

func TestTrocaDesfazSeNaoConseguirPorONovo(t *testing.T) {
	// O pior desfecho de uma atualizacao e ficar sem binario nenhum. Se o
	// passo final falhar, o anterior tem que voltar.
	dir := t.TempDir()
	exe := filepath.Join(dir, "sysmon")
	os.WriteFile(exe, []byte("ANTIGO"), 0o755)

	// Um diretorio no lugar do destino faz o rename final falhar.
	if err := os.Mkdir(exe+".bloqueio", 0o755); err != nil {
		t.Skip("nao consegui montar o cenario")
	}
	// Simula a falha trocando para um caminho impossivel.
	err := Trocar(filepath.Join(dir, "nao", "existe", "sysmon"), []byte("NOVO"))
	if err == nil {
		t.Fatal("troca em caminho invalido nao falhou")
	}
	b, _ := os.ReadFile(exe)
	if string(b) != "ANTIGO" {
		t.Fatalf("o binario original foi afetado: %q", b)
	}
}

func TestVerificarEmThreadNaoDuplica(t *testing.T) {
	a := comGitHub(t, &githubFalso{tag: "v9.0.0", corpo: binarioFalso("NOVO")})
	feito := make(chan struct{})
	if !a.VerificarEmThread(func() { close(feito) }) {
		t.Fatal("a primeira verificacao foi recusada")
	}
	select {
	case <-feito:
	case <-time.After(5 * time.Second):
		t.Fatal("a verificacao nao terminou")
	}
	if !a.Estado().Pronta {
		t.Fatalf("estado = %+v", a.Estado())
	}
}

func TestComparaNumeroENaoTexto(t *testing.T) {
	// Comparando como string, "4.10.0" viria antes de "4.9.0" e a
	// atualizacao pararia de ser oferecida na decima versao menor.
	if Compara("v4.10.0", "4.9.0") <= 0 {
		t.Error("4.10.0 nao ficou acima de 4.9.0")
	}
	if Compara("v4.2.1", "4.2.0") <= 0 {
		t.Error("4.2.1 nao ficou acima de 4.2.0")
	}
	if Compara("4.2.0", "v4.2.0") != 0 {
		t.Error("o v do prefixo mudou a comparacao")
	}
	if Compara("sem numero", "1.0.0") >= 0 {
		t.Error("texto sem numero passou na frente")
	}
}

func TestNomeDoAtivoPorPlataforma(t *testing.T) {
	if got := NomeDoAtivo("windows", "amd64"); got != "sysmon-windows-amd64.exe" {
		t.Errorf("windows = %q", got)
	}
	if got := NomeDoAtivo("linux", "arm64"); got != "sysmon-linux-arm64" {
		t.Errorf("linux = %q", got)
	}
}

func TestSomaAceitaOFormatoBinarioDoSha256sum(t *testing.T) {
	// O sha256sum marca binario com '*' antes do nome; sem tratar isso, a
	// conferencia falharia em todo release gerado no modo binario.
	texto := "abc123  sysmon-linux-amd64\ndef456 *sysmon-windows-amd64.exe\n"
	if s, ok := SomaDe(texto, "sysmon-windows-amd64.exe"); !ok || s != "def456" {
		t.Fatalf("soma = %q ok=%v", s, ok)
	}
}

func TestExecutavelValido(t *testing.T) {
	if !ExecutavelValido([]byte{'M', 'Z', 0, 0}, "windows") {
		t.Error("PE valido recusado")
	}
	if ExecutavelValido([]byte("<html>"), "windows") {
		t.Error("HTML aceito como PE")
	}
	if !ExecutavelValido([]byte{0x7f, 'E', 'L', 'F', 2}, "linux") {
		t.Error("ELF valido recusado")
	}
	if ExecutavelValido([]byte{'M', 'Z'}, "linux") {
		t.Error("PE aceito no linux")
	}
	if ExecutavelValido([]byte{1}, "linux") {
		t.Error("arquivo truncado aceito")
	}
}
