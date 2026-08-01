package tela

// Item e um campo que pode ser escondido.
type Item struct {
	Chave  string
	Rotulo string
}

// Secao agrupa itens na tela de exibicao.
//
// SoTela marca as secoes que NAO viram um bloco na arvore: sao ajustes de
// aparencia. Sem essa marca, o teste que confere catalogo contra arvore
// precisaria carregar o nome delas escrito na mao, e envelheceria na
// primeira secao nova.
type Secao struct {
	Nome   string
	Nota   string
	SoTela bool
	Itens  []Item
}

// Catalogo e a fonte unica da tela de exibicao: acrescentar um campo aqui o
// faz aparecer na interface sem mexer na interface.
var Catalogo = []Secao{
	{Nome: "TELA", Nota: "aparencia da janela", SoTela: true, Itens: []Item{
		{"c:scope", "grafico animado de cpu no topo"},
		{"c:detalhe", "coluna do meio (o texto entre o nome e o valor)"},
	}},
	{Nome: "RESUMO", Nota: "na linha do host", SoTela: true, Itens: []Item{
		{"r:temp", "temperatura da cpu"},
		{"r:cpu", "uso de cpu"},
		{"r:ram", "uso de memoria em %"},
		{"r:gb", "memoria usada em GB"},
		{"r:cpumodelo", "modelo do processador"},
		{"r:so", "sistema operacional"},
	}},
	{Nome: "DESEMPENHO", Itens: []Item{
		{"p:cpu", "uso de cpu"},
		{"p:mem", "memoria"},
		{"p:swap", "swap"},
		{"p:load", "carga (load average)"},
		{"p:up", "tempo no ar"},
	}},
	{Nome: "TEMPERATURA", Itens: []Item{
		{"t:cpu", "cpu"},
		{"t:todos", "demais sensores do hardware"},
	}},
	{Nome: "VENTOINHAS", Itens: []Item{
		{"v:todas", "rotacao em rpm"},
	}},
	{Nome: "DISCOS", Itens: []Item{
		{"b:todos", "discos fisicos: modelo, temperatura, desgaste, SMART"},
	}},
	{Nome: "ARMAZENAMENTO", Itens: []Item{
		{"a:fs", "filesystems montados"},
		{"a:thin", "thin pool LVM (Proxmox)"},
	}},
	{Nome: "REDE", Itens: []Item{
		{"n:todas", "interfaces ativas"},
	}},
}

// ChavesDoCatalogo devolve todas as chaves, inclusive as de secao.
func ChavesDoCatalogo() []string {
	var out []string
	for _, s := range Catalogo {
		out = append(out, "sec:"+s.Nome)
		for _, i := range s.Itens {
			out = append(out, i.Chave)
		}
	}
	return out
}
