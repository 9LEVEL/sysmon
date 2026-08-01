package nucleo

// A ponte entre o que o agente manda pelo fio e as regras de internal/smart.
//
// Mora aqui, e nao dentro de internal/smart, de proposito: aquele pacote e
// funcao pura sobre uma leitura ja normalizada e nao sabe que existe rede,
// JSON ou agente. Manter a conversao fora dele e o que permite testar a
// especificacao inteira sem disco nenhum.

import (
	"fmt"

	"sysmon/internal/metricas"
	"sysmon/internal/smart"
)

// LeituraSmart converte um disco recebido do agente.
//
// O segundo retorno e falso quando o agente e antigo demais para as regras
// novas. Ate a v5.0 o campo Smart trazia so o resumo - sem tabela de
// atributos, sem serial, sem coleta_ok - e avaliar aquilo com as regras da
// v5.1 diria "coleta falhou" para todo disco de toda frota que ainda nao
// atualizou o agente. Nesse caso o chamador cai nas checagens antigas, que
// continuam valendo para o que elas sabem ver.
func LeituraSmart(b metricas.Bloco) (smart.Leitura, bool) {
	s := b.Smart
	if s == nil {
		return smart.Leitura{}, false
	}
	// Um agente >= 5.1 sempre afirma um dos dois: a coleta deu certo, ou deu
	// errado e aqui esta o motivo. Nenhum dos dois presente = agente antigo.
	if !s.ColetaOK && s.ErroColeta == "" {
		return smart.Leitura{}, false
	}

	l := smart.Leitura{
		Dev:                b.Dev,
		Tipo:               b.Tipo,
		Serial:             s.Serial,
		Modelo:             b.Modelo,
		Familia:            s.Familia,
		ColetaOK:           s.ColetaOK,
		ErroColeta:         s.ErroColeta,
		Saude:              s.Saude,
		TempC:              b.TempC,
		TempMaxC:           s.TempMaxC,
		Throttle:           s.Throttle,
		PercentualUsado:    s.DesgastePercent,
		DesligamentosSujos: s.DesligamentosSujos,
		CiclosEnergia:      s.CiclosEnergia,
	}
	l.Atributos = make([]smart.Atributo, 0, len(s.Atributos))
	for _, a := range s.Atributos {
		l.Atributos = append(l.Atributos, smart.Atributo{
			ID: a.ID, Nome: a.Nome, Valor: a.Valor, Pior: a.Pior,
			Limiar: a.Limiar, Cru: a.Cru,
			Delta24h: a.Delta24h, Delta7d: a.Delta7d, Delta30d: a.Delta30d,
			Base30d: a.Base30d, Amostras: a.Amostras,
		})
	}
	return l, true
}

// VereditoSmart avalia um disco, ou devolve false se nao houver base para
// isso.
func VereditoSmart(b metricas.Bloco, lim Limiares) (smart.Veredito, bool) {
	l, ok := LeituraSmart(b)
	if !ok {
		return smart.Veredito{}, false
	}
	return smart.Avaliar(l, lim.Smart), true
}

// NivelSmart traduz a severidade do pacote smart para a do resto do projeto.
//
// Info nao vira Aviso: a especificacao separa os dois justamente para que
// exista um degrau que se registra sem acordar ninguem, e apagar essa
// distincao aqui devolveria o ruido que ela existe para conter.
func NivelSmart(sev int) int {
	switch sev {
	case smart.Critico:
		return Critico
	case smart.Aviso:
		return Aviso
	}
	return OK
}

// alertasSmart transforma o veredito em linhas para o rodape.
//
// A categoria entra na frase porque e a informacao que mais economiza tempo:
// "interconexao" manda olhar o cabo, "host" manda olhar a energia, e so
// "dispositivo" manda trocar o disco. Sem isso, todo achado vira "troque o
// disco" - inclusive os que nao seriam resolvidos por trocar o disco.
func alertasSmart(dev string, v smart.Veredito,
	marcar func(nivel int, chave, valor, texto string)) {
	for _, a := range v.Achados {
		n := NivelSmart(a.Severidade)
		if n == OK {
			continue
		}
		// A temperatura ATUAL ja foi avaliada logo acima, contra lim.TempDisco,
		// que e o que o usuario configura no config.json. Deixar a regra do
		// pacote smart tambem falar dela produziria duas linhas dizendo a mesma
		// coisa com numeros diferentes. O maximo historico e o throttling nao
		// tem equivalente antigo, entao esses passam.
		if a.Regra == "temp" {
			continue
		}
		// A chave e a REGRA, nao a mensagem: "smart:sda:host:desligamento_sujo"
		// continua a mesma se um dia a frase for reescrita. O valor sao os
		// numeros da frase curta, que mudam quando e so quando a situacao muda.
		marcar(n, "smart:"+dev+":"+a.Regra, soNumeros(a.Curto()),
			fmt.Sprintf("disco %s%s: %s", dev, sufixoCategoria(a.Categoria),
				a.Mensagem))
	}
}

func sufixoCategoria(c string) string {
	switch c {
	case smart.Interconexao:
		return " (cabo/porta)"
	case smart.Host:
		return " (energia do host)"
	}
	return ""
}
