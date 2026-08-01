package janela

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"sysmon/internal/nucleo"
	"sysmon/internal/tela"
)

// Testes de interacao sem abrir janela nenhuma.
//
// O roteador de eventos do Gio funciona fora de uma janela de verdade: da
// para desenhar um quadro, injetar um clique numa coordenada e desenhar o
// seguinte. E o unico jeito honesto de testar "clicar aqui abre aquilo" no
// CI, que nao tem tela - e e mais confiavel que automacao de X, onde a
// posicao da janela depende do gerenciador de janelas instalado.

// bancada monta uma Janela pronta para receber cliques.
type bancada struct {
	j     *Janela
	r     input.Router
	ops   op.Ops
	tam   image.Point
	agora time.Time
}

func novaBancada(t *testing.T, hosts ...nucleo.Host) *bancada {
	t.Helper()
	bruto := map[string]any{"hosts": []any{}}
	lista := []any{}
	for _, h := range hosts {
		lista = append(lista, map[string]any{
			"nome": h.Nome, "url": h.URL, "token": h.Token})
	}
	bruto["hosts"] = lista
	cfg, err := nucleo.ConfigDe(bruto)
	if err != nil {
		t.Fatal(err)
	}
	j := Nova(nucleo.NovaFrota(cfg, nil), t.TempDir()+"/config.json", "teste")
	return &bancada{j: j, tam: image.Pt(820, 640), agora: time.Now()}
}

// quadro desenha um quadro e entrega os eventos pendentes aos widgets.
func (b *bancada) quadro() {
	b.ops.Reset()
	// Now avanca a cada quadro: o gesture.Click do Gio usa o relogio para
	// distinguir clique de duplo clique, e com Now zerado ele nunca conclui
	// o gesto. Foi o que fez a primeira versao deste teste falhar sem que
	// houvesse nada errado no codigo da janela.
	b.agora = b.agora.Add(16 * time.Millisecond)
	gtx := layout.Context{
		Ops:         &b.ops,
		Now:         b.agora,
		Constraints: layout.Exact(b.tam),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      b.r.Source(),
	}
	b.j.desenhar(gtx)
	b.r.Frame(&b.ops)
}

// gtx devolve um contexto para medir texto do mesmo jeito que o desenho mede.
func (b *bancada) gtx() layout.Context {
	return layout.Context{
		Ops:         &b.ops,
		Now:         b.agora,
		Constraints: layout.Exact(b.tam),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      b.r.Source(),
	}
}

// clique injeta press e release na coordenada e roda os quadros necessarios.
//
// Sao dois quadros de proposito: o Gio entrega o evento ao widget durante o
// quadro seguinte a injecao, e e nesse que o handler roda.
func (b *bancada) clique(x, y int) {
	b.quadro()
	b.r.Queue(
		pointer.Event{Kind: pointer.Press, Position: f32.Pt(float32(x), float32(y)),
			Buttons: pointer.ButtonPrimary},
		pointer.Event{Kind: pointer.Release, Position: f32.Pt(float32(x), float32(y)),
			Buttons: pointer.ButtonPrimary},
	)
	b.quadro()
	b.quadro()
}

// posicao dos icones do cabecalho, da direita para a esquerda.
func iconeX(larg, indiceDaDireita int) int {
	x := larg - Margem - 24 // fechar
	for i := 0; i < indiceDaDireita; i++ {
		x -= 24
		if i == 1 { // separador entre controles de janela e ferramentas
			x -= 10 + 20 - 24
		}
	}
	return x + 12
}

func TestCliqueAbreOsDialogos(t *testing.T) {
	casos := []struct {
		nome   string
		indice int
		quer   qualDialogo
	}{
		{"hosts", 2, dlgHosts},
		{"exibir", 3, dlgExibir},
		{"alertas", 4, dlgAlertas},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			b := novaBancada(t)
			b.clique(iconeX(b.tam.X, c.indice), 19)
			if b.j.dialogo != c.quer {
				t.Fatalf("dialogo = %d, queria %d", b.j.dialogo, c.quer)
			}
		})
	}
}

