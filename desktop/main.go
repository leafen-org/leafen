// Command leafen is Leafen, one program (the one-program decision of
// 2026-10-27): on a computer the Leafen server, running inside the app with
// the app's own data on the loopback interface, and the React product in
// the app's window; without a window a server's command line
// (server_command.go) — the Docker image carries that server alone
// (cmd/leafen); on a phone the same, its own server Leafen tried alone on
// the phone (the trial of 2026-10-20), with no app platform.
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"leafen.org/leafen/internal/netproxy"
)

func main() {
	// The server inside runs by its command line (cobra), whose Windows guard
	// ends a program Explorer started — a double-click, the Start menu, the
	// Microsoft Store's app — as a console tool started by mistake, five
	// seconds into the command. The app is a window; the guard stays off.
	cobra.MousetrapHelpText = ""
	// Requests out go by the proxy in force: the person's, set in This
	// computer, else the environment's or the system's.
	netproxy.Install()
	// An app worker is this executable started again for one app's calls
	// (the app platform starts it); it opens no window and no data.
	if code, ok := runAppWorker(os.Args[1:]); ok {
		os.Exit(code)
	}
	// A server's command line, without a window, on the app's own data
	// unless --dir names another: Leafen is one program (server_command.go).
	if code, ok := runServerCommand(os.Args[1:]); ok {
		os.Exit(code)
	}
	// The updater's helper is this executable started again to switch
	// versions once the app has ended (update_helper.go); only the Mac app
	// switches versions itself.
	if len(os.Args) > 1 && os.Args[1] == "apply-update" {
		os.Exit(runUpdateHelper(os.Args[2:]))
	}
	updater := newUpdater()
	// A newer version made ready earlier starts now, through the helper, as
	// this start was asked: behind its window when opened at login, which
	// macOS tells only once the app has launched (below).
	handOff := updater.ReadyAtStart()
	if handOff && !windowWaitsForLaunch {
		if updater.HandOffAtStart(openedAtLogin()) {
			return
		}
		handOff = false
	}
	local := newLocalServer()
	firstRun := newFirstRun(local)
	local.idle = firstRun.idleState
	fit := &windowFit{}
	notices := newDesktopNotices()
	instance := singleInstance()
	desktop := application.New(application.Options{
		Name:        "Leafen",
		Description: "Leafen workspace",
		Assets:      application.AssetOptions{Handler: local.Client()},
		// The window keeps its sign-ins on other servers through the broker,
		// saves files onto the device through the saver, picks folders for
		// its own server's backups and shows the person's notifications
		// through the notices; it takes the links the app is opened with, and
		// starts its server again to finish a restore.
		Services: append([]application.Service{
			application.NewService(newCredentialBroker()),
			application.NewService(newFileSaver()),
			application.NewService(newFolderChooser()),
			application.NewService(newServerKeys()),
			application.NewService(newSignInReturn()),
			application.NewService(newAppearance()),
			application.NewService(fit),
			application.NewService(notices),
			application.NewService(links),
			application.NewService(&ServerRestarter{local: local}),
			application.NewService(&StartupRecovery{local: local}),
			application.NewService(&LocalBrowser{local: local}),
			application.NewService(updater),
			application.NewService(firstRun),
			application.NewService(newCodeScanner()),
			application.NewService(newDeviceOwner(local)),
		}, append(append(notices.services(), windowServices()...), tabServices()...)...),
		SingleInstance: instance,
		// A Mac or Windows window's strip of tabs tells the app what the
		// person does there (window_tabs_strip.go).
		RawMessageHandler: windowMessage,
		// Closing the window ends the app unless it serves in the
		// background (background_desktop.go), which decides.
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		// An iPhone's or iPad's video plays in its block with its own
		// controls, as in Notion's app; WebKit there otherwise plays every
		// video only in full screen.
		IOS: application.IOSOptions{
			EnableInlineMediaPlayback: true,
		},
		OnShutdown:   local.stop,
		ErrorHandler: appErrors(local),
	})
	// The app's log goes to its file too, for the startup recovery screen.
	openStartupLog()
	if err := applyAppProxy(); err != nil {
		log.Printf("network proxy: couldn't apply the one set here: %v", err)
	}
	// A second start has handed its link to the running app and ended in
	// New, before reaching the data; this one is the app for it. Its own
	// server starts when it has data or this computer was chosen to serve; a
	// first start asks first (first_run.go).
	if firstRun.startsLocal() && !handOff {
		firstRun.startLocal()
	}
	if desktopUpdater, ok := any(updater).(interface{ setApp(*application.App) }); ok {
		desktopUpdater.setApp(desktop)
	}
	go updater.Run(context.Background())
	// The version a helper waits on says it came up once its server answers,
	// or at once when it runs no server of its own (a terminal of another, or a
	// first start still choosing).
	go markHealthyWhenUp(updater, firstRun, local)
	// Tabs kept on a page a restore took away open home (window_tabs_restored.go).
	go forgetGoneTabPagesWhenUp(local)
	// Opened at login, it starts behind its window (background_desktop.go):
	// Windows says so before the window is made, macOS as the app finishes
	// launching (login_launch_darwin.go).
	watchLoginLaunch()
	// A Mac's and Windows' windows hold tabs and open as the app last left
	// them (window_tabs_frames.go); a phone's one window opens on the start
	// page.
	if tabsInWindows {
		fit.window = firstWindow(desktop)
	} else {
		options := appWindowOptions("/")
		options.Hidden = windowWaitsForLaunch || openedAtLogin()
		fit.window = desktop.Window.NewWithOptions(options)
		// What its page asks to open in a new window or tab goes by the
		// app's rule (new_window.go).
		routeNewWindows(fit.window)
	}
	notices.window = fit.window
	// While this computer serves, closing the window leaves the app running.
	if handOff {
		// macOS: the window stays hidden while the new version takes over,
		// and the app goes on as it would have when the helper can't start.
		desktop.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
			if updater.HandOffAtStart(openedAtLogin()) {
				desktop.Quit()
				return
			}
			if firstRun.startsLocal() {
				firstRun.startLocal()
			}
			if !openedAtLogin() {
				application.InvokeAsync(func() { fit.window.Show() })
			}
		})
	} else {
		showUnlessOpenedAtLogin(fit.window)
	}
	startBackground(desktop, fit.window, local, openedAtLogin)
	if instance != nil {
		links.window = fit.window
	}
	desktop.Event.OnApplicationEvent(events.Common.ApplicationLaunchedWithUrl, launchedWithLink)
	err := desktop.Run()
	// Windows ends the app when its last window closes without running the
	// shutdown hooks, so the server also ends here; it ends only once.
	local.stop()
	if err != nil {
		log.Fatal(err)
	}
}

