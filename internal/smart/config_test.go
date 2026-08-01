package smart

import "testing"

func TestMexerNumCampoNaoApagaOsIrmaos(t *testing.T) {
	// {"smart":{"temperature":{"ssd":{"warn":55}}}} no config.json quer mudar
	// um numero. Herdar por sub-arvore deixaria o usuario sem os limiares de
	// HDD e sem o resto da faixa de SSD, em silencio.
	c := Config{}
	c.Temperatura.SSD.Aviso = 55
	c = c.ComPadroes()

	if c.Temperatura.SSD.Aviso != 55 {
		t.Errorf("perdeu o valor escolhido: %v", c.Temperatura.SSD.Aviso)
	}
	p := Padrao()
	if c.Temperatura.SSD.Critico != p.Temperatura.SSD.Critico {
		t.Errorf("ssd critico = %v, queria %v", c.Temperatura.SSD.Critico,
			p.Temperatura.SSD.Critico)
	}
	if c.Temperatura.HDD.Critico != p.Temperatura.HDD.Critico {
		t.Errorf("hdd zerou: %+v", c.Temperatura.HDD)
	}
	if c.Temperatura.OffsetMaxima != p.Temperatura.OffsetMaxima {
		t.Errorf("offset = %d, queria %d", c.Temperatura.OffsetMaxima,
			p.Temperatura.OffsetMaxima)
	}
	if c.Reserva.OKMin != p.Reserva.OKMin {
		t.Errorf("mexer na temperatura zerou a reserva: %+v", c.Reserva)
	}
}

func TestLimiarImediatoHerdaChaveAChave(t *testing.T) {
	// Afrouxar command_timeout nao pode desligar a regra de setor pendente.
	c := Config{Imediatos: map[string]Par{"command_timeout": {Aviso: 50}}}
	c = c.ComPadroes()

	if got := c.Imediatos["command_timeout"].Aviso; got != 50 {
		t.Errorf("aviso = %d, queria 50", got)
	}
	p := Padrao()
	if got := c.Imediatos["command_timeout"].Critico; got != p.Imediatos["command_timeout"].Critico {
		t.Errorf("critico da mesma chave sumiu: %d", got)
	}
	if got := c.Imediatos["current_pending_sector"].Aviso; got == 0 {
		t.Error("setor pendente parou de disparar")
	}
}

func TestPadraoNaoEMutavelPeloChamador(t *testing.T) {
	// Padrao() devolve mapas; se ComPadroes escrevesse neles, o primeiro
	// config personalizado contaminaria todos os discos avaliados depois.
	c := Config{Imediatos: map[string]Par{"command_timeout": {Aviso: 50}}}
	c.ComPadroes()
	if got := Padrao().Imediatos["command_timeout"].Aviso; got != 10 {
		t.Fatalf("o padrao global foi alterado: %d", got)
	}
}

func TestZeradoInteiroDaOsPadroesDaSpec(t *testing.T) {
	c := Config{}.ComPadroes()
	p := Padrao()
	if c.Crescimento.Critico7d != p.Crescimento.Critico7d ||
		c.SaudeHost.Critico != p.SaudeHost.Critico ||
		c.Desgaste.CiclosNAND["tlc"] != p.Desgaste.CiclosNAND["tlc"] {
		t.Fatalf("config zerada nao virou o padrao: %+v", c)
	}
}

func TestComPadroesNaoAlteraOQueRecebeu(t *testing.T) {
	// Avaliar e documentada como funcao pura. Um mapa compartilhado a faria
	// escrever na configuracao do chamador pelas costas.
	meu := map[string]Par{"command_timeout": {Aviso: 50}}
	Config{Imediatos: meu}.ComPadroes()
	if len(meu) != 1 {
		t.Fatalf("o mapa do chamador foi preenchido: %+v", meu)
	}
}
