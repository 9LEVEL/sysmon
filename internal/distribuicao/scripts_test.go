// Package distribuicao guarda as checagens estaticas dos scripts que vao no
// pacote.
//
// Nao ha PowerShell na maquina de build, entao estes erros so apareciam na
// maquina do usuario, um por vez, a cada release. Sao checagens simples, mas
// cobrem exatamente o que ja quebrou de verdade aqui - e por isso elas
// sobreviveram a migracao do cliente para Go, portadas da suite Python que
// foi aposentada junto com o .pyz.
package distribuicao

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func raiz(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..")
}

func ler(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(raiz(t), rel))
	if err != nil {
		t.Fatalf("nao consegui ler %s: %v", rel, err)
	}
	return string(b)
}

func scripts(t *testing.T, ext string) []string {
	t.Helper()
	return scriptsEm(t, ext, "windows-tray")
}

// scriptsEm varre uma ou mais pastas. Existe porque as checagens nasceram
// olhando so o windows-tray, e o deploy.sh do linux-agent guardava a mesma
// classe de erro sem ninguem olhar.
func scriptsEm(t *testing.T, ext string, pastas ...string) []string {
	t.Helper()
	var out []string
	for _, p := range pastas {
		nomes, err := filepath.Glob(filepath.Join(raiz(t), p, "*"+ext))
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		out = append(out, nomes...)
	}
	if len(out) == 0 {
		t.Fatalf("nenhum %s em %v", ext, pastas)
	}
	return out
}

func TestVariavelNaoColideComParametro(t *testing.T) {
	// PowerShell nao diferencia maiusculas em nome de variavel. Atribuir
	// string a uma variavel homonima de um [switch]$X derruba o script com
	// ArgumentTransformationMetadataException - foi exatamente o que
	// aconteceu com $inicializar vs [switch]$Inicializar.
	reParam := regexp.MustCompile(`\[(?:switch|int|string|bool)\]\$(\w+)`)
	for _, arq := range scripts(t, ".ps1") {
		b, _ := os.ReadFile(arq)
		texto := string(b)
		for _, m := range reParam.FindAllStringSubmatch(texto, -1) {
			re := regexp.MustCompile(`(?im)^\s*\$` + m[1] + `\s*=`)
			if n := re.FindAllString(texto, -1); len(n) > 0 {
				t.Errorf("%s: $%s e parametro e recebe atribuicao (%dx). "+
					"Renomeie a variavel local.", filepath.Base(arq), m[1], len(n))
			}
		}
	}
}

func TestBalanceamento(t *testing.T) {
	for _, arq := range scripts(t, ".ps1") {
		b, _ := os.ReadFile(arq)
		var corpo strings.Builder
		for _, l := range strings.Split(string(b), "\n") {
			if i := strings.Index(l, "#"); i >= 0 {
				l = l[:i]
			}
			corpo.WriteString(l + "\n")
		}
		s := corpo.String()
		for _, par := range []struct{ abre, fecha string }{{"{", "}"}, {"(", ")"}} {
			if a, f := strings.Count(s, par.abre), strings.Count(s, par.fecha); a != f {
				t.Errorf("%s: %s%s desbalanceado (%d vs %d)",
					filepath.Base(arq), par.abre, par.fecha, a, f)
			}
		}
	}
}

func TestArquivosReferenciadosExistem(t *testing.T) {
	// Um .ps1 que chama arquivo inexistente so falha na maquina do usuario.
	re := regexp.MustCompile(`"(sysmon\.exe|config\.example\.json)"`)
	for _, arq := range scripts(t, ".ps1") {
		b, _ := os.ReadFile(arq)
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			if m[1] == "sysmon.exe" {
				continue // vem do release, nao do repositorio
			}
			p := filepath.Join(raiz(t), "windows-tray", m[1])
			if _, err := os.Stat(p); err != nil {
				t.Errorf("%s referencia %s, que nao existe",
					filepath.Base(arq), m[1])
			}
		}
	}
}

