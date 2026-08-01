//go:build linux

package coleta

import (
	"syscall"

	"sysmon/internal/metricas"
)

// Discos consulta statfs por ponto de montagem, deduplicando filesystems iguais
// por st_dev (bind mounts e subvolumes apontam para o mesmo device).
func (f Fontes) Discos(pontos []string) []metricas.Disco {
	out := []metricas.Disco{}
	vistos := map[uint64]bool{}
	for _, mp := range pontos {
		caminho := f.P(mp)
		var st syscall.Stat_t
		if err := syscall.Stat(caminho, &st); err != nil {
			continue
		}
		if vistos[uint64(st.Dev)] {
			continue
		}
		var fs syscall.Statfs_t
		if err := syscall.Statfs(caminho, &fs); err != nil {
			continue
		}
		total := int64(fs.Blocks) * fs.Bsize
		livre := int64(fs.Bavail) * fs.Bsize
		if total <= 0 {
			continue
		}
		vistos[uint64(st.Dev)] = true

		d := metricas.Disco{
			Mount:   mp,
			Total:   total,
			Usado:   total - livre,
			Percent: arred(100*float64(total-livre)/float64(total), 1),
		}
		// metricas.Disco cheio de inodes falha igual a disco cheio de bytes, e o df -h
		// nao mostra. Vale reportar.
		if fs.Files > 0 {
			usados := int64(fs.Files) - int64(fs.Ffree)
			d.InodesPercent = f64(arred(100*float64(usados)/float64(fs.Files), 1))
		}
		out = append(out, d)
	}
	return out
}
