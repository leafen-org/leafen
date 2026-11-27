// Command leafen is Leafen's server without the desktop app's window, as the
// Docker image runs it and as Leafen runs on Linux, which has no desktop app:
// `leafen serve --http=… --dir=…` and the server's other commands, and the
// app worker the app platform starts this executable again as. It links no
// window libraries, so the image carries none; the desktop app (desktop/)
// runs the same server beside its window.
package main

import (
	"os"

	"leafen.org/leafen/internal/app"
	"leafen.org/leafen/internal/commandline"
	"leafen.org/leafen/internal/netproxy"
)

func main() {
	os.Exit(run())
}

// run runs the command line and says how it ended, once its output is all
// written (commandline.Prepare).
func run() int {
	// Requests out go by the environment's proxy, as the desktop app's do
	// where the person set none.
	netproxy.Install()
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "app-worker" {
		return app.RunAppWorker(os.Stdin, os.Stdout)
	}
	// Typed in a terminal, it writes there as the desktop app's program
	// does: on Windows, PowerShell reads what it pipes in the console's code
	// page (internal/commandline).
	finish := commandline.Prepare(args)
	defer finish()
	if len(args) > 0 && args[0] == "app" {
		return app.RunAppCommand(args[1:])
	}
	if err := app.Execute(app.New()); err != nil {
		return commandline.Failed(err)
	}
	return 0
}
