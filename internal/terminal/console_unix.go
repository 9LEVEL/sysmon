//go:build !windows

package terminal

// No Unix o processo ja nasce com os descritores certos.
func PrenderConsole() bool { return true }
