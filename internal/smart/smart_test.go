package smart

import (
	"strings"
	"testing"
)

func i(v int) *int         { return &v }
func c(v int64) *int64     { return &v }
func f(v float64) *float64 { return &v }

// attr monta um atributo; nil em delta = sem historico.
func attr(nome string, cru int64, mod ...func(*Atributo)) Atributo {
	a := Atributo{Nome: nome, Cru: c(cru)}
	for _, m := range mod {
		m(&a)
	}
	return a
}

func id(n int) func(*Atributo)     { return func(a *Atributo) { a.ID = n } }
func valor(n int) func(*Atributo)  { return func(a *Atributo) { a.Valor = i(n) } }
func limiar(n int) func(*Atributo) { return func(a *Atributo) { a.Limiar = i(n) } }
func hist(d24, d7, d30, base int64, amostras int) func(*Atributo) {
	return func(a *Atributo) {
		a.Delta24h, a.Delta7d, a.Delta30d = c(d24), c(d7), c(d30)
		a.Base30d, a.Amostras = c(base), amostras
	}
}

// parado: historico dizendo que nao mexe ha um mes.
func parado(cru int64) func(*Atributo) { return hist(0, 0, 0, cru, 180) }

func disco(mod ...func(*Leitura)) Leitura {
	l := Leitura{Dev: "sda", Tipo: "ssd", Serial: "X1", Saude: "ok", ColetaOK: true}
	for _, m := range mod {
		m(&l)
	}
	return l
}

func attrs(a ...Atributo) func(*Leitura) {
	return func(l *Leitura) { l.Atributos = a }
}

func julgar(l Leitura) Veredito { return Avaliar(l, Config{}) }

// ------------------------------------------------------------- catalogo

func TestCasaPorNomeENaoPorID(t *testing.T) {
	// O mesmo ID 170 e Grown_Bad_Blocks num WD e reserva num Intel. Casar
	// por ID seria escolher um significado no cara ou coroa.
	wd := Indexar([]Atributo{attr("Grown_Bad_Blocks", 4, id(170))})
	intel := Indexar([]Atributo{attr("Available_Reservd_Space", 0, id(170), valor(98))})
	if _, ok := wd["blocos_crescidos"]; !ok {
		t.Error("WD: nao reconheceu blocos crescidos")
	}
	if _, ok := wd["reserva"]; ok {
		t.Error("WD: virou reserva por causa do ID")
	}
	if _, ok := intel["reserva"]; !ok {
		t.Error("Intel: nao reconheceu reserva")
	}
}

func TestNomeDesconhecidoEIgnoradoSemPalpite(t *testing.T) {
	p := Indexar([]Atributo{
		attr("Vendor_Specific_170", 12345, id(170)),
		attr("Unknown_Attribute", 7, id(169)),
	})
	if len(p) != 0 {
		t.Fatalf("inventou significado: %v", p)
	}
}

// -------------------------------------------------------------- reserva

func reservaCom(valorNorm int, lim *int) Veredito {
	a := attr("Available_Reservd_Space", 0, id(232), valor(valorNorm))
	if lim != nil {
		a.Limiar = lim
	}
	return julgar(disco(attrs(a)))
}

func TestReservaFaixas(t *testing.T) {
	casos := []struct {
		v    int
		quer int
	}{{98, OK}, {85, Info}, {60, Aviso}, {40, Critico}}
	for _, k := range casos {
		if got := reservaCom(k.v, nil).Dispositivo(); got != k.quer {
			t.Errorf("reserva %d = %d, queria %d", k.v, got, k.quer)
		}
	}
}

func TestLimiarDoFabricanteEAutoridade(t *testing.T) {
	// VALUE <= THRESH: o drive esta declarando falha iminente. Nao ha margem
	// de interpretacao nossa por cima disso.
	v := reservaCom(10, i(10))
	if v.Dispositivo() != Critico {
		t.Fatalf("nivel = %d", v.Dispositivo())
	}
	if !temRegra(v, "reserva:limiar_fabricante") {
		t.Fatalf("regra errada: %+v", v.Achados)
	}
}

