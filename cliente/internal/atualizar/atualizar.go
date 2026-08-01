// Package atualizar troca o proprio binario pela versao nova.
//
// A versao em Python precisava de um lancador: um .pyz nao pode se
// sobrescrever com o Python o segurando aberto, entao um .vbs, um .bat ou um
// .sh promoviam o arquivo baixado antes do proximo arranque. Isso era codigo
// em quatro linguagens, um laco de repeticao em cada, e uma classe de falha
// silenciosa quando o lancador nao existia ao lado.
//
// Com um binario unico o truque some. O Windows nao deixa SOBRESCREVER um
// executavel em uso, mas deixa RENOMEAR: renomeia-se o atual para .old,
// grava-se o novo no lugar, reexecuta e apaga o .old no arranque seguinte.
// No Unix nem isso e preciso - substituir arquivo aberto e legitimo.
//
// Falhar aqui nunca pode atrapalhar o monitoramento: qualquer erro de rede,
// JSON ou disco vira "sem atualizacao" e a vida segue.
package atualizar

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	Repo      = "9LEVEL/sysmon"
	Somas     = "SHA256SUMS"
	tempoLim  = 30 * time.Second
	tetoBytes = 80 << 20 // o binario tem dezenas de MB; teto de sanidade
	sufixoOld = ".old"
)

// API e variavel para os testes apontarem para um GitHub de mentira.
var API = "https://api.github.com/repos/" + Repo + "/releases/latest"

// Estado e o que a interface consulta.
type Estado struct {
	Suportado  bool // false quando rodando de `go run`, sem binario proprio
	Checando   bool
	Disponivel string // tag nova, quando houver
	Pronta     bool   // baixada, conferida e pronta para aplicar
	Erro       string
	Versao     string
}

// Atualizador guarda o estado da verificacao.
type Atualizador struct {
	versao    string
	exe       string
	intervalo time.Duration
	cliente   *http.Client

	mu      sync.Mutex
	estado  Estado
	baixado []byte
}

func Novo(versao string, intervalo time.Duration) *Atualizador {
	exe, err := os.Executable()
	if err == nil {
		exe, _ = filepath.EvalSymlinks(exe)
	}
	a := &Atualizador{
		versao: versao, exe: exe, intervalo: intervalo,
		cliente: &http.Client{Timeout: tempoLim},
	}
	a.estado = Estado{Suportado: exe != "", Versao: versao}
	return a
}

func (a *Atualizador) Estado() Estado {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.estado
}

func (a *Atualizador) marcar(f func(*Estado)) {
	a.mu.Lock()
	f(&a.estado)
	a.mu.Unlock()
}

// LimparAntigo apaga o binario da versao anterior, deixado pela troca.
//
// Roda no arranque porque e o unico momento em que ele com certeza nao esta
// mais em uso. Falhar e irrelevante: sobra um arquivo, e a proxima tentativa
// o pega.
func (a *Atualizador) LimparAntigo() {
	if a.exe != "" {
		_ = os.Remove(a.exe + sufixoOld)
	}
}

// Iniciar verifica no arranque e a cada intervalo, numa goroutine.
func (a *Atualizador) Iniciar(parar <-chan struct{}) {
	if !a.Estado().Suportado || a.intervalo <= 0 {
		return
	}
	go func() {
		// Espera a janela subir antes de gastar rede com atualizacao.
		select {
		case <-time.After(20 * time.Second):
		case <-parar:
			return
		}
		for {
			a.Verificar()
			select {
			case <-time.After(a.intervalo):
			case <-parar:
				return
			}
		}
	}()
}

// VerificarEmThread dispara uma verificacao sob demanda, para o botao.
//
// Devolve false se ja ha uma em curso: clicar de novo nao enfileira
// downloads do mesmo arquivo.
func (a *Atualizador) VerificarEmThread(aoTerminar func()) bool {
	a.mu.Lock()
	if a.estado.Checando {
		a.mu.Unlock()
		return false
	}
	a.estado.Checando = true
	a.mu.Unlock()

	go func() {
		a.verificar(true)
		if aoTerminar != nil {
			aoTerminar()
		}
	}()
	return true
}