// appErrors logs the app's errors and, before one ends the app, tells the
// person why and ends the server.
func appErrors(local *localServer) func(error) {
	return func(err error) {
		log.Print(err)
		var fatal *application.FatalError
		if errors.As(err, &fatal) || webviewRuntimeMissing(err) {
			reportFatal(err)
			local.stop()
		}
	}
}

// webviewRuntimeMissing says whether the error is the window failing on
// Windows for want of the WebView2 runtime, after which the app ends at once.
func webviewRuntimeMissing(err error) bool {
	return strings.Contains(err.Error(), "no webview2 found")
}

// safariUserAgentName ends the Mac app's user agent as Safari's on this Mac
// does. Wails' default end, "wails.io", reads as no browser a video site knows,
// and YouTube's player in the app stayed on its picture, never playing
// (seen 2026-11-05).
func safariUserAgentName() string {
	version := "18.0"
	if plist, err := os.ReadFile("/Applications/Safari.app/Contents/Info.plist"); err == nil {
		text := string(plist)
		if at := strings.Index(text, "<key>CFBundleShortVersionString</key>"); at >= 0 {
			rest := text[at:]
			if start := strings.Index(rest, "<string>"); start >= 0 {
				if end := strings.Index(rest[start:], "</string>"); end > len("<string>") {
					version = strings.TrimSpace(rest[start+len("<string>") : start+end])
				}
			}
		}
	}
	return "Version/" + version + " Safari/605.1.15"
}
