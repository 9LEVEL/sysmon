// Package terminal desenha a frota como tabela de texto.
//
// A tabela e a JANELA mostram exatamente a mesma coisa porque partem das
// mesmas linhas, montadas em internal/tela. Enquanto cada tela montava a sua,
// as duas podiam discordar sobre o mesmo host - e discordaram.
//
// Aqui nao ha framework de TUI, e e escolha. Os dois usos reais deste modo
// sao olhar rapido por SSH e `--once` dentro de cron ou script; um TUI de
// tela cheia atrapalha o segundo e depende de TERM bem configurado no
// primeiro. Texto simples funciona nos dois, e ainda pode ser canalizado.
package terminal

import (
	"fmt"
	"image/color"
	"io"
	"os"
	"strings"

	"sysmon/internal/nucleo"
	"sysmon/internal/tela"
)

// Opcoes de desenho.
type Opcoes struct {
	Cor     bool // false em pipe: sequencia ANSI em arquivo e lixo
	Largura int
}

// PadraoOpcoes decide cor e largura a partir do ambiente.
func PadraoOpcoes() Opcoes {
	return Opcoes{Cor: TemCor(), Largura: LarguraTerminal()}
}

// TemCor decide se vale emitir ANSI.
//
// Respeita NO_COLOR, que e a convencao que todo mundo ja tem, e desliga em
// pipe: ninguem quer sequencia de escape dentro de um arquivo de log.
func TemCor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	st, err := os.Stdout.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	return prepararANSI()
}

// ansi devolve o codigo de cor mais proximo do RGB, em 256 cores.
//
// Nao usa truecolor de proposito: 256 cores funciona em terminal antigo, em
// tmux sem configuracao e em PuTTY, que e onde este modo costuma ser usado.
func ansi(c color.NRGBA) string {
	// Escala de cinza tem faixa propria e fica melhor que o cubo.
	if abs(int(c.R)-int(c.G)) < 12 && abs(int(c.G)-int(c.B)) < 12 {
		n := (int(c.R) + int(c.G) + int(c.B)) / 3
		return fmt.Sprintf("\x1b[38;5;%dm", 232+n*23/255)
	}
	idx := 16 + 36*(int(c.R)*5/255) + 6*(int(c.G)*5/255) + int(c.B)*5/255
	return fmt.Sprintf("\x1b[38;5;%dm", idx)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

const reset = "\x1b[0m"

// Larguras fixas das colunas. O detalhe e a unica elastica: e o texto mais
// dispensavel, e por isso e ele quem cede quando o terminal aperta - o nome
// e o valor nunca somem.
const (
	colNome  = 26
	colValor = 12
	colBarra = 11 // 10 blocos mais o espaco antes
)

// Desenhar escreve a tabela inteira.
func Desenhar(w io.Writer, leituras []nucleo.LeituraHost, lim nucleo.Limiares,
	alertas []string, o Opcoes) {
	linhas := tela.Montar(leituras, lim, tela.Visiveis{},
		func(string, string) []float64 { return nil })

	pinta := func(s string, c color.NRGBA) string {
		if !o.Cor || s == "" {
			return s
		}
		return ansi(c) + s + reset
	}

	larg := o.Largura
	if larg < 60 {
		larg = 60
	}
	// Prioridade quando o terminal aperta: o VALOR nunca some, a barra some
	// antes do detalhe, e o detalhe encolhe por ultimo. A barra e enfeite -
	// o numero ao lado dela diz a mesma coisa com precisao.
	barraAqui := colBarra
	if larg < 90 {
		barraAqui = 0
	}
	colDet := larg - colNome - colValor - barraAqui - 2
	if colDet < 8 {
		colDet = 8
	}

	for _, l := range linhas {
		var linha string
		switch {
		case l.Host:
			fmt.Fprintln(w)
			// Na linha do host o valor e longo ("49C · cpu 23% · ram 63%"),
			// entao ele fica com a barra tambem.
			det := preencher(cortar(l.Detalhe, colDet), colDet)
			val := alinharDir(cortar(l.Valor, colValor+barraAqui), colValor+barraAqui)
			linha = pinta(preencher(cortar(l.Nome, colNome), colNome), l.Cor) +
				" " + pinta(det, tela.Titulo) + " " + pinta(val, l.Cor)
		case l.Secao:
			linha = "  " + pinta(l.Nome, tela.Fraco)
		default:
			nome := preencher("    "+cortar(l.Nome, colNome-4), colNome)
			det := preencher(cortar(l.Detalhe, colDet), colDet)
			// A barra volta a ser feita de caractere aqui: e o unico jeito
			// num terminal, e o motivo de a janela ter deixado de usar.
			barra := ""
			if barraAqui > 0 {
				barra = strings.Repeat(" ", barraAqui)
				if l.Pct >= 0 {
					barra = " " + barraTexto(l.Pct, barraAqui-1)
				}
			}
			linha = pinta(nome, l.Cor) + " " + det + pinta(barra, l.Cor) +
				" " + pinta(alinharDir(l.Valor, colValor), l.Cor)
		}
		// Rede de seguranca: qualquer erro de conta acima seria uma linha
		// quebrando no terminal do usuario, que e feio e desalinha tudo.
		fmt.Fprintln(w, truncarVisivel(linha, larg))
	}

	if len(alertas) > 0 {
		fmt.Fprintln(w)
		for _, a := range alertas {
			fmt.Fprintln(w, truncarVisivel(pinta("! "+a, tela.Vermelho), larg))
		}
	}
}

// truncarVisivel corta pela largura VISIVEL, mantendo as sequencias de cor.
func truncarVisivel(s string, larg int) string {
	if len([]rune(semANSI(s))) <= larg {
		return s
	}
	// Fecha a cor no fim so se houve cor: sem isto, uma tabela pedida sem
	// cor sairia com um "\x1b[0m" solto em cada linha cortada.
	temCor := strings.ContainsRune(s, 0x1b)
	var b strings.Builder
	visiveis, dentro := 0, false
	for _, r := range s {
		switch {
		case r == 0x1b:
			dentro = true
			b.WriteRune(r)
		case dentro:
			b.WriteRune(r)
			if r == 'm' {
				dentro = false
			}
		default:
			if visiveis >= larg-1 {
				b.WriteString("…")
				if temCor {
					b.WriteString(reset)
				}
				return b.String()
			}
			b.WriteRune(r)
			visiveis++
		}
	}
	return b.String()
}

// barraTexto e a barra de progresso em caractere.
func barraTexto(pct float64, larg int) string {
	n := int(pct/100*float64(larg) + 0.5)
	if n < 0 {
		n = 0
	}
	if n > larg {
		n = larg
	}
	return strings.Repeat("█", n) + strings.Repeat("·", larg-n)
}

func preencher(s string, n int) string {
	d := n - len([]rune(semANSI(s)))
	if d <= 0 {
		return s
	}
	return s + strings.Repeat(" ", d)
}

func alinharDir(s string, n int) string {
	d := n - len([]rune(s))
	if d <= 0 {
		return s
	}
	return strings.Repeat(" ", d) + s
}

func cortar(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// semANSI mede o texto sem contar as sequencias de escape, que ocupam bytes
// e nao ocupam coluna.
func semANSI(s string) string {
	var b strings.Builder
	dentro := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			dentro = true
		case dentro && r == 'm':
			dentro = false
		case !dentro:
			b.WriteRune(r)
		}
	}
	return b.String()
}
