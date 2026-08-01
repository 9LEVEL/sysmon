//go:build !windows

package bandeja

// Sem bandeja fora do Windows, e de proposito.
//
// Icone de bandeja no Linux nao tem um caminho unico: e StatusNotifierItem
// por DBus nos ambientes novos, XEmbed nos antigos, e nenhum dos dois esta
// presente em toda parte. Uma implementacao pela metade seria pior que
// nenhuma - o programa pareceria ter sumido ao fechar a janela.
//
// Aqui fechar a janela encerra o programa, que e o comportamento esperado
// num sistema sem bandeja garantida. Quando houver uma implementacao, ela
// entra neste arquivo e o resto do codigo nao muda.

type semBandeja struct{}

func Iniciar(Acoes) (Bandeja, error) { return semBandeja{}, nil }

func (semBandeja) Estado(int, string)       {}
func (semBandeja) Notificar(string, string) {}
func (semBandeja) Fechar()                  {}

// Disponivel diz se ha bandeja de verdade nesta plataforma.
//
// A janela consulta isto para decidir se fechar deve esconder ou encerrar:
// esconder sem bandeja deixaria um processo invisivel rodando, que o usuario
// so descobre no gerenciador de tarefas.
func Disponivel() bool { return false }