func TestMargemDisparaAntesDoFabricante(t *testing.T) {
	// Dez pontos antes do limite, para haver janela de substituicao.
	if reservaCom(19, i(10)).Dispositivo() != Critico {
		t.Error("nao disparou na margem")
	}
	if reservaCom(95, i(10)).Dispositivo() != OK {
		t.Error("disparou longe do limite")
	}
}

func TestComReservaAContagemBrutaNaoDispara(t *testing.T) {
	// Principio 2: havendo reserva, ela e o sinal - 4 blocos com 98% de
	// reserva intacta e ruido.
	v := julgar(disco(attrs(
		attr("Available_Reservd_Space", 0, id(232), valor(98)),
		attr("Grown_Bad_Blocks", 4, id(170), parado(4)),
	)))
	if v.Dispositivo() != OK {
		t.Fatalf("nivel = %d: %+v", v.Dispositivo(), v.Achados)
	}
}

// ----------------------------------------------------------------- taxa

func taxaCom(cru int64, d24, d7, d30, base int64, amostras int) Veredito {
	return julgar(disco(attrs(attr("Reallocated_Sector_Ct", cru, id(5),
		hist(d24, d7, d30, base, amostras)))))
}

func TestTaxaFaixas(t *testing.T) {
	if got := taxaCom(10, 0, 1, 1, 9, 30).Dispositivo(); got != Info {
		t.Errorf("1 em 7d = %d, queria Info", got)
	}
	if got := taxaCom(10, 0, 3, 3, 7, 30).Dispositivo(); got != Aviso {
		t.Errorf("3 em 7d = %d, queria Aviso", got)
	}
	if got := taxaCom(10, 5, 5, 5, 5, 30).Dispositivo(); got != Critico {
		t.Errorf("5 em 24h = %d, queria Critico", got)
	}
	if got := taxaCom(10, 0, 10, 10, 0, 30).Dispositivo(); got != Critico {
		t.Errorf("10 em 7d = %d, queria Critico", got)
	}
}

func TestDobrarExigeBaseMinima(t *testing.T) {
	// 1 -> 2 nao pode virar alarme; 4 -> 8 pode.
	if got := taxaCom(2, 0, 1, 1, 1, 30).Dispositivo(); got >= Critico {
		t.Errorf("1->2 virou %d", got)
	}
	if got := taxaCom(8, 0, 4, 4, 4, 30).Dispositivo(); got != Critico {
		t.Errorf("4->8 = %d, queria Critico", got)
	}
}

func TestAceleracao(t *testing.T) {
	v := taxaCom(10, 0, 2, 3, 7, 30)
	if !temRegraContendo(v, "acelerou") {
		t.Fatalf("nao detectou aceleracao: %+v", v.Achados)
	}
}

// ------------------------------------------------------ escalacao direta

func umAtributo(nome string, cru int64, idn int) Veredito {
	return julgar(disco(attrs(attr(nome, cru, id(idn)))))
}

func TestPendentes(t *testing.T) {
	if got := umAtributo("Current_Pending_Sector", 2, 197).Dispositivo(); got != Aviso {
		t.Errorf("2 pendentes = %d", got)
	}
	if got := umAtributo("Current_Pending_Sector", 10, 197).Dispositivo(); got != Critico {
		t.Errorf("10 pendentes = %d", got)
	}
}

func TestUmSoJaECritico(t *testing.T) {
	for _, k := range []struct {
		nome string
		id   int
	}{{"Offline_Uncorrectable", 198}, {"Reported_Uncorrect", 187},
		{"End-to-End_Error", 184}} {
		if got := umAtributo(k.nome, 1, k.id).Dispositivo(); got != Critico {
			t.Errorf("%s = %d", k.nome, got)
		}
	}
}