func TestInstaladorCobreOsDoisAutostarts(t *testing.T) {
	// Deixar os dois ativos faz duas instancias subirem no logon.
	texto := ler(t, "windows-tray/instalar-autostart.ps1")
	for _, agulha := range []string{"Register-ScheduledTask",
		`GetFolderPath("Startup")`, "Remove-Item $atalhoInicio"} {
		if !strings.Contains(texto, agulha) {
			t.Errorf("instalar-autostart.ps1 sem %q", agulha)
		}
	}
}

func TestLimpezaPreservaConfig(t *testing.T) {
	// O config.json tem os tokens; apaga-lo seria perder o acesso a frota.
	texto := ler(t, "windows-tray/limpar.ps1")
	re := regexp.MustCompile(`Remove-Item[^\n]*config\.json`)
	if re.MatchString(texto) {
		t.Fatal("limpar.ps1 apaga o config.json")
	}
	if !strings.Contains(texto, "MANTIDO") {
		t.Error("limpar.ps1 nao avisa que o config foi mantido")
	}
}

func TestNadaMaisMencionaPython(t *testing.T) {
	// A migracao terminou. Uma mencao sobrando manda o usuario instalar algo
	// que nao e mais preciso - e o tipo de instrucao errada que faz alguem
	// desistir na primeira tentativa.
	// Os .sh entraram depois: o deploy.sh ainda mandava rodar
	// `python3 sysmon.py term` no fim da instalacao, e o install.sh chamava o
	// cliente de "sysmon.pyz" - dois caminhos mortos na ULTIMA linha que o
	// usuario le, que e a que ele vai seguir.
	for _, ext := range []string{".ps1", ".json", ".sh"} {
		for _, arq := range scriptsEm(t, ext, "windows-tray", "linux-agent") {
			b, _ := os.ReadFile(arq)
			texto := strings.ToLower(string(b))
			for _, agulha := range []string{"python", ".pyz", "pystray", "tkinter",
				"sysmon.bat", "sysmon.vbs", "sysmon.py ", "diagnostico.bat"} {
				// O comentario historico do proprio arquivo explica o que
				// mudou; o que nao pode e INSTRUCAO viva.
				for _, linha := range strings.Split(texto, "\n") {
					if strings.Contains(linha, agulha) &&
						!strings.HasPrefix(strings.TrimSpace(linha), "#") {
						t.Errorf("%s: instrucao viva mencionando %q: %s",
							filepath.Base(arq), agulha, strings.TrimSpace(linha))
					}
				}
			}
		}
	}
}

func TestEmpacotarGeraOQueOAutoUpdateProcura(t *testing.T) {
	// O cliente instalado procura estes nomes exatos no release. Mudar um
	// deles no empacotar.sh quebraria a atualizacao de todo mundo, em
	// silencio - o erro so apareceria semanas depois.
	texto := ler(t, "empacotar.sh")
	for _, agulha := range []string{"sysmon-windows-amd64.exe",
		"sysmon-linux-amd64", "SHA256SUMS"} {
		if !strings.Contains(texto, agulha) {
			t.Errorf("empacotar.sh nao produz %q", agulha)
		}
	}
	if !strings.Contains(texto, "-H windowsgui") {
		t.Error("o binario do Windows nao sai como GUI - abriria janela preta")
	}
}

func TestCaminhosDosScriptsDeBuildExistem(t *testing.T) {
	// O checar-versao.sh apontava para linux-agent/main.go e
	// linux-agent/Makefile. Quando os dois modulos viraram um so, os dois
	// sumiram e o script passou a morrer com "No such file or directory" - ou
	// seja, falhando pelo motivo errado, que e quase pior que nao falhar,
	// porque a mensagem nao diz que a conferencia deixou de acontecer.
	//
	// Este teste le os caminhos que os scripts de build citam e confere que
	// ainda existem. Nao substitui rodar o script, mas roda em `make teste`,
	// que e onde o erro precisava ter aparecido.
	scripts := []string{"checar-versao.sh", "empacotar.sh", "Makefile"}
	caminho := regexp.MustCompile(`(?:\$AQUI/|\./)((?:cmd|internal|linux-agent|windows-tray)/[A-Za-z0-9._/-]+)`)

	for _, s := range scripts {
		texto := ler(t, s)
		for _, m := range caminho.FindAllStringSubmatch(texto, -1) {
			rel := m[1]
			if strings.ContainsAny(rel, "*$") {
				continue // glob ou variavel: nao da para conferir estaticamente
			}
			if _, err := os.Stat(filepath.Join(raiz(t), rel)); err != nil {
				t.Errorf("%s cita %s, que nao existe", s, rel)
			}
		}
	}
}

