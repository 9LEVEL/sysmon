package janela

import (
	"strings"
	"testing"
	"time"

	"sysmon/internal/atualizar"
)

func TestSituacaoDaVersao(t *testing.T) {
	agora := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	base := atualizar.Estado{Suportado: true, Versao: "5.8.0"}

	casos := []struct {
		nome     string
		muda     func(*atualizar.Estado)
		titulo   string
		instalar bool
		repetir  bool
	}{
		{"mais recente", func(e *atualizar.Estado) { e.Conferido = agora.Add(-3 * time.Minute) },
			"voce esta na versao mais recente: 5.8.0", false, true},
		{"procurando", func(e *atualizar.Estado) { e.Checando = true },
			"procurando versao nova...", false, false},
		{"baixando", func(e *atualizar.Estado) { e.Checando, e.Disponivel = true, "v5.9.0" },
			"baixando a versao v5.9.0...", false, false},
		{"pronta", func(e *atualizar.Estado) { e.Pronta, e.Disponivel = true, "v5.9.0" },
			"versao v5.9.0 pronta para instalar", true, false},
		{"erro", func(e *atualizar.Estado) { e.Erro = "sem rede" },
			"nao consegui verificar", false, true},
		{"sem binario", func(e *atualizar.Estado) { e.Suportado = false },
			"atualizacao automatica indisponivel", false, false},
	}
	for _, c := range casos {
		e := base
		c.muda(&e)
		s := situacaoDe(e, agora)
		if s.titulo != c.titulo || s.instalar != c.instalar || s.repetir != c.repetir {
			t.Errorf("%s: %+v", c.nome, s)
		}
	}

	e := base
	e.Conferido = agora.Add(-3 * time.Minute)
	if s := situacaoDe(e, agora); !strings.Contains(s.detalhe, "ha 3 min") {
		t.Errorf("sem a data da conferencia: %q", s.detalhe)
	}
}

func TestBotaoDeVersaoDizQueEstaNaMaisRecente(t *testing.T) {
	// O pedido: clicar e ver, na tela e sem sumir, que ja esta na ultima.
	// Antes a resposta durava um quadro no rodape e o botao parecia morto.
	b := novaBancada(t)
	anterior := atualizar.API
	atualizar.API = githubFalso(t, "v5.8.0", binPlausivel())
	defer func() { atualizar.API = anterior }()
	at := atualizar.Novo("5.8.0", 0)
	b.j.Atual = at

	b.clique(iconeX(b.tam.X, 6), 19)
	if b.j.dialogo != dlgVersao {
		t.Fatalf("o botao nao abriu o dialogo: %v", b.j.dialogo)
	}
	prazo := time.Now().Add(5 * time.Second)
	for time.Now().Before(prazo) && (at.Estado().Checando || at.Estado().Conferido.IsZero()) {
		time.Sleep(20 * time.Millisecond)
	}
	s := situacaoDe(at.Estado(), time.Now())
	if s.titulo != "voce esta na versao mais recente: 5.8.0" {
		t.Fatalf("situacao = %+v (estado %+v)", s, at.Estado())
	}
	// Varios quadros depois, o dialogo continua la.
	for i := 0; i < 5; i++ {
		b.quadro()
	}
	if b.j.dialogo != dlgVersao {
		t.Fatal("o dialogo sumiu sozinho")
	}
}
