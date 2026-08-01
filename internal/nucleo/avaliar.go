package nucleo

import (
	"fmt"

	"sysmon/internal/metricas"
)

// Estado e a ultima leitura de um host. Sempre consistente: dados OU erro,
// nunca os dois.
type Estado struct {
	Dados      *metricas.Snapshot
	Erro       string
	Atualizado float64
	Falhas     int
}

// versaoAgente devolve o que o host respondeu, para a mensagem dizer qual e.
func versaoAgente(d *metricas.Snapshot) string {
	if d == nil || d.V == "" {
		return "instalado"
	}
	return d.V
}

// DiscosRelevantes filtra os filesystems que nao vale avaliar por espaco.
func DiscosRelevantes(discos []metricas.Disco, lim Limiares) []metricas.Disco {
	ign := make(map[string]bool, len(lim.IgnorarMounts))
	for _, m := range lim.IgnorarMounts {
		ign[m] = true
	}
	out := make([]metricas.Disco, 0, len(discos))
	for _, d := range discos {
		if !ign[d.Mount] {
			out = append(out, d)
		}
	}
	return out
}

// NivelTemp avalia a temperatura da CPU contra o critico do proprio sensor.
func NivelTemp(c, crit *float64, lim Limiares) int {
	if c == nil {
		return OK
	}
	aviso, critico := lim.TempFixa.Aviso, lim.TempFixa.Critico
	if crit != nil && *crit > 0 {
		aviso = *crit * lim.TempFrac.Aviso
		critico = *crit * lim.TempFrac.Critico
	}
	return faixa(c, aviso, critico)
}

func faixa(v *float64, aviso, critico float64) int {
	if v == nil {
		return OK
	}
	switch {
	case *v >= critico:
		return Critico
	case *v >= aviso:
		return Aviso
	}
	return OK
}

func faixaV(v float64, aviso, critico float64) int { return faixa(&v, aviso, critico) }

