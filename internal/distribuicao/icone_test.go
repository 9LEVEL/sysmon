package distribuicao

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// O icone do Windows entra pelo cmd/sysmon/icone_windows.syso, que o
// linkeditor do Go embute sozinho. E um objeto COFF binario, versionado: se
// ele for regerado errado, o build ou falha ("fail to read string table") ou
// - pior - passa e o executavel sai com o icone generico. O segundo caso so
// se descobre olhando o Explorer no Windows, que e onde ninguem olha.
//
// Este teste le a estrutura e confere o essencial.

const (
	rtIcon      = 3
	rtGroupIcon = 14
)

func syso(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(raiz(t), "cmd", "sysmon",
		"icone_windows.syso"))
	if err != nil {
		t.Fatalf("sem o recurso do icone: %v", err)
	}
	return b
}

func TestOSysoEUmCOFFValido(t *testing.T) {
	b := syso(t)
	if len(b) < 60 {
		t.Fatal("pequeno demais para ser um objeto COFF")
	}
	if maq := binary.LittleEndian.Uint16(b); maq != 0x8664 {
		t.Errorf("maquina = %#x, queria amd64 (0x8664) - o cliente do Windows "+
			"so sai em amd64", maq)
	}
	if n := binary.LittleEndian.Uint16(b[2:]); n != 1 {
		t.Fatalf("secoes = %d, queria 1", n)
	}
	if nome := string(b[20:25]); nome != ".rsrc" {
		t.Fatalf("secao = %q, queria .rsrc", nome)
	}
	// Cada simbolo COFF tem 18 bytes; passar disso faz a tabela de strings ser
	// lida do lugar errado, que foi exatamente o defeito da primeira versao.
	ofsSimbolos := binary.LittleEndian.Uint32(b[8:])
	nSimbolos := binary.LittleEndian.Uint32(b[12:])
	fim := ofsSimbolos + nSimbolos*18
	if int(fim)+4 > len(b) {
		t.Fatalf("a tabela de strings comecaria em %d, alem dos %d bytes do "+
			"arquivo", fim, len(b))
	}
}

func TestOSysoTemGrupoDeIconeETodosOsTamanhos(t *testing.T) {
	b := syso(t)
	raw := binary.LittleEndian.Uint32(b[20+20:]) // PointerToRawData da secao

	// Nivel 1 da arvore de recursos: os tipos.
	entradas := func(off uint32) [][2]uint32 {
		nNome := binary.LittleEndian.Uint16(b[raw+off+12:])
		nID := binary.LittleEndian.Uint16(b[raw+off+14:])
		var out [][2]uint32
		for i := 0; i < int(nNome+nID); i++ {
			p := raw + off + 16 + uint32(i)*8
			out = append(out, [2]uint32{
				binary.LittleEndian.Uint32(b[p:]),
				binary.LittleEndian.Uint32(b[p+4:]) &^ 0x80000000,
			})
		}
		return out
	}

	temGrupo, nIcones := false, 0
	for _, e := range entradas(0) {
		switch e[0] {
		case rtGroupIcon:
			temGrupo = true
		case rtIcon:
			nIcones = len(entradas(e[1]))
		}
	}
	if !temGrupo {
		t.Error("sem RT_GROUP_ICON: o Windows nao acha o icone do executavel")
	}
	// 16, 24, 32, 48, 64, 128, 256 - o de 16 e o que mais aparece.
	if nIcones != 7 {
		t.Errorf("tamanhos embutidos = %d, queria 7", nIcones)
	}
}
