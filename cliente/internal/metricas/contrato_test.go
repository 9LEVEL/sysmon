package metricas

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// O agente e o cliente ainda sao modulos separados enquanto a migracao
// acontece, entao o contrato do fio existe em duas copias. Divergencia entre
// elas nao daria erro de compilacao nem de execucao: daria um campo que o
// cliente le como zero para sempre, calado. Este teste torna isso impossivel.
//
// Quando os dois virarem um modulo so, este arquivo pode sumir - e a copia
// tambem.

var reTag = regexp.MustCompile(`json:"([^",]+)`)

func tags(t *testing.T, caminho string) []string {
	t.Helper()
	fonte, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatalf("nao consegui ler %s: %v", caminho, err)
	}
	var out []string
	for _, m := range reTag.FindAllStringSubmatch(string(fonte), -1) {
		if m[1] != "-" {
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

func TestContratoNaoDivergiuDoAgente(t *testing.T) {
	nosso := tags(t, "tipos.go")
	deles := tags(t, "../../../linux-agent/tipos.go")

	if len(nosso) == 0 || len(deles) == 0 {
		t.Fatal("nenhuma tag encontrada - o regex ou o caminho quebrou")
	}
	if strings.Join(nosso, ",") == strings.Join(deles, ",") {
		return
	}

	falta := diferenca(deles, nosso)
	sobra := diferenca(nosso, deles)
	if len(falta) > 0 {
		t.Errorf("o agente serve campos que o cliente nao le: %v", falta)
	}
	if len(sobra) > 0 {
		t.Errorf("o cliente espera campos que o agente nao serve: %v", sobra)
	}
}

func diferenca(a, b []string) []string {
	tem := make(map[string]bool, len(b))
	for _, s := range b {
		tem[s] = true
	}
	var out []string
	for _, s := range a {
		if !tem[s] {
			out = append(out, s)
		}
	}
	return out
}
