//go:build !ios && !android

package main

import (
	"errors"
	"fmt"
	"slices"

	"leafen.org/leafen/internal/app"
	"leafen.org/leafen/internal/commandline"
)

// The server's commands, run as the app's program without --dir, work on
// the app's own data, as `leafen …` does on a server's data folder:
// `connect` connects an agent, `backup` and `restore` back it up and fill
// it, `account` helps an account back in — a one-time code that signs a
// device in (device-code) or a one-time password (reset-password);
// `tailscale` and `address` give it an outside address; `setup` goes
// through its first run from a terminal, the first screen included (user
// decision 2026-11-09). Whoever runs the app's program as the computer's
// user can read that data anyway. They work while the app runs. This is the
// desktop program's command line (server_command.go), so it is built where
// that is: a phone has none (server_command_mobile.go).

func dirGiven(args []string) bool {
	return slices.ContainsFunc(args, func(arg string) bool { return arg == "--dir" || len(arg) > 6 && arg[:6] == "--dir=" })
}

// runOwnDataCommand runs a command line on the app's own data.
func runOwnDataCommand(args []string) int {
	// `setup` leaves its request beside the app's data, and may come with
	// the app's first start, so its folder is made; any other command makes
	// none, and says so where the app has no data of its own.
	locate := localDataPath
	if len(args) > 0 && args[0] == "setup" {
		locate = localDataDir
	}
	dataDir, err := locate()
	if err != nil {
		return commandline.Failed(err)
	}
	product := app.NewWithDataDir(dataDir)
	describeProgram(product)
	run := func() error { return app.Execute(product) }
	// `setup` opens no data: it leaves its request beside the app's.
	if len(args) > 0 && args[0] == "setup" {
		app.UseSetupApp(app.SetupApp{Choice: setupChoice, SignedIn: setupSignedIn, Program: typedProgram()})
		product.RootCmd.SetArgs(args)
		run = product.RootCmd.Execute
	}
	// The command's own words for what went wrong, as `leafen` says them:
	// the agent running it reports them (docs/install.md).
	if err := run(); err != nil {
		var none *app.NoServerDataError
		if errors.As(err, &none) {
			err = fmt.Errorf("no Leafen data of the app's own on this computer, in %s: open the Leafen app and start it on this "+
				"computer first — or, where it logs in to a Leafen elsewhere, run the command on that server; "+
				"--dir names another data folder", none.Dir)
		}
		return commandline.Failed(err)
	}
	return 0
}
