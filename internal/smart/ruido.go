package smart

import "sync"

// Estabilizador aplica histerese e debounce por (serial, regra).
//
// Sem isto a ferramenta vira fonte de alerta ignorado em duas semanas, que e
// a unica forma de falha que nenhum threshold corrige.
//
// Histerese: subir de severidade exige N leituras seguidas concordando - um
// pico isolado nao promove nada. Descer exige o mesmo numero de leituras
// limpas, senao um valor parado em cima do limite fica piscando.
//
// Debounce: a mesma condicao no mesmo disco nao volta a notificar dentro da
// janela. Critico tem janela mais curta porque a acao esperada e mais
// urgente.
//
// O relogio entra por parametro: assim o comportamento no tempo e testavel
// sem esperar horas.
//
// CADENCIA. Isto foi calibrado para a amostragem HORARIA do smartctl, nao
// para o laco do cliente. "Duas leituras seguidas" a cada 3 s sao 6 segundos,
// que nao filtram nada; a cada hora sao duas horas, que e o que a
// especificacao quer dizer. Por isso nao esta ligado ao Avaliar do cliente:
// ali as regras sao funcao pura da leitura atual, e o que poderia oscilar - a
// temperatura instantanea - e avaliado pelo limiar do proprio config, com o
// historico do agente por tras. Ligar aqui e util no dia em que existir
// notificacao disparada pela camada SMART; ligar no laco de 3 s so daria uma
// falsa sensacao de estabilidade.
type Estabilizador struct {
	minParaSubir int
	debounce     map[int]float64

	mu        sync.Mutex
	nivel     map[chave]int
	candidato map[chave]candidatura
	ausencias map[chave]int
	avisado   map[chaveAviso]float64
}

type chave struct{ serial, regra string }
type chaveAviso struct {
	chave
	sev int
}
type candidatura struct{ sev, vezes int }

func NovoEstabilizador(cfg Config) *Estabilizador {
	cfg = cfg.ComPadroes()
	return &Estabilizador{
		minParaSubir: cfg.Ruido.LeiturasParaSubir,
		debounce: map[int]float64{
			Aviso:   float64(cfg.Ruido.DebounceAviso) * 3600,
			Critico: float64(cfg.Ruido.DebounceCritico) * 3600,
		},
		nivel:     map[chave]int{},
		candidato: map[chave]candidatura{},
		ausencias: map[chave]int{},
		avisado:   map[chaveAviso]float64{},
	}
}

// Estabilizar devolve os achados no nivel ja estavel.
func (e *Estabilizador) Estabilizar(serial string, achados []Achado) []Achado {
	e.mu.Lock()
	defer e.mu.Unlock()

	vistos := map[chave]Achado{}
	for _, a := range achados {
		vistos[chave{serial, a.Regra}] = a
	}

	var saida []Achado
	for k, a := range vistos {
		atual := e.nivel[k]
		if a.Severidade > atual {
			c := e.candidato[k]
			if c.sev == a.Severidade {
				c.vezes++
			} else {
				c = candidatura{a.Severidade, 1}
			}
			if c.vezes >= e.minParaSubir {
				e.nivel[k] = a.Severidade
				delete(e.candidato, k)
			} else {
				e.candidato[k] = c
				if atual == OK {
					continue // ainda nao confirmou: nao reporta
				}
				b := a
				b.Severidade = atual
				saida = append(saida, b)
				continue
			}
		} else {
			delete(e.candidato, k)
			e.nivel[k] = a.Severidade
		}
		saida = append(saida, a)
		delete(e.ausencias, k)
	}

	// Regra que parou de disparar. Descer exige tantas leituras limpas
	// quantas foram exigidas para subir - a especificacao pede uma margem
	// abaixo da fronteira, e como aqui ja chega severidade e nao o valor
	// bruto, a simetria de leituras cumpre o mesmo papel: impedir que um
	// valor parado em cima do limite fique piscando.
	//
	// O candidato tambem zera. Sem isso um pico isolado deixava meia
	// confirmacao guardada, e o pico seguinte - horas depois, com leitura
	// limpa no meio - promovia como se os dois fossem consecutivos.
	for k := range e.candidato {
		if k.serial == serial {
			if _, visto := vistos[k]; !visto {
				delete(e.candidato, k)
			}
		}
	}
	for k := range e.nivel {
		if k.serial != serial {
			continue
		}
		if _, visto := vistos[k]; visto {
			continue
		}
		n := e.ausencias[k] + 1
		if n >= e.minParaSubir {
			delete(e.nivel, k)
			delete(e.ausencias, k)
		} else {
			e.ausencias[k] = n
		}
	}
	return saida
}

// DeveNotificar diz se e hora de avisar de novo.
func (e *Estabilizador) DeveNotificar(serial string, a Achado, agora float64) bool {
	if a.Severidade < Aviso {
		return false // Info se registra, nao se notifica
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	k := chaveAviso{chave{serial, a.Regra}, a.Severidade}
	janela := e.debounce[a.Severidade]
	if ultimo, ok := e.avisado[k]; ok && agora-ultimo < janela {
		return false
	}
	e.avisado[k] = agora
	return true
}