func TestCommandTimeout(t *testing.T) {
	if got := umAtributo("Command_Timeout", 5, 188).Dispositivo(); got != OK {
		t.Errorf("5 = %d", got)
	}
	if got := umAtributo("Command_Timeout", 10, 188).Dispositivo(); got != Aviso {
		t.Errorf("10 = %d", got)
	}
	if got := umAtributo("Command_Timeout", 100, 188).Dispositivo(); got != Critico {
		t.Errorf("100 = %d", got)
	}
}

// ------------------------------------------------------------ interconexao

func TestCRCNaoContaminaODispositivo(t *testing.T) {
	v := julgar(disco(attrs(attr("UDMA_CRC_Error_Count", 12, id(199),
		hist(0, 3, 3, 9, 30)))))
	if v.Dispositivo() != OK {
		t.Errorf("dispositivo = %d, queria OK", v.Dispositivo())
	}
	if v.Interconexao() != Aviso {
		t.Errorf("interconexao = %d, queria Aviso", v.Interconexao())
	}
	if !strings.Contains(v.Achados[0].Mensagem, "cabo") {
		t.Errorf("mensagem nao aponta o cabo: %q", v.Achados[0].Mensagem)
	}
}

func TestCRCEstaticoESoRegistro(t *testing.T) {
	v := julgar(disco(attrs(attr("UDMA_CRC_Error_Count", 40, id(199), parado(40)))))
	if v.Interconexao() != Info || v.Dispositivo() != OK {
		t.Fatalf("interconexao=%d dispositivo=%d", v.Interconexao(), v.Dispositivo())
	}
}

// ---------------------------------------------------------------- desgaste

func TestDesgasteFaixas(t *testing.T) {
	for _, k := range []struct {
		pct  float64
		quer int
	}{{50, OK}, {75, Info}, {90, Aviso}, {96, Critico}} {
		v := julgar(disco(func(l *Leitura) { l.PercentualUsado = f(k.pct) }))
		if got := v.Dispositivo(); got != k.quer {
			t.Errorf("%.0f%% = %d, queria %d", k.pct, got, k.quer)
		}
	}
}

func TestIndicadorContaVidaRestante(t *testing.T) {
	v := julgar(disco(attrs(attr("Media_Wearout_Indicator", 0, id(233), valor(10)))))
	if v.Dispositivo() != Aviso {
		t.Fatalf("90%% consumido = %d", v.Dispositivo())
	}
}

func TestDesgasteAltoPedePlanejamentoNaoAcaoImediata(t *testing.T) {
	// CRITICO de desgaste e "planeje a troca"; CRITICO de setor pendente e
	// "aja hoje". Sao urgencias diferentes e o motivo distingue.
	v := julgar(disco(func(l *Leitura) { l.PercentualUsado = f(99) }))
	if v.Achados[0].Motivo != Planejar {
		t.Fatalf("motivo = %q", v.Achados[0].Motivo)
	}
}

func TestRawEmpacotadoEDescartado(t *testing.T) {
	// 0x1b2017001b20 nao e um inteiro decimal. Interpretar raw empacotado
	// sem tabela do fabricante e chute.
	v := julgar(disco(attrs(attr("Media_Wearout_Indicator", 0x1b2017001b20, id(233)))))
	if v.Dispositivo() != OK {
		t.Fatalf("interpretou raw empacotado: %+v", v.Achados)
	}
}

func TestDerivaDeCiclosPE(t *testing.T) {
	v := julgar(disco(
		func(l *Leitura) { l.NAND = "tlc" },
		attrs(attr("Ave_Block-Erase_Count", 900, id(173))),
	))
	if v.Dispositivo() != Aviso {
		t.Fatalf("900/1000 ciclos = %d", v.Dispositivo())
	}
}

// ------------------------------------------------------------- temperatura