// Avaliar devolve (nivel, alertas) de um host.
//
// E a unica definicao de "isso merece sua atencao" no projeto. Tanto a cor do
// icone da bandeja quanto as linhas da janela saem daqui - e e por isso que
// nao pode existir uma segunda copia dessa logica em lugar nenhum.
func Avaliar(e Estado, lim Limiares) (int, []string) {
	if e.Erro != "" || e.Dados == nil {
		msg := e.Erro
		if msg == "" {
			msg = "sem dados"
		}
		return Offline, []string{msg}
	}

	d := e.Dados
	nivel := OK
	var alertas []string

	marcar := func(n int, msg string) {
		if n > nivel {
			nivel = n
		}
		if msg != "" && n >= Aviso {
			alertas = append(alertas, msg)
		}
	}

	n := NivelTemp(d.CPUTemp, d.CPUCrit, lim)
	if n >= Aviso && d.CPUTemp != nil {
		marcar(n, fmt.Sprintf("CPU em %.0fC", *d.CPUTemp))
	} else {
		marcar(n, "")
	}

	if n := faixa(d.Mem.Percent, lim.RAM.Aviso, lim.RAM.Critico); n >= Aviso {
		marcar(n, fmt.Sprintf("RAM em %.0f%%", *d.Mem.Percent))
	} else {
		marcar(n, "")
	}

	// Particoes de tamanho fixo ficam de fora: ver Limiares.IgnorarMounts.
	for _, disco := range DiscosRelevantes(d.Discos, lim) {
		if n := faixaV(disco.Percent, lim.Disco.Aviso, lim.Disco.Critico); n >= Aviso {
			marcar(n, fmt.Sprintf("disco %s em %.0f%%", disco.Mount, disco.Percent))
		} else {
			marcar(n, "")
		}
		// Inode esgotado quebra igual a disco cheio, e o df -h nao mostra.
		if n := faixa(disco.InodesPercent, lim.Inodes.Aviso, lim.Inodes.Critico); n >= Aviso {
			marcar(n, fmt.Sprintf("inodes de %s em %.0f%%", disco.Mount, *disco.InodesPercent))
		} else {
			marcar(n, "")
		}
	}

	for _, tp := range d.Thinpools {
		if n := faixaV(tp.DataPercent, lim.Thinpool.Aviso, lim.Thinpool.Critico); n >= Aviso {
			marcar(n, fmt.Sprintf("thin pool %s em %.0f%%", tp.Nome, tp.DataPercent))
		} else {
			marcar(n, "")
		}
		if n := faixaV(tp.MetaPercent, lim.Thinpool.Aviso, lim.Thinpool.Critico); n >= Aviso {
			marcar(n, fmt.Sprintf("metadata de %s em %.0f%%", tp.Nome, tp.MetaPercent))
		} else {
			marcar(n, "")
		}
	}

	for _, r := range d.Raid {
		if r.Degradado != nil && *r.Degradado {
			marcar(Critico, fmt.Sprintf("RAID %s degradado (%s)", r.Nome, r.Discos))
		}
	}

	smartLegado := false
	for _, b := range d.Blocos {
		dev := b.Dev
		if dev == "" {
			dev = "?"
		}
		// NVMe faz throttling termico por volta de 70C; acima disso o disco
		// fica lento e a vida util encurta.
		if n := faixa(b.TempC, lim.TempDisco.Aviso, lim.TempDisco.Critico); n >= Aviso {
			marcar(n, fmt.Sprintf("disco %s em %.0fC", dev, *b.TempC))
		} else {
			marcar(n, "")
		}
		if b.Smart == nil {
			continue
		}
		if v, ok := VereditoSmart(b, lim); ok {
			alertasSmart(dev, v, marcar)
			continue
		}
		// Agente anterior a v5.1: sem tabela de atributos, sobra o resumo.
		s := b.Smart
		if s.Saude == "falha" {
			marcar(Critico, fmt.Sprintf("SMART reprovou o disco %s", dev))
		}
		if n := faixa(s.DesgastePercent, lim.Desgaste.Aviso, lim.Desgaste.Critico); n >= Aviso {
			marcar(n, fmt.Sprintf("disco %s com %.0f%% de vida consumida",
				dev, *s.DesgastePercent))
			smartLegado = true
		} else {
			marcar(n, "")
		}
		// Um setor realocado ja significa midia se degradando: nao espera piorar.
		if s.Realocados != nil && *s.Realocados >= lim.RealocadosAviso {
			marcar(Aviso, fmt.Sprintf("disco %s com %d setores realocados",
				dev, *s.Realocados))
			smartLegado = true
		}
	}

	// Sem isto, quem atualiza so o cliente ve as MESMAS reclamacoes de antes e
	// conclui que as regras novas nao funcionam. Elas nao funcionam mesmo: a
	// tabela de atributos e a variacao no tempo vem do agente, e um agente
	// antigo nao tem o que mandar. Uma linha por host, e nao por disco.
	if smartLegado {
		marcar(Aviso, fmt.Sprintf("as regras de disco estao no modo antigo: "+
			"o agente %s nao envia a tabela SMART - atualize o agente do host",
			versaoAgente(d)))
	}

	// PSI 'some' alto significa que ha tarefa parada esperando o recurso - e o
	// sinal que aparece antes de o host ficar visivelmente lento.
	for _, r := range []struct{ chave, rotulo string }{
		{"io", "IO"}, {"cpu", "CPU"}, {"memory", "memoria"},
	} {
		rec, ok := d.Pressure[r.chave]
		if !ok {
			continue
		}
		v, ok := rec["some_avg60"]
		if !ok {
			continue
		}
		if n := faixaV(v, lim.PSI.Aviso, lim.PSI.Critico); n >= Aviso {
			marcar(n, fmt.Sprintf("pressao de %s em %.0f%%", r.rotulo, v))
		} else {
			marcar(n, "")
		}
	}

	// Dado velho: o agente responde, mas a coleta dele parou.
	intervalo := d.IntervaloS
	if intervalo <= 0 {
		intervalo = 5
	}
	limite := lim.IdadeFator * intervalo
	if limite < 30 {
		limite = 30
	}
	if d.IdadeS > limite {
		marcar(Aviso, fmt.Sprintf("coleta parada ha %.0fs", d.IdadeS))
	}

	return nivel, alertas
}

// NivelDo e o atalho para quem so quer a cor.
func NivelDo(e Estado, lim Limiares) int {
	n, _ := Avaliar(e, lim)
	return n
}
