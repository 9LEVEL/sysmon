//go:build !linux

package coleta

import "sysmon/internal/metricas"

// Discos nao existe fora do Linux: statfs e /proc/mounts sao daqui.
//
// O pacote inteiro compila para Windows por um motivo so - o cliente linka
// ele por causa do subcomando `local`, e o binario do Windows e um so. Aqui a
// resposta e uma lista vazia, e nao um erro, porque `sysmon local` ja recusa
// rodar fora do Linux com uma mensagem que diz o que fazer no lugar; quem
// chegar ate aqui em outro sistema esta num teste.
func (f Fontes) Discos(pontos []string) []metricas.Disco {
	return []metricas.Disco{}
}
