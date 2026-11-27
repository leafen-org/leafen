package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"

	"leafen.org/leafen/internal/app"
	"leafen.org/leafen/web"
)

// localServer is the Leafen server the app runs for itself: the same server
// a Leafen instance runs, with the app's own data, listening only on the
// loopback interface, at the port it had before when it is still free so
// the window keeps what it keeps for its address. It starts behind the
// app's window, which waits for it on the app's own page.
type localServer struct {
	// While the server is off (a first start not yet chosen, or a terminal of
	// another server), what the window's local profile says (first_run.go).
	idle func() string
	// mu guards the fields below, which a restart replaces.
	mu sync.RWMutex
	// started says the server was asked to start: one never asked has
	// nothing to stop, so the app ends at once.
	started  bool
	ready    chan struct{} // closed once the server answers or couldn't start
	err      error         // why it couldn't, once ready is closed
	product  *pocketbase.PocketBase
	owner    *app.LocalOwner
	listener net.Listener
	done     chan error
	stopping *sync.Once
	URL      string
}

// localServerSettings is what the app remembers of its server beside the
// server's data.
type localServerSettings struct {
	Port int `json:"port"`
}

const localServerSettingsFile = "desktop.json"

// listenLocal listens on the loopback interface at the port the app had,
// or at one the system chooses, remembered for next time.
func listenLocal(dataDir string) (net.Listener, error) {
	path := filepath.Join(dataDir, localServerSettingsFile)
	settings := localServerSettings{}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &settings)
	}
	if settings.Port > 0 && settings.Port < 65536 {
		if listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", settings.Port)); err == nil {
			return listener, nil
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	settings.Port = listener.Addr().(*net.TCPAddr).Port
	if raw, err := json.Marshal(settings); err == nil {
		_ = os.WriteFile(path, raw, 0o600)
	}
	return listener, nil
}

// placeholderUserNames are the names a system gives an account that are
// no one's: a phone's ("mobile", Android's "u0_a123") and an administrator's.
var placeholderUserNames = regexp.MustCompile(`(?i)^(admin|administrator|root|user|owner|guest|mobile|leafen|u\d+_a\d+)$`)

// ownerName is the name the first setup suggests in the app's window: the
// person's name on this device, when it has one that is theirs. The window
// suggests a friendly one otherwise.
func ownerName() string {
	current, err := user.Current()
	if err != nil {
		return ""
	}
	for _, name := range []string{current.Name, current.Username} {
		if name = strings.TrimSpace(name); name != "" && !placeholderUserNames.MatchString(name) {
			return name
		}
	}
	return ""
}

func newLocalServer() *localServer {
	return &localServer{ready: make(chan struct{}), done: make(chan error, 1), stopping: new(sync.Once)}
}

// launch starts the server behind the caller.
func (local *localServer) launch() {
	local.mu.Lock()
	local.started = true
	local.mu.Unlock()
	go local.start()
}

// start starts the server and marks it ready once it answers, or says why
// it couldn't start.
func (local *localServer) start() {
	local.mu.RLock()
	ready := local.ready
	local.mu.RUnlock()
	err := local.run()
	if err != nil {
		dir, _ := localDataDir()
		reason := app.StartupFailureReason(dir, err)
		if reason == "" {
			reason = "other"
		}
		log.Printf("Leafen's own server couldn't start (%s): %v", reason, err)
	}
	local.mu.Lock()
	local.err = err
	local.mu.Unlock()
	close(ready)
}

// restart ends the server and starts it again on the same data, as a Leafen
// server starts itself again to finish a restore: its start finishes what
// was asked of it. It returns once the new server answers, or why it
// couldn't start.
func (local *localServer) restart() error {
	local.stop()
	local.mu.Lock()
	local.ready, local.done, local.stopping = make(chan struct{}), make(chan error, 1), new(sync.Once)
	local.err, local.product, local.owner, local.listener, local.URL = nil, nil, nil, nil, ""
	local.mu.Unlock()
	local.start()
	local.mu.RLock()
	err, product := local.err, local.product
	local.mu.RUnlock()
	if err == nil && product != nil {
		// Tabs kept on a page the restore took away open home (window_tabs_restored.go).
		forgetGoneTabPages(product)
	}
	return err
}

// current is the server as it is now, for those reading it while a restart
// may replace it.
func (local *localServer) current() (ready chan struct{}, err error, url string, owner *app.LocalOwner) {
	local.mu.RLock()
	defer local.mu.RUnlock()
	return local.ready, local.err, local.URL, local.owner
}

func (local *localServer) run() error {
	dataDir, err := localDataDir()
	if err != nil {
		return err
	}
	log.Printf("Leafen %s starts its own server on %s", app.ReleaseVersion(), dataDir)
	socket, err := listenLocal(dataDir)
	if err != nil {
		return err
	}
	listener := net.Listener(newReopeningListener(socket))
	// A phone's backup folder opens through its document providers.
	registerDeviceBackupFolders()
	product := app.NewWithDataDir(dataDir)
	origin := "http://" + listener.Addr().String()
	owner, err := app.EnableLocalOwner(product, origin)
	if err != nil {
		_ = listener.Close()
		return err
	}
	// A screen the server opens for the person — an agent's `leafen setup
	// notion`, or its wait for the Notion token — comes to the front in the
	// window, as a link handed to the app does (links.go): the window is
	// signed in, where a browser isn't (2026-11-19).
	owner.ShowsScreens(func(path string) { links.opened(origin + path) })
	product.OnServe().BindFunc(func(event *core.ServeEvent) error {
		event.Listener = listener
		return event.Next()
	})
	// The serve command runs by itself rather than through Execute, which
	// would answer the system's quit signals too: the app answers them, and
	// stop ends the server once.
	if err := product.Bootstrap(); err != nil {
		// What opened lets go of the data, for trying again or a restore.
		_ = product.ClearBootstrap()
		_ = listener.Close()
		return err
	}
	local.mu.Lock()
	local.product, local.owner, local.listener, local.URL = product, owner, listener, origin+"/"
	done := local.done
	local.mu.Unlock()
	product.RootCmd.SetArgs([]string{"serve", "--http", listener.Addr().String(), "--setup-open", "print"})
	go func() { done <- product.RootCmd.Execute() }()
	return local.waitReady(done, origin+"/", 60*time.Second)
}

