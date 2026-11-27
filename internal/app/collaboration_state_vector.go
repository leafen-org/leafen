package app

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A page's collaboration state as the server stores it: the state vector of
// its stored document, so a device can tell whether the server already has
// its own text edits before it forgets them, as at sign-out; whether a
// device's copy holds anything — text or deletions — the stored document
// does not, so edits the server refused are never let go in silence; and
// what the stored document holds that a device's copy lacks, which a page
// kept for offline use takes while the device is connected.

const (
	collaborationStateVectorPath  = apiRoot + "/collaboration-state/{room}"
	collaborationStateLacksPath   = apiRoot + "/collaboration-state/{room}/lacks"
	collaborationStateMissingPath = apiRoot + "/collaboration-state/{room}/missing"
	// A device's difference from the stored document, sent as Yjs update bytes.
	collaborationLacksMaxBodyBytes = 16 << 20
	// A device copy's state vector, a few bytes for each of its writers.
	collaborationMissingMaxBodyBytes = 64 << 10
)

type collaborationStateLacksResponse struct {
	Lacks bool `json:"lacks"`
}

type collaborationStateVectorResponse struct {
	StateVector string `json:"stateVector"`
}

func registerCollaborationStateVectorRoute(event *core.ServeEvent) {
	event.Router.GET(collaborationStateVectorPath, readCollaborationStateVector)
	event.Router.POST(collaborationStateLacksPath, readCollaborationStateLacks)
	event.Router.POST(collaborationStateMissingPath, readCollaborationStateMissing)
	event.Router.POST(offlinePageMarksPath, readOfflinePageMarks)
}

