//go:build !windows

package commandline

// Prepare: a terminal gives the program its own input and output elsewhere
// (console_windows.go).
func Prepare([]string) func() { return func() {} }
