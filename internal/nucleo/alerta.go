// Alerta e uma condicao que merece atencao, com identidade e valor.
//
// Ate a v5.2 um alerta era uma string solta. Isso bastava para mostrar na
// tela e para nada mais: nao dava para dizer "eu ja vi este e concordo" sem
// dizer junto "e me avise de novo se piorar", porque nao havia como comparar
// o alerta de agora com o de ontem.
//
// Sao as duas informacoes que faltavam:
//
//   Chave  identifica a condicao e nao muda com a redacao da frase
//   Valor  e o que a disparou, no formato mais estavel que der - e o que
//          decide se um reconhecimento ainda vale

package nucleo

import (
	"sort"
	"strings"
)

type Alerta struct {
	Host  string // preenchido por Frota.Alertas
	Chave string
	Valor string
	Texto string
	Nivel int
}

// Reconhecivel diz se faz sentido oferecer "eu concordo, nao me avise mais".
//
// Alerta sem valor nao e reconhecivel, e sao dois grupos:
//
//   - Temperatura, RAM, CPU e pressao: sobem e descem o tempo todo. Aceitar
//     "CPU em 82%" congelaria um numero que ja mudou no ciclo seguinte. Para
//     esses existe o ajuste de limiar, que e a resposta certa.
//   - Host fora do ar e coleta parada: sao estados transitorios. Silenciar
//     um agente morto e o unico jeito de esta ferramenta mentir por omissao.
func (a Alerta) Reconhecivel() bool { return a.Valor != "" }

// Reconhecido e a decisao do usuario sobre um alerta.
type Reconhecido struct {
	Valor  string  `json:"valor"`
	Quando float64 `json:"quando"`
	Texto  string  `json:"texto,omitempty"` // como estava quando foi aceito
}

// Reconhecimentos guarda o que ja foi aceito, por chave.
type Reconhecimentos map[string]Reconhecido

// Cobre diz se este reconhecimento silencia o alerta.
//
// Exige valor IGUAL, e nao "menor ou igual". Um contador que subiu de 89 para
// 90 e um evento novo - foi por isso que o usuario pediu o recurso -, e um
// que desceu significa que a serie foi reiniciada, o que tambem merece ser
// visto de novo. Igualdade e a unica comparacao que acerta os dois casos sem
// precisar saber se o valor e numero, porcentagem ou mapa de RAID.
func (r Reconhecimentos) Cobre(a Alerta) bool {
	if !a.Reconhecivel() {
		return false
	}
	rec, ok := r[a.Chave]
	return ok && rec.Valor == a.Valor
}

// Filtrar remove os alertas ja aceitos.
func (r Reconhecimentos) Filtrar(alertas []Alerta) []Alerta {
	if len(r) == 0 || len(alertas) == 0 {
		return alertas
	}
	out := make([]Alerta, 0, len(alertas))
	for _, a := range alertas {
		if !r.Cobre(a) {
			out = append(out, a)
		}
	}
	return out
}

// Chaves lista o que esta reconhecido, em ordem estavel.
func (r Reconhecimentos) Chaves() []string {
	out := make([]string, 0, len(r))
	for k := range r {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// NivelDe e o pior nivel de uma lista de alertas.
//
// A cor sai daqui, e nao de um nivel calculado antes: e o que faz a interface
// voltar ao normal assim que o ultimo alerta e aceito, sem nenhum caminho
// paralelo para manter em sincronia.
func NivelDe(alertas []Alerta) int {
	pior := OK
	for _, a := range alertas {
		if a.Nivel > pior {
			pior = a.Nivel
		}
	}
	return pior
}

// Textos extrai as frases, para quem so quer mostrar.
func Textos(alertas []Alerta) []string {
	out := make([]string, 0, len(alertas))
	for _, a := range alertas {
		out = append(out, a.Texto)
	}
	return out
}

// soNumeros extrai os numeros de uma frase, na ordem, separados por barra.
//
// E o "valor" dos alertas cuja unica fonte estavel e a propria mensagem - os
// achados de SMART, por exemplo, cuja frase ja carrega as contagens. Usar a
// frase inteira funcionaria, mas ela mudaria numa versao que so reescrevesse
// o texto, e o reconhecimento de todo mundo seria perdido por nada. Os
// numeros so mudam quando a situacao muda, que e exatamente a regra.
func soNumeros(s string) string {
	var partes []string
	atual := strings.Builder{}
	for _, r := range s {
		if r >= '0' && r <= '9' {
			atual.WriteRune(r)
			continue
		}
		if atual.Len() > 0 {
			partes = append(partes, atual.String())
			atual.Reset()
		}
	}
	if atual.Len() > 0 {
		partes = append(partes, atual.String())
	}
	return strings.Join(partes, "/")
}
