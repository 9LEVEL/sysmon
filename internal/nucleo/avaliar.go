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
func Avaliar(e Estado, lim Limiares) (int, []Alerta) {
	if e.Erro != "" || e.Dados == nil {
		msg := e.Erro
		if msg == "" {
			msg = "sem dados"
		}
		// Sem valor: host fora do ar nao e reconhecivel. Ver Alerta.Reconhecivel.
		return Offline, []Alerta{{Chave: "offline", Texto: msg, Nivel: Offline}}
	}

	d := e.Dados
	var alertas []Alerta

	// marcar so registra o que passa de Aviso. As chamadas com nivel abaixo
	// disso, que existiam so para "subir o nivel", eram todas no-op: faixa()
	// devolve OK, Aviso ou Critico, e OK nunca sobe nada.
	marcar := func(n int, chave, valor, texto string) {
		if n >= Aviso && texto != "" {
			alertas = append(alertas, Alerta{Chave: chave, Valor: valor,
				Texto: texto, Nivel: n})
		}
	}
	// pct e o valor de uma medida continua, arredondado ao ponto percentual.
	//
	// Sem arredondar, um disco em 96,4% viraria valor novo a cada coleta e o
	// reconhecimento nunca duraria um minuto. Com o arredondamento ele dura
	// ate o disco chegar a 97 - que e quando piorou de verdade.
	pct := func(v float64) string { return fmt.Sprintf("%.0f%%", v) }

	n := NivelTemp(d.CPUTemp, d.CPUCrit, lim)
	if n >= Aviso && d.CPUTemp != nil {
		marcar(n, "cpu:temp", "", fmt.Sprintf("CPU em %.0fC", *d.CPUTemp))
	}

	if n := faixa(d.Mem.Percent, lim.RAM.Aviso, lim.RAM.Critico); n >= Aviso {
		marcar(n, "ram", "", fmt.Sprintf("RAM em %.0f%%", *d.Mem.Percent))
	}

	// Particoes de tamanho fixo ficam de fora: ver Limiares.IgnorarMounts.
	for _, disco := range DiscosRelevantes(d.Discos, lim) {
		if n := faixaV(disco.Percent, lim.Disco.Aviso, lim.Disco.Critico); n >= Aviso {
			marcar(n, "fs:"+disco.Mount, pct(disco.Percent),
				fmt.Sprintf("disco %s em %.0f%%", disco.Mount, disco.Percent))
		}
		// Inode esgotado quebra igual a disco cheio, e o df -h nao mostra.
		if n := faixa(disco.InodesPercent, lim.Inodes.Aviso, lim.Inodes.Critico); n >= Aviso {
			marcar(n, "inodes:"+disco.Mount, pct(*disco.InodesPercent),
				fmt.Sprintf("inodes de %s em %.0f%%", disco.Mount, *disco.InodesPercent))
		}
	}

	for _, tp := range d.Thinpools {
		if n := faixaV(tp.DataPercent, lim.Thinpool.Aviso, lim.Thinpool.Critico); n >= Aviso {
			marcar(n, "thin:"+tp.Nome+":data", pct(tp.DataPercent),
				fmt.Sprintf("thin pool %s em %.0f%%", tp.Nome, tp.DataPercent))
		}
		if n := faixaV(tp.MetaPercent, lim.Thinpool.Aviso, lim.Thinpool.Critico); n >= Aviso {
			marcar(n, "thin:"+tp.Nome+":meta", pct(tp.MetaPercent),
				fmt.Sprintf("metadata de %s em %.0f%%", tp.Nome, tp.MetaPercent))
		}
	}

	for _, r := range d.Raid {
		if r.Degradado != nil && *r.Degradado {
			// O valor e o mapa de discos: [U_] reconhecido volta a alertar se
			// virar [__], que e um disco a menos.
			marcar(Critico, "raid:"+r.Nome, r.Discos,
				fmt.Sprintf("RAID %s degradado (%s)", r.Nome, r.Discos))
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
			marcar(n, "disco:"+dev+":temp", "",
				fmt.Sprintf("disco %s em %.0fC", dev, *b.TempC))
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
			marcar(Critico, "smart:"+dev+":autoteste", "falha",
				fmt.Sprintf("SMART reprovou o disco %s", dev))
		}
		if n := faixa(s.DesgastePercent, lim.Desgaste.Aviso, lim.Desgaste.Critico); n >= Aviso {
			marcar(n, "smart:"+dev+":desgaste", pct(*s.DesgastePercent),
				fmt.Sprintf("disco %s com %.0f%% de vida consumida",
					dev, *s.DesgastePercent))
			smartLegado = true
		}
		// Um setor realocado ja significa midia se degradando: nao espera piorar.
		if s.Realocados != nil && *s.Realocados >= lim.RealocadosAviso {
			marcar(Aviso, "smart:"+dev+":realocados", fmt.Sprint(*s.Realocados),
				fmt.Sprintf("disco %s com %d setores realocados",
					dev, *s.Realocados))
			smartLegado = true
		}
	}

	// Sem isto, quem atualiza so o cliente ve as MESMAS reclamacoes de antes e
	// conclui que as regras novas nao funcionam. Elas nao funcionam mesmo: a
	// tabela de atributos e a variacao no tempo vem do agente, e um agente
	// antigo nao tem o que mandar. Uma linha por host, e nao por disco.
	if smartLegado {
		marcar(Aviso, "smart:agente_antigo", versaoAgente(d),
			fmt.Sprintf("as regras de disco estao no modo antigo: "+
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
			marcar(n, "psi:"+r.chave, "",
				fmt.Sprintf("pressao de %s em %.0f%%", r.rotulo, v))
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
		marcar(Aviso, "coleta:parada", "",
			fmt.Sprintf("coleta parada ha %.0fs", d.IdadeS))
	}

	// O que o usuario ja aceitou sai daqui, e o nivel e recalculado do que
	// sobrou - e isso que faz a cor voltar ao normal sem nenhum caminho
	// paralelo para manter em sincronia.
	alertas = lim.Reconhecidos.Filtrar(alertas)
	return NivelDe(alertas), alertas
}

// AvaliarBruto ignora os reconhecimentos.
//
// E o que a tela de alertas usa para listar o que PODERIA estar alertando,
// incluindo o que ja foi aceito - senao nao haveria como revogar uma aceitacao
// olhando para ela.
func AvaliarBruto(e Estado, lim Limiares) (int, []Alerta) {
	sem := lim
	sem.Reconhecidos = nil
	return Avaliar(e, sem)
}

// NivelDo e o atalho para quem so quer a cor.
func NivelDo(e Estado, lim Limiares) int {
	n, _ := Avaliar(e, lim)
	return n
}