func TestTemperaturaSSD(t *testing.T) {
	for _, k := range []struct {
		t    float64
		quer int
	}{{45, OK}, {55, Info}, {65, Aviso}, {72, Critico}} {
		v := julgar(disco(func(l *Leitura) { l.TempC = f(k.t) }))
		if got := v.Dispositivo(); got != k.quer {
			t.Errorf("ssd %.0fC = %d, queria %d", k.t, got, k.quer)
		}
	}
}

func TestTemperaturaHDD(t *testing.T) {
	for _, k := range []struct {
		t    float64
		quer int
	}{{35, OK}, {42, Info}, {50, Aviso}, {56, Critico}, {10, Critico}} {
		v := julgar(disco(func(l *Leitura) { l.Tipo = "hdd"; l.TempC = f(k.t) }))
		if got := v.Dispositivo(); got != k.quer {
			t.Errorf("hdd %.0fC = %d, queria %d", k.t, got, k.quer)
		}
	}
}

func TestMaximaHistoricaContaUmNivelAbaixo(t *testing.T) {
	// Pico de 65 C ha seis meses e registro, nao emergencia de agora.
	v := julgar(disco(func(l *Leitura) { l.TempC = f(40); l.TempMaxC = f(65) }))
	if v.Dispositivo() != Info {
		t.Fatalf("nivel = %d, queria Info", v.Dispositivo())
	}
}

func TestThrottleDisparaSozinho(t *testing.T) {
	v := julgar(disco(func(l *Leitura) { l.TempC = f(40); l.Throttle = true }))
	if v.Dispositivo() != Aviso {
		t.Fatalf("nivel = %d", v.Dispositivo())
	}
}

// ------------------------------------------------------------ saude do host

func razaoHost(sujos, ciclos int64) Veredito {
	return julgar(disco(func(l *Leitura) {
		l.DesligamentosSujos, l.CiclosEnergia = c(sujos), c(ciclos)
	}))
}

func TestSaudeDoHostFaixas(t *testing.T) {
	for _, k := range []struct {
		sujos, ciclos int64
		quer          int
	}{{1, 100, OK}, {10, 100, Info}, {20, 100, Aviso}, {40, 100, Critico}} {
		if got := razaoHost(k.sujos, k.ciclos).Host(); got != k.quer {
			t.Errorf("%d/%d = %d, queria %d", k.sujos, k.ciclos, got, k.quer)
		}
	}
}

func TestHostNaoContaminaODispositivo(t *testing.T) {
	v := razaoHost(40, 100)
	if v.Dispositivo() != OK {
		t.Fatalf("dispositivo = %d", v.Dispositivo())
	}
}

func TestRecomendaNobreakNaoTrocaDeDisco(t *testing.T) {
	m := razaoHost(40, 100).Achados[0].Mensagem
	if !strings.Contains(m, "nobreak") || !strings.Contains(m, "nao e a causa") {
		t.Fatalf("mensagem = %q", m)
	}
}

// ------------------------------------------------------------- anti-ruido

func TestSubirExigeDuasLeituras(t *testing.T) {
	e := NovoEstabilizador(Config{})
	quente := []Achado{{Dispositivo, Aviso, "temp", "61 C", AgirAgora}}
	if got := e.Estabilizar("X1", quente); len(got) != 0 {
		t.Fatalf("promoveu na primeira leitura: %+v", got)
	}
	if got := e.Estabilizar("X1", quente); len(got) != 1 || got[0].Severidade != Aviso {
		t.Fatalf("nao promoveu na segunda: %+v", got)
	}
}

func TestPicoIsoladoNaoPromove(t *testing.T) {
	e := NovoEstabilizador(Config{})
	quente := []Achado{{Dispositivo, Aviso, "temp", "61 C", AgirAgora}}
	e.Estabilizar("X1", quente)
	e.Estabilizar("X1", nil) // voltou ao normal
	if got := e.Estabilizar("X1", quente); len(got) != 0 {
		t.Fatalf("dois picos separados promoveram: %+v", got)
	}
}

