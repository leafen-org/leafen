//go:build ios || android

package main

// A phone has no server's command line: its own server is the trial the
// app starts and stops with its window (server.go).

func runServerCommand([]string) (int, bool) { return 0, false }