func TestCancelarFechaSemGravar(t *testing.T) {
	b := novaBancada(t, nucleo.Host{Nome: "pve",
		URL: "http://10.0.0.9:9109/metrics", Token: "t"})
	b.j.abrirHosts()
	b.quadro()

	// Mede do mesmo jeito que o desenho mede, em vez de chutar a posicao: o
	// rodape do dialogo alinha os botoes pela direita.
	r := centrado(b.gtx(), 780, 460)
	lSalvar := b.j.Medir(b.gtx(), "SALVAR", 12, true) + 22
	lCancel := b.j.Medir(b.gtx(), "CANCELAR", 12, true) + 22
	x := r.Min.X + r.Dx() - 24 - lSalvar - lCancel + lCancel/2
	b.clique(x, r.Min.Y+r.Dy()-42+13)

	if b.j.dialogo != semDialogo {
		t.Fatal("cancelar nao fechou o dialogo")
	}
	if _, err := nucleo.CarregarConfig(b.j.caminho); err == nil {
		t.Fatal("cancelar gravou o arquivo")
	}
}

func TestExibirDesmarcadoSomeDaTela(t *testing.T) {
	// O caminho inteiro: abrir o dialogo, desmarcar uma secao, aplicar, e a
	// secao sumir das linhas montadas.
	b := novaBancada(t)
	b.j.abrirExibir()
	b.quadro()

	for si, s := range tela.Catalogo {
		if s.Nome == "REDE" {
			b.j.dlgExibir.secoes[si].Marcar(false)
		}
	}
	b.j.aplicarExibir(b.j.dlgExibir)

	if !b.j.oculto["sec:REDE"] {
		t.Fatal("aplicar nao guardou a escolha")
	}
	if b.j.dialogo != semDialogo {
		t.Fatal("aplicar nao fechou o dialogo")
	}
}

func TestEstadoDaTelaSobreviveAoFechar(t *testing.T) {
	b := novaBancada(t)
	b.j.oculto = tela.Visiveis{"sec:REDE": true, "c:scope": true}
	b.j.salvarEstado()

	outra := novaBancada(t)
	outra.j.caminho = b.j.caminho
	outra.j.carregarEstado()
	if !outra.j.oculto["sec:REDE"] || !outra.j.oculto["c:scope"] {
		t.Fatalf("estado nao voltou: %v", outra.j.oculto)
	}
}

func TestAlertasRecusaAvisoAcimaDoCritico(t *testing.T) {
	// Aviso acima do critico nunca dispararia aviso - o critico venceria
	// sempre - e o campo pareceria ignorado.
	b := novaBancada(t)
	b.j.abrirAlertas()
	d := b.j.dlgAlertas
	d.aviso[0].Definir("95")
	d.critico[0].Definir("80")
	b.j.salvarAlertas(d)

	if d.erro == "" {
		t.Fatal("aceitou aviso acima do critico")
	}
	if b.j.dialogo != dlgAlertas {
		t.Fatal("fechou o dialogo mesmo com erro")
	}
}

func TestAlertasRecusaTextoNoLugarDeNumero(t *testing.T) {
	b := novaBancada(t)
	b.j.abrirAlertas()
	d := b.j.dlgAlertas
	d.aviso[0].Definir("muito")
	b.j.salvarAlertas(d)
	if d.erro == "" {
		t.Fatal("aceitou texto onde devia ser numero")
	}
}

func TestSalvarHostsGravaEAplica(t *testing.T) {
	b := novaBancada(t)
	b.j.abrirHosts()
	d := b.j.dlgHosts
	d.linhas[0].nome.Definir("pve")
	d.linhas[0].url.Definir("http://10.0.0.9:9109")
	d.linhas[0].token.Definir("segredo")
	b.j.salvarHosts(d)

	if d.erro != "" {
		t.Fatalf("erro ao salvar: %s", d.erro)
	}
	cfg, err := nucleo.CarregarConfig(b.j.caminho)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Hosts) != 1 || cfg.Hosts[0].Nome != "pve" {
		t.Fatalf("config gravado errado: %+v", cfg.Hosts)
	}
	// A url sem caminho ganha /metrics na validacao, e e a validada que
	// tem que ter sido gravada.
	if cfg.Hosts[0].URL != "http://10.0.0.9:9109/metrics" {
		t.Fatalf("url = %q", cfg.Hosts[0].URL)
	}
	// E a frota tem que ter trocado sem reiniciar o programa.
	if len(b.j.frota.Cfg().Hosts) != 1 {
		t.Fatal("a frota nao recebeu a config nova")
	}
}

func TestUrlInvalidaNaoDestroiOConfigQueFuncionava(t *testing.T) {
	b := novaBancada(t)
	b.j.abrirHosts()
	d := b.j.dlgHosts
	d.linhas[0].url.Definir("http://bom:9109/metrics")
	b.j.salvarHosts(d)
	if d.erro != "" {
		t.Fatal(d.erro)
	}

	b.j.abrirHosts()
	d = b.j.dlgHosts
	d.linhas[0].url.Definir("isso nao e uma url")
	b.j.salvarHosts(d)
	if d.erro == "" {
		t.Fatal("url invalida foi aceita")
	}

	// O arquivo anterior tem que continuar intacto: validar antes de gravar
	// existe exatamente para isto.
	cfg, err := nucleo.CarregarConfig(b.j.caminho)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Hosts[0].URL != "http://bom:9109/metrics" {
		t.Fatalf("o config bom foi sobrescrito: %+v", cfg.Hosts)
	}
}

