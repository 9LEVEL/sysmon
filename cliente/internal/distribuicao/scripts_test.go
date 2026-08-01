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
	return filepath.Join("..", "..", "..")
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
	nomes, err := filepath.Glob(filepath.Join(raiz(t), "windows-tray", "*"+ext))
	if err != nil || len(nomes) == 0 {
		t.Fatalf("nenhum %s em windows-tray/", ext)
	}
	return nomes
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
	for _, ext := range []string{".ps1", ".json"} {
		for _, arq := range scripts(t, ext) {
			b, _ := os.ReadFile(arq)
			texto := strings.ToLower(string(b))
			for _, agulha := range []string{"python", ".pyz", "pystray", "tkinter"} {
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
