package app

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/security"
)

// While a start brings the data up to date — re-checking the schema a new
// build reads, keeping the data as it was, applying the migrations — the
// server isn't serving yet, and on a NAS that was minutes with no answer at
// all, which people and an update's script took for a hang (2026-11-22). So
// that time is told, in two ways:
//
//   - In this process, updateProgress names the step, for the desktop and
//     phone apps, whose window and update helper read it (UpdateStep).
//   - A server started by `leafen serve` on plain HTTP answers on its address
//     at once (migrationAnswer): health says "migrating" with the step, a
//     browser gets a page saying Leafen is updating, which opens Leafen once
//     it is done (the app's service worker lets it through, sw.js), and the
//     API says the server is away for a moment (503, which the app asks
//     again after, apiTransport.ts). Its socket is the server's own:
//     PocketBase is handed it as it begins to serve (ServeEvent.Listener),
//     and the answer stops only as the server takes its first connection, so
//     no moment goes unanswered and no two listeners race for the port.
//     Every answer closes its connection, so the next request reaches the
//     server.
//
// An update that fails turns the answer into "failed" — health says so, the
// page says it didn't finish —, and `leafen serve` goes on saying so with the
// way back until it is stopped (schema_migration_failure.go); elsewhere, when
// anyone asked meanwhile, the process keeps saying so for a short while
// before it stops with the error.

const (
	updateStepChecking  = "checking"
	updateStepKeeping   = "keeping"
	updateStepMigrating = "migrating"
	updateStepStarting  = "starting"

	errorCodeServerUpdating = "LEAFEN_SERVER_UPDATING"
	migrationAnswerRetry    = "2"
	// migrationFailureShown is how long a failed update goes on answering so
	// once someone asked.
	migrationFailureShown = 15 * time.Second
)

// updateProgress is the update this process applies now.
var updateProgress = struct {
	mu   sync.Mutex
	step string
}{}

// UpdateStep is the step of the update this process applies to its data now,
// "" when none: the desktop and phone apps say "updating" while it is one.
func UpdateStep() string {
	updateProgress.mu.Lock()
	defer updateProgress.mu.Unlock()
	return updateProgress.step
}

// setUpdateStep names the step of the update under way; with none under
// way (a new data folder taking its schema) it does nothing.
func setUpdateStep(step string) {
	updateProgress.mu.Lock()
	if updateProgress.step == "" {
		updateProgress.mu.Unlock()
		return
	}
	updateProgress.step = step
	updateProgress.mu.Unlock()
	if answer := currentMigrationAnswer(); answer != nil {
		answer.setStep(step)
	}
}

// beginUpdateSteps tells the update a start brings the data, from schema
// from up to to, and returns what ends it once the migrations' run returns
// with *err: a failure turns the answer into one; a run that passed leaves
// it answering "starting" until the server serves.
func beginUpdateSteps(from, to int, step string, err *error) func() {
	updateProgress.mu.Lock()
	updateProgress.step = step
	updateProgress.mu.Unlock()
	answer := startMigrationAnswer(from, to, step)
	return func() {
		updateProgress.mu.Lock()
		updateProgress.step = ""
		updateProgress.mu.Unlock()
		if answer == nil {
			return
		}
		if *err != nil {
			answer.fail(*err)
			return
		}
		answer.setStep(updateStepStarting)
	}
}

// serveEarlyAddress is the address `leafen serve` listens on, kept by
// Execute when it serves plain HTTP (no --https, no domains), "" otherwise.
var serveEarlyAddress string

var migrationAnswers = struct {
	mu      sync.Mutex
	current *migrationAnswer
}{}

func currentMigrationAnswer() *migrationAnswer {
	migrationAnswers.mu.Lock()
	defer migrationAnswers.mu.Unlock()
	return migrationAnswers.current
}

type migrationAnswer struct {
	socket   *net.TCPListener
	early    *earlyListener
	server   *http.Server
	from, to int
	since    time.Time
	asked    atomic.Bool
	// served closes once the answer's server stopped taking connections.
	served chan struct{}

	mu     sync.Mutex
	step   string
	failed string
}

// startMigrationAnswer answers on the serve address while the data is
// brought from schema from up to to; nil where this start serves no address
// (another command, the apps, HTTPS) or it can't be listened on.
func startMigrationAnswer(from, to int, step string) *migrationAnswer {
	if !serveCommandLine || serveEarlyAddress == "" {
		return nil
	}
	listener, err := net.Listen("tcp", serveEarlyAddress)
	if err != nil {
		return nil
	}
	socket, ok := listener.(*net.TCPListener)
	if !ok {
		_ = listener.Close()
		return nil
	}
	answer := &migrationAnswer{socket: socket, early: &earlyListener{TCPListener: socket}, from: from, to: to, since: time.Now(), step: step}
	answer.server = &http.Server{Handler: answer, ReadHeaderTimeout: 10 * time.Second}
	answer.served = make(chan struct{})
	go func() {
		_ = answer.server.Serve(answer.early)
		close(answer.served)
	}()
	migrationAnswers.mu.Lock()
	migrationAnswers.current = answer
	migrationAnswers.mu.Unlock()
	fmt.Fprintf(os.Stderr, "leafen: answering on %s while the data is brought up to date\n", serveEarlyAddress)
	return answer
}