func TestDebounceNaoRepeteNaJanela(t *testing.T) {
	e := NovoEstabilizador(Config{})
	a := Achado{Dispositivo, Critico, "imediato:pendentes", "10", AgirAgora}
	if !e.DeveNotificar("X1", a, 0) {
		t.Fatal("nao notificou a primeira vez")
	}
	if e.DeveNotificar("X1", a, 3600) {
		t.Fatal("repetiu uma hora depois")
	}
	if !e.DeveNotificar("X1", a, 7*3600) {
		t.Fatal("nao notificou depois da janela")
	}
}

func TestInfoNaoNotifica(t *testing.T) {
	e := NovoEstabilizador(Config{})
	if e.DeveNotificar("X1", Achado{Dispositivo, Info, "x", "y", AgirAgora}, 0) {
		t.Fatal("Info virou notificacao")
	}
}

func TestContadorQueDiminuiEAnomalia(t *testing.T) {
	// So cresce. Diminuiu = disco trocado na baia, firmware bugado ou
	// parsing errado - nunca melhora.
	if !ContadorRegrediu(10, 4) {
		t.Error("nao detectou regressao")
	}
	if ContadorRegrediu(4, 10) {
		t.Error("crescimento virou regressao")
	}
}

// ------------------------------------------- os sete casos da secao 10

func TestSpec1_WDBlueDoExemplo(t *testing.T) {
	// WD Blue 240G: 4 blocos crescidos, reserva 98, 0 erros, 11% de
	// desgaste, 39 de 90 desligamentos sujos (razao 0,43).
	//
	// DIVERGENCIA: a secao 10 espera `host` em Aviso. A tabela da secao 7 e
	// o esqueleto de configuracao da secao 9 dizem os dois que acima de 0,30
	// e Critico, e 0,43 esta acima. Seguimos as duas fontes concordantes.
	v := julgar(disco(
		func(l *Leitura) {
			l.Modelo, l.Serial = "WDC WDS240G2G0A", "WD-1"
			l.PercentualUsado = f(11)
			l.DesligamentosSujos, l.CiclosEnergia = c(39), c(90)
		},
		attrs(
			attr("Available_Reservd_Space", 0, id(232), valor(98), limiar(10)),
			attr("Grown_Bad_Blocks", 4, id(170), parado(4)),
			attr("Current_Pending_Sector", 0, id(197)),
			attr("Reported_Uncorrect", 0, id(187)),
		),
	))
	if v.Dispositivo() != OK {
		t.Errorf("dispositivo = %d: %+v", v.Dispositivo(), v.Achados)
	}
	if v.Host() != Critico {
		t.Errorf("host = %d, queria Critico", v.Host())
	}
	if v.Interconexao() != OK {
		t.Errorf("interconexao = %d", v.Interconexao())
	}
}

func TestSpec2_DuzentosSetoresEstaticos(t *testing.T) {
	// No maximo Info. Sair Aviso significa que a regra de taxa esta sendo
	// ignorada - o principio 3 vence a tabela de contagem bruta.
	v := julgar(disco(
		func(l *Leitura) { l.Tipo = "hdd" },
		attrs(attr("Reallocated_Sector_Ct", 200, id(5), parado(200))),
	))
	if v.Dispositivo() > Info {
		t.Fatalf("dispositivo = %d: %+v", v.Dispositivo(), v.Achados)
	}
}

func TestSpec3_CrescimentoRecenteVenceReservaSaudavel(t *testing.T) {
	// 0 -> 6 em poucos dias, com reserva em 97.
	//
	// DIVERGENCIA: a secao 10 espera Critico. Pela tabela da secao 3, seis
	// novos em sete dias e Aviso (>= 3) e nao Critico (>= 10); so vira
	// Critico se cinco deles cairem em 24 h. Os dois casos abaixo cobrem a
	// diferenca, que e onde o surto acontece.
	espalhado := julgar(disco(attrs(
		attr("Available_Reservd_Space", 0, id(232), valor(97)),
		attr("Grown_Bad_Blocks", 6, id(170), hist(1, 6, 6, 0, 30)),
	)))
	if espalhado.Dispositivo() != Aviso {
		t.Errorf("espalhado = %d, queria Aviso", espalhado.Dispositivo())
	}
	surto := julgar(disco(attrs(
		attr("Available_Reservd_Space", 0, id(232), valor(97)),
		attr("Grown_Bad_Blocks", 6, id(170), hist(5, 6, 6, 0, 30)),
	)))
	if surto.Dispositivo() != Critico {
		t.Errorf("surto = %d, queria Critico", surto.Dispositivo())
	}
}

