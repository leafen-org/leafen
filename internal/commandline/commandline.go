// Package commandline is what Leafen's two programs share as a terminal
// runs them: the server's (cmd/leafen, `leafen`) and the desktop app's
// (desktop/, `leafen-desktop`), which run the same commands
// (internal/app). Prepare sets a typed command's input and output up for
// the terminal it was typed in (console_windows.go; elsewhere a terminal
// gives the program its own), and Failed says what went wrong with it.
package commandline

import (
	"fmt"
	"os"
	"strings"
)

// Failed writes what went wrong with a typed command as one line on
// standard error, in the command's own words, and is the exit code a
// command that failed ends with, 1. Both programs say it so: the server's
// program and the desktop program with --dir wrote it with the log's date
// and time before it ("2026/11/19 04:51:56 no account on this server has
// the name nobody"), while the desktop program on the app's own data wrote
// the words alone (2026-11-19). An agent reports the message as it is
// (docs/install.md).
func Failed(err error) int {
	fmt.Fprintln(os.Stderr, err)
	return 1
}

// speaksProtocol says whether a command line's standard streams carry a
// protocol another program reads, which takes them as they are: `mcp`'s
// JSON-RPC, in UTF-8 whatever the terminal (MCP.md). The app worker, the
// other one, is started by the app platform and runs before Prepare.
func speaksProtocol(args []string) bool {
	return commandOf(args) == "mcp"
}

// rootValueFlags are the program's own flags that take a value as the next
// argument (PocketBase's and the setup's, internal/app/app.go), so that a
// value isn't read as the command's name.
var rootValueFlags = []string{"--dir", "--encryptionEnv", "--queryTimeout", "--setup-open"}

// commandOf is the command a command line names: its first argument that is
// neither a flag nor a flag's value; "" where it names none.
func commandOf(args []string) string {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			break
		}
		if !strings.HasPrefix(arg, "-") {
			return arg
		}
		for _, flag := range rootValueFlags {
			if arg == flag {
				index++
				break
			}
		}
	}
	return ""
}