// Client is the app's own page, which only the app's window loads: the
// product's web build, opened at once, and the local profile it asks for —
// the app's server; while it isn't set up, the code that opens its first
// setup and the name to suggest; while an earlier version's owner waits for
// the window to keep a sign-in, the capability that signs it in — which says
// whether the server is still starting or why it couldn't start. It always
// answers 200: Android's window takes no other answer from the app's page.
func (local *localServer) Client() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(localProfilePath, func(response http.ResponseWriter, _ *http.Request) {
		body := map[string]string{}
		if local.idle != nil {
			if state := local.idle(); state != "" {
				body["state"] = state
				response.Header().Set("Cache-Control", "no-store")
				response.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(response).Encode(body)
				return
			}
		}
		ready, _, _, _ := local.current()
		select {
		case <-ready:
			// Read again once ready: a restart may have begun meanwhile.
			if _, err, url, owner := local.current(); err != nil {
				// With why, for the startup recovery screen, and the last
				// backup it may go back to (startup_recovery.go).
				body["error"] = "Leafen couldn't start: " + err.Error()
				if dir, dirErr := localDataDir(); dirErr == nil {
					body["reason"] = app.StartupFailureReason(dir, err)
					if made, ok := app.LastBackup(dir); ok {
						body["backupAt"] = strconv.FormatInt(made.UnixMilli(), 10)
					}
				}
			} else if owner == nil {
				body["state"] = "starting"
			} else {
				body["server"] = strings.TrimSuffix(url, "/")
				local.mu.RLock()
				product := local.product
				local.mu.RUnlock()
				if product != nil {
					if code := owner.SetupCode(product); code != "" {
						body["setupCode"] = code
						body["name"] = ownerName()
					}
					if owner.Waiting(product) {
						body["capability"] = owner.Capability()
					}
				}
			}
		default:
			// While it brings its data up to date the window says Leafen is
			// updating, not slow to start (app.UpdateStep).
			body["state"] = "starting"
			if app.UpdateStep() != "" {
				body["state"] = "updating"
			}
		}
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(body)
	})
	// A Mac window's strip of tabs is the app's own page (window_tabs_strip.go).
	if strip := tabStripFiles(); strip != nil {
		mux.Handle("/desktop-tabs/", strip)
	}
	mux.Handle("/", singlePageFiles(web.Dist()))
	return mux
}

// waitReady waits until the server answers its health check.
func (local *localServer) waitReady(done chan error, url string, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	client := http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			done <- err
			return errors.Join(errors.New("the local server stopped while starting"), err)
		default:
		}
		response, err := client.Get(url + "api/leafen/v1/health")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("the local server didn't start in time")
}

// stop ends the server as a quit signal ends a Leafen server: the product
// settles its documents and work, the HTTP server closes, and its data
// closes last. A server still starting is waited for first.
func (local *localServer) stop() {
	local.mu.RLock()
	stopping, ready, started := local.stopping, local.ready, local.started
	local.mu.RUnlock()
	if !started {
		return
	}
	stopping.Do(func() {
		select {
		case <-ready:
		case <-time.After(70 * time.Second):
			return
		}
		// The app's own readers of its server (the tray, the device owner's
		// sign-in) find none from here on, never one whose data is closing.
		local.mu.Lock()
		product, listener, done := local.product, local.listener, local.done
		local.product = nil
		local.mu.Unlock()
		if product == nil {
			return
		}
		event := new(core.TerminateEvent)
		event.App = product
		_ = product.OnTerminate().Trigger(event, func(event *core.TerminateEvent) error {
			return event.App.ClearBootstrap()
		})
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
		}
	})
}

// ServerRestarter lets the app's window start the app's own server again,
// as a Leafen server starts itself again to finish a restore.
type ServerRestarter struct {
	local *localServer
}

// Restart starts the server again on the same data and port and returns once
// it answers; the window then opens afresh.
func (restarter *ServerRestarter) Restart() error {
	return restarter.local.restart()
}

// runAppWorker runs this executable as one app's worker, when started so.
func runAppWorker(args []string) (int, bool) {
	if len(args) > 0 && args[0] == "app-worker" {
		return app.RunAppWorker(os.Stdin, os.Stdout), true
	}
	return 0, false
}

// updateProgressEvery is how often a version whose server brings its data
// up to date tells the update helper so.
const updateProgressEvery = 5 * time.Second

// markHealthyWhenUp says the version came up once the app's own server
// answers, or at once when it runs none.
func markHealthyWhenUp(updater *Updater, firstRun *FirstRun, local *localServer) {
	if !firstRun.startsLocal() {
		updater.MarkHealthy()
		return
	}
	ready, _, _, _ := local.current()
	// While the server brings its data up to date, the update helper is told
	// it is still coming up, so a long update isn't taken for a version that
	// doesn't start (update_helper.go).
	tick := time.NewTicker(updateProgressEvery)
	defer tick.Stop()
	for waiting := true; waiting; {
		select {
		case <-ready:
			waiting = false
		case <-tick.C:
			if app.UpdateStep() != "" {
				updater.MarkUpdating()
			}
		}
	}
	if _, err, _, _ := local.current(); err == nil {
		updater.MarkHealthy()
	}
}
