package app

import "runtime"

// A command of Leafen's program that takes JSON (`leafen mcp call`, `leafen
// mcp secret`) as it is typed. Windows PowerShell 5.1 hands a program its
// argument with the double quotes taken out: '{"method": "GET", "path":
// "/bootstrap"}' arrived as {method: GET, path: /bootstrap} (2026-11-25, on
// Windows PowerShell 5.1). So every text that gives such a command gives it
// for a POSIX shell and, where the shell may be Windows PowerShell, also with
// the JSON on standard input (the command's -), sent as UTF-8: Windows
// PowerShell 5.1 pipes a program ASCII unless $OutputEncoding says
// otherwise, and a name in Korean would arrive as ??. This is the one place
// that writes them.

// psUTF8Pipe makes Windows PowerShell pipe UTF-8 to a program; PowerShell 7
// does already.
const psUTF8Pipe = "$OutputEncoding = [Text.UTF8Encoding]::new($false); "

// jsonCommand is `command '<json>' flags` for a POSIX shell.
func jsonCommand(command, json, flags string) string {
	return command + " " + shellQuoted(json) + flags
}

// jsonCommandPowerShell is the same for Windows PowerShell, the JSON on
// standard input.
func jsonCommandPowerShell(command, json, flags string) string {
	return psUTF8Pipe + psQuoted(json) + " | " + command + " -" + flags
}

// jsonCommandWords is the command as a text gives it: the POSIX shell's in
// backticks and, where powerShellToo, Windows PowerShell's after it.
func jsonCommandWords(command, json, flags string, powerShellToo bool) string {
	words := "`" + jsonCommand(command, json, flags) + "`"
	if powerShellToo {
		words += " (in Windows PowerShell: `" + jsonCommandPowerShell(command, json, flags) + "`)"
	}
	return words
}

// jsonCommandHere is jsonCommandWords for a command this program prints for
// the computer it runs on: Windows PowerShell's too on Windows. A text the
// server sends to an agent anywhere gives both (powerShellToo true).
func jsonCommandHere(command, json, flags string) string {
	return jsonCommandWords(command, json, flags, runtime.GOOS == "windows")
}
