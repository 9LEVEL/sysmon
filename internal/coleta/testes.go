package coleta

import (
	"os"
	"path/filepath"
	"testing"
)

// FontesDeTeste monta uma raiz falsa de /proc e /sys com o conteudo dado.
//
// Vive num arquivo normal, e nao num _test.go, porque quem testa contra ela
// nao e so este pacote: o servidor do agente e o modo local do cliente
// tambem precisam de um sistema de arquivos previsivel. Duplicar o helper
// em cada um seria tres copias divergindo com o tempo.
//
// Recebe *testing.T de proposito: assim ela so pode ser chamada de dentro de
// um teste, e o diretorio temporario e limpo pelo proprio testing.
func FontesDeTeste(t *testing.T, arquivos map[string]string) Fontes {
	t.Helper()
	raiz := t.TempDir()
	for caminho, conteudo := range arquivos {
		completo := filepath.Join(raiz, caminho)
		if err := os.MkdirAll(filepath.Dir(completo), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(completo, []byte(conteudo), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return Fontes{raiz: raiz, NetIgnorar: NetIgnorarPadrao}
}