func TestTokenEmBrancoMantemOAnterior(t *testing.T) {
	// Mudar um apelido nao deve exigir redigitar o segredo.
	b := novaBancada(t, nucleo.Host{Nome: "pve",
		URL: "http://10.0.0.9:9109/metrics", Token: "segredo"})
	b.j.abrirHosts()
	d := b.j.dlgHosts
	d.linhas[0].token.Definir("")
	b.j.salvarHosts(d)

	cfg, _ := nucleo.CarregarConfig(b.j.caminho)
	if cfg.Hosts[0].Token != "segredo" {
		t.Fatalf("token = %q, queria segredo", cfg.Hosts[0].Token)
	}
}

// mover injeta um movimento de ponteiro, para exercitar hover.
func (b *bancada) mover(x, y int) {
	b.quadro()
	b.r.Queue(pointer.Event{Kind: pointer.Move,
		Position: f32.Pt(float32(x), float32(y))})
	b.quadro()
	b.quadro()
}

func TestDicaAparecePassandoOMouseNoIcone(t *testing.T) {
	// Sete icones sem rotulo e um enigma novo a cada release. A versao Tkinter
	// tinha essa dica e ela se perdeu na migracao para o Gio.
	casos := []struct {
		nome   string
		indice int
		trecho string
	}{
		{"hosts", 2, "hosts"},
		{"exibir", 3, "aparece"},
		{"alertas", 4, "limiares"},
		{"topo", 6, "topo"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			b := novaBancada(t)
			b.mover(iconeX(b.tam.X, c.indice), 19)
			if b.j.dicaBotao == "" {
				t.Fatal("nenhuma dica sob o cursor")
			}
			if !strings.Contains(b.j.dicaBotao, c.trecho) {
				t.Fatalf("dica = %q, esperava conter %q", b.j.dicaBotao, c.trecho)
			}
		})
	}
}

func TestDicaSomeQuandoOCursorSai(t *testing.T) {
	// Dica que fica presa na tela e pior que dica nenhuma: vira um rotulo
	// errado apontando para um icone que o cursor ja deixou.
	b := novaBancada(t)
	b.mover(iconeX(b.tam.X, 2), 19)
	if b.j.dicaBotao == "" {
		t.Fatal("nao apareceu")
	}
	b.mover(b.tam.X/2, b.tam.Y/2)
	if b.j.dicaBotao != "" {
		t.Fatalf("dica ficou presa: %q", b.j.dicaBotao)
	}
}

func TestBotaoDeTopoAcendeEDesligaComOEstado(t *testing.T) {
	// Ate a v5.1 o clique so virava o booleano e a janela nao subia; agora o
	// estado tambem vai para o disco, entao o texto da dica muda junto.
	b := novaBancada(t)
	if b.j.NoTopo() {
		t.Fatal("nasceu ligado")
	}
	b.clique(iconeX(b.tam.X, 6), 19)
	if !b.j.NoTopo() {
		t.Fatal("o clique nao ligou")
	}
	b.mover(iconeX(b.tam.X, 6), 19)
	if !strings.Contains(b.j.dicaBotao, "ligado") {
		t.Fatalf("dica = %q", b.j.dicaBotao)
	}
}

func TestCliqueNoSeletorTrocaAMedidaDoGrafico(t *testing.T) {
	// O grafico do topo mostra UM host e UMA medida, e as duas etiquetas sao
	// como se escolhe. Sem elas, o numero ali nao tem referencia.
	b := novaBancada(t, nucleo.Host{Nome: "pve", URL: "http://x/metrics", Token: "t"})
	b.j.mu.Lock()
	b.j.hostsScope = []string{"pve"}
	b.j.mu.Unlock()

	if got := b.j.medidaScope(); got != "cpu" {
		t.Fatalf("medida inicial = %q", got)
	}
	// A etiqueta da medida vem depois da do host, no canto do grafico.
	larguraHost := b.j.Medir(b.gtx(), "pve", 11, false)
	b.clique(Margem+4+larguraHost+16, AltCabec+6)
	if got := b.j.medidaScope(); got != "ram" {
		t.Fatalf("medida = %q, queria ram", got)
	}
}