func (a *Atualizador) Verificar() { a.verificar(false) }

func (a *Atualizador) verificar(jaMarcado bool) {
	if !a.Estado().Suportado {
		return
	}
	if !jaMarcado {
		a.marcar(func(e *Estado) { e.Checando = true; e.Erro = "" })
	}
	tag, ativos, err := a.consultar()
	if err != nil {
		a.marcar(func(e *Estado) { e.Checando = false; e.Erro = err.Error() })
		return
	}
	if Compara(tag, a.versao) <= 0 {
		a.marcar(func(e *Estado) {
			e.Checando, e.Disponivel, e.Pronta = false, "", false
		})
		return
	}

	nome := NomeDoAtivo(runtime.GOOS, runtime.GOARCH)
	urlBin, temBin := ativos[nome]
	urlSomas, temSomas := ativos[Somas]
	if !temBin || !temSomas {
		a.marcar(func(e *Estado) {
			e.Checando = false
			e.Erro = fmt.Sprintf("release %s sem %s ou %s", tag, nome, Somas)
		})
		return
	}

	a.marcar(func(e *Estado) { e.Disponivel = tag })
	corpo, err := a.baixarEConferir(urlBin, urlSomas, nome)
	if err != nil {
		a.marcar(func(e *Estado) { e.Checando = false; e.Erro = err.Error() })
		return
	}
	a.mu.Lock()
	a.baixado = corpo
	a.estado.Checando, a.estado.Pronta, a.estado.Erro = false, true, ""
	a.mu.Unlock()
}

func (a *Atualizador) consultar() (string, map[string]string, error) {
	req, err := http.NewRequest(http.MethodGet, API, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", "sysmon/"+a.versao)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := a.cliente.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("sem conexao (%v)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("github respondeu %d", resp.StatusCode)
	}
	var doc struct {
		Tag    string `json:"tag_name"`
		Ativos []struct {
			Nome string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return "", nil, fmt.Errorf("resposta inesperada (%v)", err)
	}
	m := make(map[string]string, len(doc.Ativos))
	for _, at := range doc.Ativos {
		m[at.Nome] = at.URL
	}
	return doc.Tag, m, nil
}

func (a *Atualizador) baixarEConferir(urlBin, urlSomas, nome string) ([]byte, error) {
	corpo, err := a.buscar(urlBin, tetoBytes)
	if err != nil {
		return nil, err
	}
	texto, err := a.buscar(urlSomas, 256<<10)
	if err != nil {
		return nil, err
	}
	esperado, ok := SomaDe(string(texto), nome)
	if !ok {
		return nil, fmt.Errorf("%s nao lista %s", Somas, nome)
	}
	obtido := sha256.Sum256(corpo)
	if hex.EncodeToString(obtido[:]) != esperado {
		// A soma do release e a unica garantia de que veio inteiro e do
		// lugar certo. Sem ela nao se troca nada.
		return nil, fmt.Errorf("SHA256 nao confere (esperado %s..., veio %s...)",
			esperado[:12], hex.EncodeToString(obtido[:])[:12])
	}
	if !ExecutavelValido(corpo, runtime.GOOS) {
		// Download corrompido de outro jeito, ou uma pagina de erro servida
		// com codigo 200. Melhor descobrir agora que no proximo arranque.
		return nil, fmt.Errorf("o arquivo baixado nao parece um executavel")
	}
	return corpo, nil
}

func (a *Atualizador) buscar(url string, teto int64) ([]byte, error) {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "sysmon/"+a.versao)
	resp, err := a.cliente.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sem conexao (%v)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download respondeu %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, teto))
}

