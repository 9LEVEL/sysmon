//go:build !windows

package janela

import "gioui.org/io/event"

// Fora do Windows nao ha bandeja (ver internal/bandeja), e esconder a janela
// sem ter de onde traze-la de volta seria perde-la. Fica so o minimizar.
type janelaNativa struct{}

func (*janelaNativa) ver(event.Event) {}
func (*janelaNativa) esconder() bool  { return false }
func (*janelaNativa) mostrar()        {}