func TestNadaMaisCitaOsModulosSeparados(t *testing.T) {
	// Ate a v5.0 havia cliente/go.mod e linux-agent/go.mod. A CI ainda
	// apontava para os dois depois da consolidacao, e por isso nao rodava.
	for _, arq := range []string{
		".github/workflows/ci.yml", ".github/workflows/release.yml",
		"Makefile", "empacotar.sh", "checar-versao.sh",
	} {
		texto := ler(t, arq)
		for _, morto := range []string{"cliente/go.mod", "linux-agent/go.mod",
			"linux-agent/Makefile", "make -C linux-agent", "cd cliente"} {
			if strings.Contains(texto, morto) {
				t.Errorf("%s ainda cita %q, que nao existe desde a v5.0",
					arq, morto)
			}
		}
	}
}

func TestInstaladorCompilaOPacoteCerto(t *testing.T) {
	// O install.sh compilava com `go build .` executado dentro de
	// linux-agent/, porque ate a v5.0 aquela pasta ERA a raiz do modulo do
	// agente. Com o modulo unico ela ficou so com scripts, e o comando passou
	// a falhar com "no Go files" - silenciosamente, porque a saida ia para o
	// subshell e o script seguia em frente.
	//
	// O host que instalar por esse caminho fica sem agente nenhum, ou com o
	// binario errado, e o servico entra num laco de restart ate o systemd
	// desistir.
	texto := ler(t, "linux-agent/install.sh")
	if !strings.Contains(texto, "./cmd/sysmon-agent") {
		t.Error("o install.sh nao compila ./cmd/sysmon-agent explicitamente")
	}
	if regexp.MustCompile(`go build[^\n]*-o [^\n]*"\s*\.\s*\)`).MatchString(texto) {
		t.Error("ainda ha um `go build .` sem pacote explicito")
	}
	// E o alvo tem que existir de verdade.
	if _, err := os.Stat(filepath.Join(raiz(t), "cmd", "sysmon-agent")); err != nil {
		t.Errorf("cmd/sysmon-agent nao existe: %v", err)
	}
}

func TestOComandoDoLeiameExisteNoRelease(t *testing.T) {
	// O passo 1 do README baixa por
	// /releases/latest/download/sysmon-agent-linux-amd64.tar.gz - um nome FIXO,
	// sem versao. Com a versao embutida, quem instala precisaria saber qual e
	// antes de baixar, o que obriga a abrir o navegador no meio de um passo de
	// terminal. Este teste liga as duas pontas.
	readme := ler(t, "README.md")
	empacotar := ler(t, "empacotar.sh")

	re := regexp.MustCompile(`releases/latest/download/([A-Za-z0-9._-]+)`)
	achados := re.FindAllStringSubmatch(readme, -1)
	if len(achados) == 0 {
		t.Skip("o README nao usa mais o atalho de download direto")
	}
	// O script escreve o nome com a arquitetura numa variavel; expandimos a
	// unica que ele usa hoje para comparar com o que o README pede.
	expandido := strings.ReplaceAll(empacotar, "$ARCO", "amd64")
	for _, m := range achados {
		if !strings.Contains(expandido, m[1]) {
			t.Errorf("o README baixa %q por /releases/latest/download/, e o "+
				"empacotar.sh nao gera esse nome - o comando daria 404", m[1])
		}
	}
}
