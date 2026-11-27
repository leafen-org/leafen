package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"leafen.org/leafen/internal/commandline"
)

// typedProgram is the app's program as the commands it prints are typed to
// run it again: on Windows `.\leafen-desktop` in its own folder, which
// PowerShell needs (it runs nothing from the folder it is in by its bare
// name, 2026-11-19) and cmd takes, and which runs the console program beside
// it (console/main.go); elsewhere the program's full path, as on a Mac,
// where no folder a terminal looks in holds it. On Windows a path with
// anything PowerShell reads as its own syntax goes single-quoted after "&"
// (commandline.PowerShellPlainPath, commandline.PowerShellQuoted).
func typedProgram() string {
	program, err := os.Executable()
	if err != nil {
		return desktopProgram
	}
	if runtime.GOOS == "windows" {
		if here, err := os.Getwd(); err == nil && strings.EqualFold(filepath.Clean(here), filepath.Dir(program)) {
			return `.\` + desktopProgram
		}
		program = strings.TrimSuffix(program, filepath.Ext(program))
		if commandline.PowerShellPlainPath(program) {
			return program
		}
		return "& " + commandline.PowerShellQuoted(program)
	}
	if strings.ContainsAny(program, " '\"&;()") {
		return "'" + strings.ReplaceAll(program, "'", `'\''`) + "'"
	}
	return program
}
