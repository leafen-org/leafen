package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page kept for offline use on a device is brought up to date while the
// device is connected, as Notion keeps its offline pages in the background.
// The device asks now and then, for all its kept pages in one request, where
// each page's changes stand: its blocks and its databases by the page's last
// change, its text by its text's. It reads again only the pages that moved,
// their text as what its copy lacks (collaboration_state_vector.go). A page
// no longer open to the person has no mark, and its copy stays as it was.

const (
	offlinePageMarksPath = apiRoot + "/offline-page-marks"
	// One request asks for at most this many pages.
	maxOfflinePageMarks          = 100
	offlinePageMarksMaxBodyBytes = 64 << 10
)

type offlinePageMarksInput struct {
	PageIDs []string `json:"pageIds"`
}

// offlinePageMark is where a page's changes stand: Graph is the revision its
// content graph reads at, Text how many text updates its document took.
type offlinePageMark struct {
	Graph int `json:"graph" db:"graph"`
	Text  int `json:"text" db:"text"`
}

type offlinePageMarksResponse struct {
	Marks map[string]offlinePageMark `json:"marks"`
}

func readOfflinePageMarks(request *core.RequestEvent) error {
	user, ok := authenticatedUser(request)
	if !ok {
		return apiError(request, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := offlinePageMarksInput{}
	request.Request.Body = http.MaxBytesReader(request.Response, request.Request.Body, offlinePageMarksMaxBodyBytes)
	decoder := json.NewDecoder(request.Request.Body)
	decoder.DisallowUnknownFields()
	var trailing any
	if decoder.Decode(&input) != nil || !errors.Is(decoder.Decode(&trailing), io.EOF) ||
		len(input.PageIDs) > maxOfflinePageMarks {
		return apiError(request, http.StatusBadRequest, errorCodeInvalidInput, "The pages are invalid.")
	}
	marks := map[string]offlinePageMark{}
	for _, raw := range input.PageIDs {
		pageID := strings.TrimSpace(raw)
		if pageID == "" || len(pageID) > maxWorkspacePermissionIdentifierLength {
			return apiError(request, http.StatusBadRequest, errorCodeInvalidInput, "The pages are invalid.")
		}
		if _, seen := marks[pageID]; seen {
			continue
		}
		owner, err := authorizeCollaborationStateRoom(request.App, user.Id, pageID)
		if isPermissionDenied(err) {
			continue
		}
		mark := offlinePageMark{}
		if err == nil {
			err = request.App.DB().NewQuery(`
				SELECT COALESCE((SELECT MAX([[cursor]]) FROM {{leafen_graph_mutations}}
						WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}), 0) AS [[graph]],
					COALESCE((SELECT [[next_sequence]] FROM {{leafen_ygo_documents}}
						WHERE [[room]] = {:pageId}), 0) AS [[text]]
			`).Bind(dbx.Params{"owner": owner, "pageId": pageID}).One(&mark)
		}
		if err != nil {
			return apiErrorWithCause(
				request, http.StatusInternalServerError, errorCodeYgoPersistence,
				"Couldn't read the pages' changes.", "offline.page_marks",
				"read", errorCodeYgoPersistence, err,
			)
		}
		marks[pageID] = mark
	}
	return request.JSON(http.StatusOK, offlinePageMarksResponse{Marks: marks})
}