// Aplicar troca o binario e devolve o comando que sobe a versao nova.
//
// Nao reexecuta sozinho: quem chama precisa fechar a janela e a bandeja
// antes, e so ele sabe fazer isso na ordem certa.
func (a *Atualizador) Aplicar() (string, error) {
	a.mu.Lock()
	corpo, pronta, exe := a.baixado, a.estado.Pronta, a.exe
	a.mu.Unlock()
	if !pronta || len(corpo) == 0 {
		return "", fmt.Errorf("nao ha atualizacao pronta")
	}
	if err := Trocar(exe, corpo); err != nil {
		return "", err
	}
	return exe, nil
}

// Trocar poe o binario novo no lugar do atual.
//
// A ordem importa e e ela que faz isto funcionar com o programa rodando:
//
//  1. grava o novo ao lado, com nome temporario;
//  2. renomeia o atual para .old - o Windows permite renomear um executavel
//     em uso, embora nao permita sobrescrever;
//  3. renomeia o novo para o nome definitivo.
//
// Se o passo 3 falhar, o passo 2 e desfeito: ficar sem binario nenhum seria
// o pior desfecho possivel de uma atualizacao.
func Trocar(exe string, corpo []byte) error {
	if exe == "" {
		return fmt.Errorf("nao sei qual e o meu proprio binario")
	}
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".sysmon-novo-*")
	if err != nil {
		return fmt.Errorf("nao consegui gravar ao lado do binario: %w", err)
	}
	nomeTmp := tmp.Name()
	defer os.Remove(nomeTmp) // no-op quando o rename deu certo

	if _, err := tmp.Write(corpo); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(nomeTmp, 0o755); err != nil {
		return err
	}

	velho := exe + sufixoOld
	_ = os.Remove(velho) // sobra de uma troca anterior
	if err := os.Rename(exe, velho); err != nil {
		return fmt.Errorf("nao consegui afastar o binario atual: %w", err)
	}
	if err := os.Rename(nomeTmp, exe); err != nil {
		// Desfaz: sem isto o programa some do disco.
		_ = os.Rename(velho, exe)
		return fmt.Errorf("nao consegui pôr o binario novo no lugar: %w", err)
	}
	return nil
}

// NomeDoAtivo e o nome do binario desta plataforma no release.
func NomeDoAtivo(so, arco string) string {
	nome := fmt.Sprintf("sysmon-%s-%s", so, arco)
	if so == "windows" {
		nome += ".exe"
	}
	return nome
}

// SomaDe extrai a soma de um arquivo do texto do SHA256SUMS.
func SomaDe(texto, nome string) (string, bool) {
	for _, linha := range strings.Split(texto, "\n") {
		campos := strings.Fields(linha)
		if len(campos) != 2 {
			continue
		}
		// O formato do sha256sum marca binario com '*' antes do nome.
		if strings.TrimPrefix(campos[1], "*") == nome {
			return strings.ToLower(campos[0]), true
		}
	}
	return "", false
}

// ExecutavelValido confere a assinatura do arquivo baixado.
//
// Pega download truncado e, principalmente, pagina de erro servida com
// codigo 200 - que um SHA de outro arquivo nao pegaria se a soma tambem
// viesse errada.
func ExecutavelValido(corpo []byte, so string) bool {
	if len(corpo) < 4 {
		return false
	}
	if so == "windows" {
		return corpo[0] == 'M' && corpo[1] == 'Z'
	}
	return corpo[0] == 0x7f && string(corpo[1:4]) == "ELF"
}

var reNum = regexp.MustCompile(`\d+`)

// Compara devolve >0 se a for mais nova que b.
//
// Numero, e nao texto: comparando como string, "4.10.0" viria antes de
// "4.9.0" e a atualizacao pararia de ser oferecida na decima versao menor.
func Compara(a, b string) int {
	na, nb := numeros(a), numeros(b)
	for i := 0; i < 3; i++ {
		x, y := 0, 0
		if i < len(na) {
			x = na[i]
		}
		if i < len(nb) {
			y = nb[i]
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}

func numeros(s string) []int {
	achados := reNum.FindAllString(s, 3)
	out := make([]int, 0, len(achados))
	for _, n := range achados {
		v, _ := strconv.Atoi(n)
		out = append(out, v)
	}
	return out
}
