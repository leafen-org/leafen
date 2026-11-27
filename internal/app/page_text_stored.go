package app

import (
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A block moved or copied on the server leaves with the text the server has
// stored, and what was typed a moment before may still be on its way there
// over the page's socket (2026-11-05: a line moved a second after typing it
// lost the words, and one a second after clearing its words brought them back).
// The client names what its copy of the page holds; this answers once the stored
// text holds all of it.
const (
	pageTextStoredPath    = "/pages/{id}/text-stored"
	pageTextStoredWait    = 15 * time.Second
	pageTextStoredMaxPoll = 500 * time.Millisecond
)

func registerPageTextStoredRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+pageTextStoredPath, waitForPageTextStored)
}

// storedTextHolds says whether the stored text has all a copy holds: what it
// wrote and what it took away, which only its delete set knows.
func storedTextHolds(document *crdt.Doc, held *crdt.Snapshot) bool {
	stored := crdt.CaptureSnapshot(document)
	for client, clock := range held.StateVector {
		if stored.StateVector.Clock(client) < clock {
			return false
		}
	}
	with := crdt.CaptureSnapshot(document)
	with.DeleteSet.Merge(held.DeleteSet)
	return crdt.EqualSnapshots(with, stored)
}

func waitForPageTextStored(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("id"))
	if _, err := authorizePage(event.App, user.Id, pageID, permissionWorkspaceRead); err != nil {
		return writeGraphError(event, graphNotFoundError(), "read", errorCodeGraphReadFailed, "Could not read the page.")
	}
	input := struct {
		State string `json:"state"`
	}{}
	_ = event.BindBody(&input)
	if input.State == "" {
		return refuseMissingInput(event, errorCodeInvalidInput, "state", "the page's text state as the screen's editor holds it, in base64.")
	}
	raw, err := base64.StdEncoding.DecodeString(input.State)
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the page's state.")
	}
	held, err := crdt.DecodeSnapshot(raw)
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the page's state.")
	}
	deadline := time.Now().Add(pageTextStoredWait)
	for poll := 50 * time.Millisecond; ; poll = min(poll*2, pageTextStoredMaxPoll) {
		document, _, err := loadYgoDoc(event.App, pageID)
		if err != nil {
			return writeGraphError(event, err, "read", errorCodeGraphReadFailed, "Could not read the page.")
		}
		stored := storedTextHolds(document, held)
		if stored || time.Now().After(deadline) {
			event.Response.Header().Set("Cache-Control", "no-store")
			return event.JSON(http.StatusOK, map[string]bool{"stored": stored})
		}
		select {
		case <-event.Request.Context().Done():
			return event.Request.Context().Err()
		case <-time.After(poll):
		}
	}
}