func (answer *migrationAnswer) setStep(step string) {
	answer.mu.Lock()
	answer.step = step
	answer.mu.Unlock()
}

// fail turns the answer into the update's failure and, when anyone asked,
// goes on saying so for a moment; then it stops answering. A serve's noted
// failure goes on until the server is stopped (waitOnFailedUpdate).
func (answer *migrationAnswer) fail(err error) {
	code := migrationFailureCode(err)
	answer.mu.Lock()
	answer.failed = code
	answer.mu.Unlock()
	// A serve whose update failed goes on saying so, with the way back,
	// until it is stopped (schema_migration_failure.go).
	if holdFailedUpdate && currentUpdateFailure() != nil {
		return
	}
	if answer.asked.Load() {
		fmt.Fprintf(os.Stderr, "leafen: the update didn't finish (%s); %s says so for %d s before this server stops\n",
			code, serveEarlyAddress, int(migrationFailureShown.Seconds()))
		time.Sleep(migrationFailureShown)
	}
	answer.close()
}

func (answer *migrationAnswer) close() {
	migrationAnswers.mu.Lock()
	if migrationAnswers.current == answer {
		migrationAnswers.current = nil
	}
	migrationAnswers.mu.Unlock()
	_ = answer.server.Close()
	_ = answer.socket.Close()
}

// handOver gives the socket to the server that begins to serve on it. The
// answer goes on until the server first takes a connection — its routes are
// built after the last start hook, which took seconds on a slow CPU — then
// stops taking them, finishes those under way, and the next connection is
// the server's.
func (answer *migrationAnswer) handOver() net.Listener {
	migrationAnswers.mu.Lock()
	if migrationAnswers.current == answer {
		migrationAnswers.current = nil
	}
	migrationAnswers.mu.Unlock()
	return &handedOverListener{answer: answer}
}

type handedOverListener struct {
	answer  *migrationAnswer
	stopped sync.Once
}

func (listener *handedOverListener) Accept() (net.Conn, error) {
	listener.stopped.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = listener.answer.server.Shutdown(ctx)
		// Its Accept has returned before the socket waits again.
		select {
		case <-listener.answer.served:
		case <-ctx.Done():
		}
		_ = listener.answer.socket.SetDeadline(time.Time{})
	})
	return listener.answer.socket.Accept()
}

func (listener *handedOverListener) Close() error   { return listener.answer.socket.Close() }
func (listener *handedOverListener) Addr() net.Addr { return listener.answer.socket.Addr() }

// registerMigrationAnswerHandOver gives the answer's socket to the server
// as the last part of its start, before it serves.
func registerMigrationAnswerHandOver(app core.App) {
	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{
		Id:       "leafenMigrationAnswerHandOver",
		Priority: 1 << 20,
		Func: func(event *core.ServeEvent) error {
			if answer := currentMigrationAnswer(); answer != nil && event.Listener == nil {
				event.Listener = answer.handOver()
			}
			return event.Next()
		},
	})
}

// earlyListener is the answer's view of the server's socket: closing it
// stops the answer taking connections and leaves the socket open.
type earlyListener struct {
	*net.TCPListener
	stopped atomic.Bool
}

func (listener *earlyListener) Accept() (net.Conn, error) {
	conn, err := listener.TCPListener.Accept()
	if err != nil && listener.stopped.Load() {
		return nil, net.ErrClosed
	}
	return conn, err
}

func (listener *earlyListener) Close() error {
	listener.stopped.Store(true)
	// Ends an Accept under way without closing the socket.
	return listener.TCPListener.SetDeadline(time.Now())
}