// collaborationStateRoom is the room of a request from someone who may read
// it; when written, the refusal is already the response and err is its write.
func collaborationStateRoom(request *core.RequestEvent) (room string, written bool, err error) {
	user, ok := authenticatedUser(request)
	if !ok {
		return "", true, apiError(request, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	room = strings.TrimSpace(request.Request.PathValue("room"))
	if room == "" || len(room) > maxWorkspacePermissionIdentifierLength {
		return "", true, apiError(request, http.StatusBadRequest, errorCodeInvalidInput, "The page ID is invalid.")
	}
	_, err = authorizeCollaborationStateRoom(request.App, user.Id, room)
	if isPermissionDenied(err) {
		// One an operation holds a while is told held (page_held_failure_answer.go).
		return "", true, apiError(request, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	}
	return room, false, err
}

// authorizeCollaborationStateRoom says whether the person may read the page,
// or the database page, the room is, with the owner of a page's workspace;
// a database page's is left empty.
func authorizeCollaborationStateRoom(app core.App, userID, room string) (string, error) {
	rowPage, err := isDatabaseRowPageRoom(app, room)
	if err != nil {
		return "", err
	}
	if rowPage {
		_, _, err = authorizeDatabaseRowBodyRoom(app, userID, room)
		if errors.Is(err, errDatabaseRowPageNotFound) {
			err = permissionDenied()
		}
		return "", err
	}
	authorization, err := authorizePage(app, userID, room, permissionWorkspaceRead)
	return authorization.Workspace.OwnerID, err
}

// readCollaborationStateMissing answers what the page's text holds that the
// copy with the posted state vector lacks, as a Yjs update — as its open
// room holds it, else as stored (a room stores what it takes a moment later:
// read as stored, a page opened while someone typed drew their line some 80
// characters short, 2026-11-13), and as its reader may read it, as their
// room's socket sends it: a mention they can't open in marks
// (collaboration_reader_mentions.go). A page opening with no copy whose words
// don't come with its blocks reads them this way beside its room
// (collaboration.ts), and a page kept for offline use takes what it lacks
// (2026-11-13: both read the titles of mentions closed to them, which the
// socket marks).
func readCollaborationStateMissing(request *core.RequestEvent) error {
	room, written, err := collaborationStateRoom(request)
	if written {
		return err
	}
	var stateVector crdt.StateVector
	if err == nil {
		request.Request.Body = http.MaxBytesReader(request.Response, request.Request.Body, collaborationMissingMaxBodyBytes)
		encoded, readErr := io.ReadAll(request.Request.Body)
		if readErr != nil {
			return apiError(request, http.StatusBadRequest, errorCodeInvalidInput, "The state vector is invalid.")
		}
		if stateVector, readErr = crdt.DecodeStateVectorV1(encoded); readErr != nil {
			return apiError(request, http.StatusBadRequest, errorCodeInvalidInput, "The state vector is invalid.")
		}
	}
	var document *crdt.Doc
	if err == nil {
		if document = openPageTextRoom(request.App, room); document == nil {
			document, _, err = loadYgoDoc(request.App, room)
		}
	}
	if err != nil {
		return apiErrorWithCause(
			request, http.StatusInternalServerError, errorCodeYgoPersistence,
			"Couldn't read the collaboration state.", "ygo.persistence.state_missing",
			"read", errorCodeYgoPersistence, err,
		)
	}
	update := crdt.EncodeStateAsUpdateV1(document, stateVector)
	if user, ok := authenticatedUser(request); ok {
		update = collaborationReaderUpdates(request.App, user.Id)(document, update)
	}
	return request.Blob(http.StatusOK, "application/octet-stream", update)
}

func readCollaborationStateVector(request *core.RequestEvent) error {
	room, written, err := collaborationStateRoom(request)
	if written {
		return err
	}
	stateVector := []byte{0}
	if err == nil {
		var stored storedYgoDocument
		var found bool
		stored, found, err = loadMaterializedYgoDocument(request.App, room)
		if err == nil && found && len(stored.State) > 0 {
			stateVector, err = crdt.EncodeStateVectorFromUpdate(stored.State)
		}
	}
	if err != nil {
		return apiErrorWithCause(
			request, http.StatusInternalServerError, errorCodeYgoPersistence,
			"Couldn't read the collaboration state.", "ygo.persistence.state_vector",
			"read", errorCodeYgoPersistence, err,
		)
	}
	return request.JSON(http.StatusOK, collaborationStateVectorResponse{
		StateVector: base64.StdEncoding.EncodeToString(stateVector),
	})
}

// readCollaborationStateLacks answers whether the stored document would
// change if the posted update were applied to it: new text, or deletions it
// does not have.
func readCollaborationStateLacks(request *core.RequestEvent) error {
	room, written, err := collaborationStateRoom(request)
	if written {
		return err
	}
	var update []byte
	if err == nil {
		request.Request.Body = http.MaxBytesReader(request.Response, request.Request.Body, collaborationLacksMaxBodyBytes)
		update, err = io.ReadAll(request.Request.Body)
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return apiError(request, http.StatusRequestEntityTooLarge, errorCodeInvalidInput, "The update is too large.")
		}
	}
	var document *crdt.Doc
	if err == nil {
		document, _, err = loadYgoDoc(request.App, room)
	}
	lacks := false
	if err == nil {
		stateBefore := crdt.EncodeStateVectorV1(document)
		deletedBefore := crdt.EncodeIDSet(crdt.DeleteSetFromDoc(document))
		if applyErr := crdt.ApplyUpdateV1(document, update, nil); applyErr != nil {
			return apiError(request, http.StatusBadRequest, errorCodeInvalidInput, "The update is invalid.")
		}
		lacks = !bytes.Equal(stateBefore, crdt.EncodeStateVectorV1(document)) ||
			!bytes.Equal(deletedBefore, crdt.EncodeIDSet(crdt.DeleteSetFromDoc(document)))
	}
	if err != nil {
		return apiErrorWithCause(
			request, http.StatusInternalServerError, errorCodeYgoPersistence,
			"Couldn't read the collaboration state.", "ygo.persistence.state_lacks",
			"read", errorCodeYgoPersistence, err,
		)
	}
	return request.JSON(http.StatusOK, collaborationStateLacksResponse{Lacks: lacks})
}
