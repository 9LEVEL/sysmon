package nucleo

import "fmt"

// Formatacao compartilhada entre as telas.
//
// Vive junto da avaliacao pelo mesmo motivo que ela: enquanto cada tela
// formatava do seu jeito, o mesmo host aparecia como "9.8G" na janela e
// "9G" no terminal, e a diferenca parecia dado divergente.

// Bytes formata em unidade legivel. Nil vira travessao, nunca "0B" - a
// diferenca entre "nao medido" e "zero" importa.
func Bytes(n *int64) string {
	if n == nil {
		return "—"
	}
	v := float64(*n)
	for _, u := range []string{"B", "K", "M", "G", "T"} {
		if v < 1024 || u == "T" {
			// Uma casa abaixo de 100. Sem ela, um disco de 480G aparecia
			// como "0G" perto do teto da unidade anterior, e 14,9G de RAM
			// virava "15G" - resolucao pior que a do df -h, que e a
			// referencia que todo mundo ja tem na cabeca.
			if u != "B" && v < 100 {
				return fmt.Sprintf("%.1f%s", v, u)
			}
			return fmt.Sprintf("%.0f%s", v, u)
		}
		v /= 1024
	}
	return fmt.Sprintf("%.1fT", v)
}

// BytesV e a versao para valores nao opcionais.
func BytesV(n int64) string { return Bytes(&n) }

// Bps formata taxa de rede.
func Bps(n *float64) string {
	if n == nil {
		return "—"
	}
	v := *n
	for _, u := range []string{"B/s", "K/s", "M/s", "G/s"} {
		if v < 1024 || u == "G/s" {
			return fmt.Sprintf("%.0f%s", v, u)
		}
		v /= 1024
	}
	return fmt.Sprintf("%.0fG/s", v)
}

// Uptime encurta para a maior unidade que ainda informa.
func Uptime(s *int64) string {
	if s == nil {
		return "—"
	}
	seg := *s
	d, h := seg/86400, (seg%86400)/3600
	if d > 0 {
		return fmt.Sprintf("%dd%dh", d, h)
	}
	if h > 0 {
		return fmt.Sprintf("%dh%dm", h, (seg%3600)/60)
	}
	return fmt.Sprintf("%dm", seg/60)
}

// Pct formata percentual. Nil vira travessao.
func Pct(v *float64) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", *v)
}

// Temp formata temperatura em Celsius.
func Temp(v *float64) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%.0fC", *v)
}
