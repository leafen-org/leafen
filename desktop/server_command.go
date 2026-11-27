//go:build !ios && !android

package main

import (
	"slices"

	"github.com/pocketbase/pocketbase"

	"leafen.org/leafen/internal/app"
	"leafen.org/leafen/internal/commandline"
)

// Leafen is one program (the one-program decision of 2026-10-27, 1): the
// desktop app's window, and without a window a server's command line —
// `leafen serve --http=… --dir=…` and its other commands. The Docker image,
// and Linux, carry the same server without the window (cmd/leafen).

// runServerCommand runs a server's command line, without a window, and
// says how it ended; false when the line isn't one. Every command works on
// the data folder --dir names, else on the app's own data
// (account_command.go): one rule for all of them (2026-11-19: `connect` and
// `backup` looked for ./data in the folder they were typed in, while
// `setup` and `account` found the app's).
func runServerCommand(args []string) (int, bool) {
	if !app.IsCommand(args) || slices.Contains(args, "--background") {
		return 0, false
	}
	// Typed in a terminal, it writes there, as the server's program does
	// (internal/commandline).
	finish := commandline.Prepare(args)
	defer finish()
	if args[0] == "app" {
		return app.RunAppCommand(args[1:]), true
	}
	if !dirGiven(args) {
		return runOwnDataCommand(args), true
	}
	product := app.New()
	describeProgram(product)
	if err := app.Execute(product); err != nil {
		return commandline.Failed(err), true
	}
	return 0, true
}

// describeProgram says in the program's help (`leafen-desktop help`) what it
// does without a command, and on which data its commands work, naming the
// program as it is typed (on Windows without .exe, docs/install.md).
func describeProgram(product *pocketbase.PocketBase) {
	product.RootCmd.Use = desktopProgram
	product.RootCmd.Long = "Leafen. Without a command it opens the Leafen app. With one it runs that command without a window, on the app's " +
		"own data unless --dir names another data folder: `serve` runs a server, and the others set it up, back it up, restore it " +
		"and connect agents to it."
}
