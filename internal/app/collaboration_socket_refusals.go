package app

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A refused live page connection is noted in the server's output as any
// refused request is (logDiagnostic's shape: a stable code, a diagnostic
// reference, the operation and stage), so an administrator can see why a
// page doesn't connect again (2026-11-11: a person over the socket limit was
// refused with 429 and nothing was written). A page asks again every few
// seconds, so each person (or address, before one is known) and reason is
// noted at most once a minute, with how many more were refused meanwhile.
type collaborationRefusalLog struct {
	mu    sync.Mutex
	notes map[string]collaborationRefusalNote
}

type collaborationRefusalNote struct {
	at   time.Time
	more int
}

const collaborationRefusalLogEvery = time.Minute

const collaborationRefusalSource = "internal/app/collaboration_socket_refusals.go"

// note writes one refusal, or counts it toward the next when the same one
// was written within the minute.
func (log *collaborationRefusalLog) note(app core.App, request *http.Request, person string, status int, code, stage string) {
	if app == nil {
		return
	}
	who := person
	if who == "" && request != nil {
		who, _, _ = net.SplitHostPort(request.RemoteAddr)
		if who == "" {
			who = request.RemoteAddr
		}
	}
	key := who + "\x00" + code + "\x00" + stage
	now := time.Now()
	log.mu.Lock()
	if log.notes == nil {
		log.notes = map[string]collaborationRefusalNote{}
	}
	last, seen := log.notes[key]
	if seen && now.Sub(last.at) < collaborationRefusalLogEvery {
		last.more++
		log.notes[key] = last
		log.mu.Unlock()
		return
	}
	log.notes[key] = collaborationRefusalNote{at: now}
	for other, note := range log.notes {
		if now.Sub(note.at) > 10*collaborationRefusalLogEvery {
			delete(log.notes, other)
		}
	}
	log.mu.Unlock()
	attributes := []any{
		"diagnosticReference", newDiagnosticReference(),
		"code", safeDiagnosticToken(code),
		"operation", "collaboration.socket",
		"stage", safeDiagnosticToken(stage),
		"status", status,
		"sourcePosition", collaborationRefusalSource,
	}
	if seen && last.more > 0 {
		attributes = append(attributes, "refusedMeanwhile", last.more)
	}
	if status >= http.StatusInternalServerError {
		app.Logger().Error("leafen request failed", attributes...)
		if !app.IsDev() {
			productionDiagnosticLogger.Error("leafen request failed", attributes...)
		}
		return
	}
	app.Logger().Warn("leafen request failed", attributes...)
	if !app.IsDev() {
		productionDiagnosticLogger.Warn("leafen request failed", attributes...)
	}
}