func (answer *migrationAnswer) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	answer.asked.Store(true)
	answer.mu.Lock()
	step, failed := answer.step, answer.failed
	answer.mu.Unlock()
	header := response.Header()
	// The app's service worker shows the page rather than its kept shell
	// (web/public/sw.js).
	header.Set("X-Leafen-Updating", step)
	if failed != "" {
		header.Set("X-Leafen-Updating", "failed")
	}
	header.Set("Connection", "close")
	header.Set("Cache-Control", "no-store")
	header.Set("Retry-After", migrationAnswerRetry)
	// The desktop and phone apps' own page, on another origin, reads the
	// answer as it reads the server's (packagedClientOrigins): its sign-in
	// screen says Leafen is updating, not that the server is away
	// (2026-11-23).
	if origin := request.Header.Get("Origin"); isPackagedClientOrigin(origin) {
		header.Set("Access-Control-Allow-Origin", origin)
		header.Add("Vary", "Origin")
	}
	path := request.URL.Path
	if path == apiRoot+"/health" {
		body := map[string]any{"status": "migrating", "step": step, "fromSchema": answer.from, "toSchema": answer.to,
			"since": answer.since.UTC().Format(time.RFC3339), "generation": 2}
		if failed != "" {
			body["status"], body["code"] = "failed", failed
			if failure := currentUpdateFailure(); failure != nil {
				addUpdateFailure(body, failure)
			}
		}
		writeMigrationAnswerJSON(response, body)
		return
	}
	if strings.HasPrefix(path, "/api/") {
		message := "Leafen is updating; try again in a moment."
		code := errorCodeServerUpdating
		if failed != "" {
			message, code = "Leafen couldn’t finish updating.", failed
		}
		writeMigrationAnswerJSON(response, map[string]any{"error": map[string]any{"code": code, "message": message}})
		return
	}
	writeMigrationAnswerPage(response, request, failed != "", currentUpdateFailure())
}

func writeMigrationAnswerJSON(response http.ResponseWriter, body map[string]any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(response).Encode(body)
}

// The page's words, English and Korean.
var migrationAnswerWords = map[string][2]string{
	"updating":     {"Leafen is updating", "리펜을 업데이트하는 중입니다"},
	"updatingBody": {"This page opens Leafen once the update is done.", "업데이트가 끝나면 이 페이지에서 리펜이 열립니다."},
	"failed":       {"Leafen couldn’t finish updating", "리펜 업데이트를 마치지 못했습니다"},
	"failedBody":   {"The person who runs this server can see why in the server log.", "이 서버를 운영하는 사람이 서버 로그에서 이유를 볼 수 있습니다."},
}

// writeMigrationAnswerPage is the page a browser gets: Leafen is updating,
// and it reloads once health answers; or the update didn't finish, with the
// way back when the failure is noted.
func writeMigrationAnswerPage(response http.ResponseWriter, request *http.Request, failed bool, failure *updateFailure) {
	language := 0
	if strings.HasPrefix(strings.ToLower(request.Header.Get("Accept-Language")), "ko") {
		language = 1
	}
	word := func(key string) string { return migrationAnswerWords[key][language] }
	title, body := word("updating"), word("updatingBody")
	if failed {
		title, body = word("failed"), word("failedBody")
	}
	nonce := security.RandomString(24)
	header := response.Header()
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Content-Security-Policy", "default-src 'none'; style-src 'nonce-"+nonce+"'; script-src 'nonce-"+nonce+
		"'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	lang := [2]string{"en", "ko"}[language]
	page := `<!doctype html><html lang="` + lang + `"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Leafen</title>` +
		`<style nonce="` + nonce + `">body{margin:96px auto;max-width:400px;padding:0 16px;font:14px/1.5 -apple-system,system-ui,sans-serif;color:#37352f;background:#fff;text-align:center}` +
		`h1{font-size:16px;font-weight:600;margin:0 0 8px}p{margin:0;color:#787774;overflow-wrap:anywhere}p+p{margin-top:8px}` +
		`@media (prefers-color-scheme:dark){body{color:#d4d4d4;background:#191919}p{color:#9b9b9b}}</style></head><body>` +
		`<h1 id="title">` + html.EscapeString(title) + `</h1><p id="body">` + html.EscapeString(body) + `</p>`
	if failed && failure != nil {
		page += `<p id="way">` + html.EscapeString(failure.wayBack(language)) + `</p>`
	}
	if !failed {
		// It asks health every two seconds: open once it answers, the
		// failure's words once it says the update failed — the page again,
		// with the way back, where the server goes on saying so.
		page += `<script nonce="` + nonce + `">(function(){var t=document.getElementById("title"),b=document.getElementById("body");` +
			`function ask(){fetch("` + apiRoot + `/health",{cache:"no-store"}).then(function(r){return r.json().then(function(j){` +
			`if(r.ok||j&&j.wayBack){location.reload();return}if(j&&j.status==="failed"){t.textContent=` + jsString(word("failed")) + `;b.textContent=` + jsString(word("failedBody")) + `;return}` +
			`setTimeout(ask,2000)})}).catch(function(){setTimeout(ask,2000)})}setTimeout(ask,2000)})()</script>`
	}
	page += `</body></html>`
	response.WriteHeader(http.StatusServiceUnavailable)
	_, _ = response.Write([]byte(page))
}
