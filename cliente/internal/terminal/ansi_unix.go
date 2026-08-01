//go:build !windows

package terminal

// No Unix o terminal ja interpreta ANSI; nao ha nada para preparar.
func prepararANSI() bool { return true }
