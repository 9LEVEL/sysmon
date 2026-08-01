package janela

import (
	"fmt"
	"image/color"
	"sort"
	"strings"

	"sysmon-cliente/internal/metricas"
	"sysmon-cliente/internal/nucleo"
)

// Linha e uma linha da arvore, ja pronta para desenhar.
//
// Montar as linhas e uma funcao pura da leitura: sem isso, testar "o disco
// aparece com a cor certa" exigiria abrir janela. Assim o desenho fica burro
// e a decisao fica testavel.
type Linha struct {
	Recuo   int
	Nome    string
	Detalhe string
	Valor   string
	Cor     color.NRGBA
	Pct     float64 // <0 = sem barra
	Serie   []float64
	Secao   bool
	Host    bool
}

// Campos que podem ser escondidos pela tela de exibicao.
type Visiveis map[string]bool

func (v Visiveis) ver(chaves ...string) bool {
	for _, c := range chaves {
		if v[c] {
			return false
		}
	}
	return true
}

// Montar transforma o estado da frota nas linhas da tela.
func Montar(leituras []nucleo.LeituraHost, lim nucleo.Limiares, oculto Visiveis,
	serie func(host, medida string) []float64) []Linha {
	var out []Linha
	for _, l := range leituras {
		nivel, _ := nucleo.Avaliar(l.Estado, lim)
		d := l.Estado.Dados

		cabecalho := Linha{Host: true, Nome: strings.ToUpper(l.Host.Nome),
			Cor: CorNivel(nivel), Pct: -1}
		if d == nil {
			cabecalho.Valor = "OFFLINE"
			cabecalho.Detalhe = l.Estado.Erro
			if cabecalho.Detalhe == "" {
				cabecalho.Detalhe = "sem dados"
			}
			out = append(out, cabecalho)
			continue
		}

		// As tres medidas que se olha primeiro, na propria linha do host.
		if oculto.ver("sec:RESUMO") {
			var resumo, detalhe []string
			if oculto.ver("r:temp") && d.CPUTemp != nil {
				resumo = append(resumo, nucleo.Temp(d.CPUTemp))
			}
			if oculto.ver("r:cpu") && d.CPUPercent != nil {
				resumo = append(resumo, "cpu "+nucleo.Pct(d.CPUPercent))
			}
			if oculto.ver("r:ram") && d.Mem.Percent != nil {
				resumo = append(resumo, "ram "+nucleo.Pct(d.Mem.Percent))
			}
			if oculto.ver("r:gb") && d.Mem.Total > 0 {
				detalhe = append(detalhe, fmt.Sprintf("%s de %s",
					nucleo.BytesV(d.Mem.Usado), nucleo.BytesV(d.Mem.Total)))
			}
			if oculto.ver("r:so") && d.SO.Nome != "" {
				detalhe = append(detalhe, d.SO.Nome)
			}
			cabecalho.Valor = strings.Join(resumo, " · ")
			cabecalho.Detalhe = strings.Join(detalhe, " · ")
		}
		out = append(out, cabecalho)

		secao := func(nome string) { out = append(out, Linha{Secao: true, Recuo: 1, Nome: nome, Pct: -1, Cor: Fraco}) }
		medida := func(nome, det string, pct *float64, aviso, crit float64, s []float64) {
			v := -1.0
			cor := Texto
			if pct != nil {
				v = *pct
				cor = CorMagnitude(v, aviso, crit)
			}
			out = append(out, Linha{Recuo: 2, Nome: nome, Detalhe: det,
				Valor: nucleo.Pct(pct), Cor: cor, Pct: v, Serie: s})
		}
		linha := func(nome, valor, det string, cor color.NRGBA) {
			out = append(out, Linha{Recuo: 2, Nome: nome, Valor: valor,
				Detalhe: det, Cor: cor, Pct: -1})
		}

		// ---- desempenho
		if oculto.ver("sec:DESEMPENHO") {
			secao("DESEMPENHO")
			if oculto.ver("p:cpu") {
				det := fmt.Sprintf("%d nucleos", d.CPUs)
				if m := limparModelo(d.CPUModelo); m != "" {
					det += " · " + m
				}
				medida("cpu", det, d.CPUPercent, 80, 95, serie(l.Host.Nome, "cpu"))
			}
			if oculto.ver("p:mem") {
				medida("memoria", fmt.Sprintf("%s / %s",
					nucleo.BytesV(d.Mem.Usado), nucleo.BytesV(d.Mem.Total)),
					d.Mem.Percent, lim.RAM.Aviso, lim.RAM.Critico,
					serie(l.Host.Nome, "ram"))
			}
			if oculto.ver("p:swap") && d.Mem.SwapPercent != nil && *d.Mem.SwapPercent > 0 {
				medida("swap", nucleo.BytesV(d.Mem.SwapUsado), d.Mem.SwapPercent,
					50, 80, nil)
			}
			if oculto.ver("p:load") {
				linha("carga", fmt.Sprintf("%.2f", d.Load[0]),
					fmt.Sprintf("%.2f 5m · %.2f 15m", d.Load[1], d.Load[2]), Texto)
			}
			if oculto.ver("p:up") {
				linha("no ar", nucleo.Uptime(&d.UptimeS), "", Texto)
			}
		}

		// ---- temperatura
		if oculto.ver("sec:TEMPERATURA") && (len(d.Temps) > 0 || d.CPUTemp != nil) {
			secao("TEMPERATURA")
			if oculto.ver("t:cpu") && d.CPUTemp != nil {
				det := ""
				if d.CPUCrit != nil {
					det = "critico " + nucleo.Temp(d.CPUCrit)
				}
				linha("cpu", nucleo.Temp(d.CPUTemp), det,
					CorNivel(nucleo.NivelTemp(d.CPUTemp, d.CPUCrit, lim)))
			}
			if oculto.ver("t:todos") {
				for i, s := range d.Temps {
					if i >= 10 {
						break
					}
					c := s.C
					linha(corta(strings.ToLower(s.Label), 18), nucleo.Temp(&c),
						corta(s.Chip, 18),
						CorNivel(nucleo.NivelTemp(&c, s.Crit, lim)))
				}
			}
		}

		// ---- ventoinhas
		if oculto.ver("sec:VENTOINHAS", "v:todas") && len(d.Fans) > 0 {
			secao("VENTOINHAS")
			nomes := make([]string, 0, len(d.Fans))
			for n := range d.Fans {
				nomes = append(nomes, n)
			}
			// Mapa em Go nao tem ordem: sem ordenar, as linhas dancariam a
			// cada quadro.
			sort.Strings(nomes)
			for i, n := range nomes {
				if i >= 6 {
					break
				}
				curto := n
				if p := strings.LastIndex(n, "/"); p >= 0 {
					curto = n[p+1:]
				}
				linha(corta(strings.ToLower(curto), 18),
					fmt.Sprintf("%d rpm", d.Fans[n]), "", Texto)
			}
		}

		// ---- discos fisicos
		if oculto.ver("sec:DISCOS", "b:todos") && len(d.Blocos) > 0 {
			secao("DISCOS")
			for _, b := range d.Blocos {
				var det []string
				if b.Smart != nil {
					if b.Smart.Saude == "falha" {
						det = append(det, "SMART REPROVOU")
					}
					if b.Smart.DesgastePercent != nil {
						det = append(det, fmt.Sprintf("%.0f%% usado",
							*b.Smart.DesgastePercent))
					}
				}
				det = append(det, nucleo.BytesV(b.Tamanho), corta(b.Modelo, 22))
				cor := CorNivel(nucleo.OK)
				if b.Smart != nil && b.Smart.Saude == "falha" {
					cor = Vermelho
				} else if b.TempC != nil {
					cor = CorMagnitude(*b.TempC, lim.TempDisco.Aviso,
						lim.TempDisco.Critico)
				}
				linha(b.Dev, nucleo.Temp(b.TempC), juntar(det), cor)
			}
		}

		// ---- armazenamento
		discos := nucleo.DiscosRelevantes(d.Discos, lim)
		if !oculto.ver("a:fs") {
			discos = nil
		}
		tps := d.Thinpools
		if !oculto.ver("a:thin") {
			tps = nil
		}
		if oculto.ver("sec:ARMAZENAMENTO") && (len(discos) > 0 || len(tps) > 0) {
			secao("ARMAZENAMENTO")
			for _, x := range discos {
				p := x.Percent
				medida(corta(x.Mount, 22), fmt.Sprintf("%s / %s",
					nucleo.BytesV(x.Usado), nucleo.BytesV(x.Total)), &p,
					lim.Disco.Aviso, lim.Disco.Critico, nil)
			}
			for _, t := range tps {
				p := t.DataPercent
				m := t.MetaPercent
				medida(corta(t.Nome, 22), "metadata "+nucleo.Pct(&m), &p,
					lim.Thinpool.Aviso, lim.Thinpool.Critico, nil)
			}
		}

		// ---- rede
		if oculto.ver("sec:REDE", "n:todas") {
			var ativas []metricas.Net
			for _, n := range d.Net {
				if n.Up {
					ativas = append(ativas, n)
				}
			}
			if len(ativas) > 0 {
				secao("REDE")
				for _, n := range ativas {
					det := "↑" + nucleo.Bps(n.TXBps)
					if n.Mbps != nil {
						det += fmt.Sprintf(" · %d Mbit", *n.Mbps)
					}
					linha(corta(n.Iface, 18), "↓"+nucleo.Bps(n.RXBps), det, Texto)
				}
			}
		}
	}
	return out
}

func limparModelo(m string) string {
	m = strings.ReplaceAll(m, "(R)", "")
	m = strings.ReplaceAll(m, "(TM)", "")
	return corta(strings.TrimSpace(m), 30)
}

func corta(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func juntar(partes []string) string {
	var vivos []string
	for _, p := range partes {
		if p != "" && p != "—" {
			vivos = append(vivos, p)
		}
	}
	return strings.Join(vivos, " · ")
}