func TestSpec4_PendenteComORestoLimpo(t *testing.T) {
	v := julgar(disco(attrs(
		attr("Current_Pending_Sector", 2, id(197)),
		attr("Available_Reservd_Space", 0, id(232), valor(99)),
	)))
	if v.Dispositivo() != Aviso {
		t.Fatalf("dispositivo = %d", v.Dispositivo())
	}
}

func TestSpec5_SemHistoricoNaoEOK(t *testing.T) {
	// Dispositivo novo no inventario: as regras de taxa ficam em "sem
	// dados", que e diferente de afirmar que esta tudo bem.
	v := julgar(disco(attrs(attr("Reallocated_Sector_Ct", 3, id(5)))))
	achou := false
	for _, s := range v.SemDados {
		if s == "realocados" {
			achou = true
		}
	}
	if !achou {
		t.Fatalf("sem dados = %v", v.SemDados)
	}
}

func TestSpec6_VendorSpecificForaDaTabelaEIgnorado(t *testing.T) {
	v := julgar(disco(attrs(attr("Unknown_Attribute_177", 999999, id(177)))))
	if v.Dispositivo() != OK || len(v.Achados) != 0 {
		t.Fatalf("inventou significado: %+v", v.Achados)
	}
}

func TestSpec7_FalhaDeColetaNaoEOK(t *testing.T) {
	// Disco atras de RAID sem -d: a coleta falha em silencio. Isso precisa
	// ser um estado proprio, senao ele fica saudavel para sempre.
	v := julgar(disco(func(l *Leitura) {
		l.ColetaOK, l.ErroColeta = false, "precisa de -d megaraid,N"
	}))
	if v.ColetaOK {
		t.Error("coleta falha nao foi registrada")
	}
	if v.Dispositivo() == OK {
		t.Error("coleta falha virou OK")
	}
	if !strings.Contains(v.Resumo(), "desconhecida") {
		t.Errorf("resumo = %q", v.Resumo())
	}
}

// ------------------------------------------------------------- composicao

func TestSeveridadeEOMaximoNuncaAMedia(t *testing.T) {
	v := julgar(disco(
		func(l *Leitura) { l.TempC = f(45) },
		attrs(
			attr("Available_Reservd_Space", 0, id(232), valor(99)),
			attr("Reported_Uncorrect", 1, id(187)),
		),
	))
	if v.Dispositivo() != Critico {
		t.Fatalf("dispositivo = %d", v.Dispositivo())
	}
}

func TestNuncaAfirmaQueODiscoEstaSaudavel(t *testing.T) {
	// Entre 23% e 36% dos discos que falharam nao tinham indicador SMART.
	// Prometer saude e o unico erro deste pacote que custaria dados.
	txt := strings.ToLower(julgar(disco()).Resumo())
	if strings.Contains(txt, "saudavel") || strings.Contains(txt, "saudável") {
		t.Fatalf("resumo promete saude: %q", txt)
	}
	if !strings.Contains(txt, "sem indicadores") {
		t.Fatalf("resumo = %q", txt)
	}
}

// ------------------------------------------------------------- auxiliares

func temRegra(v Veredito, regra string) bool {
	for _, a := range v.Achados {
		if a.Regra == regra {
			return true
		}
	}
	return false
}

func temRegraContendo(v Veredito, parte string) bool {
	for _, a := range v.Achados {
		if strings.Contains(a.Regra, parte) {
			return true
		}
	}
	return false
}
