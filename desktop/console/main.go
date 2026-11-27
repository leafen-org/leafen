// Command console is the desktop app's program as a terminal types it on
// Windows: leafen-desktop.com beside leafen-desktop.exe, which PowerShell
// and cmd run for `leafen-desktop` (and `.\leafen-desktop`), as .COM comes
// before .EXE. The app's own program is a window program, which a terminal
// neither waits for nor gives its console (2026-11-19: `setup status` said
// nothing in PowerShell); this one is a console program that runs it with
// the terminal's input and output, waits, and ends with its exit code.
// Without a command it starts the app and ends at once.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	program := strings.TrimSuffix(self, filepath.Ext(self)) + ".exe"
	command := exec.Command(program, os.Args[1:]...)
	if len(os.Args) < 2 {
		if err := command.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// Ctrl+C reaches the program too where it shares the console; where
	// its output goes elsewhere, it ends a few seconds on.
	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, os.Interrupt)
	go func() {
		<-interrupted
		time.Sleep(5 * time.Second)
		_ = command.Process.Kill()
	}()
	err = command.Wait()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		os.Exit(exit.ExitCode())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
