package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Notion's API takes a data source's trash change beside the rest of its
// update (seen 2026-11-23, Notion-Version 2026-03-11, synthetic sources
// under the test page, two runs): out of the trash first and into it last,
// as a page's (notion_database_row_page_mutation.go). A source in the trash,
// or under a page in it, takes only being taken out: anything else — the
// same trash again, an empty change, a title, either beside the trash again —
// isn't found, as a change to it isn't (serveNotionDataSourceChange). A
// value of the wrong form is refused by the request check before either, and
// a rest refused once the source is out puts it back. A move beside a change
// of the trash stays its own request (not seen in Notion), and a rest beside
// taking out one that is in the trash with its database or its page isn't
// found, as that rest alone isn't.

// changes is whether an update asks for anything beside the trash.
func (input notionDataSourceUpdateRequest) changes() bool {
	return input.Title != nil || input.Icon != nil || input.Properties != nil || input.Parent != nil
}

// notionDataSourceChangePlace is where a data source stands for a change:
// trashed is in the trash or under a page in it, and itself is in the trash
// on its own, its database and page out of it, so a rest can follow taking
// it out.
type notionDataSourceChangePlace struct {
	trashed, itself bool
}

// loadNotionDataSourceChangePlace finds where a data source stands; one not
// found is out of the trash here, and its change answers that it isn't.
func loadNotionDataSourceChangePlace(app core.App, workspaceID, dataSourceID string) (notionDataSourceChangePlace, error) {
	state, err := loadNotionDataSourceTrashState(app, workspaceID, dataSourceID)
	if errors.Is(err, errDatabaseRowPageNotFound) {
		return notionDataSourceChangePlace{}, nil
	}
	if err != nil {
		return notionDataSourceChangePlace{}, err
	}
	_, pageTrashed, err := loadPageTrashEffect(app, state.PageID)
	if err != nil || !state.trashed() {
		return notionDataSourceChangePlace{trashed: pageTrashed}, err
	}
	origin, err := notionDataSourceTrashOrigin(app, state)
	if err != nil {
		return notionDataSourceChangePlace{}, err
	}
	databaseTrashed := state.Deleted || state.RootBlockID.Valid && origin == ""
	return notionDataSourceChangePlace{trashed: true, itself: !databaseTrashed && !pageTrashed}, nil
}

// serveNotionDataSourceTrashTurn answers an update that names the trash, or
// changes nothing.
func serveNotionDataSourceTrashTurn(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, bearer notionOAuthBearerIdentity,
	dataSourceID, publicID string, input notionDataSourceUpdateRequest,
) error {
	place, err := loadNotionDataSourceChangePlace(event.App, bearer.WorkspaceID, dataSourceID)
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	restoring := input.InTrash != nil && !*input.InTrash
	notFound := func() error {
		return notionNamedObjectNotFound(event, "data_source", notionDashedUUID(strings.TrimSpace(publicID)))
	}
	if place.trashed && !restoring && input.InTrash != nil && !input.changes() && !place.itself {
		// One in the trash with its database or its page is answered there.
		return serveNotionDataSourceTrashChange(event, ygo, bearer, dataSourceID, true)
	}
	if place.trashed && (!restoring || input.changes() && !place.itself) {
		return notFound()
	}
	// A body that changes nothing answers with the source as it is, as
	// Notion's does (seen 2026-11-10: {} and unknown keys alone are 200).
	if input.InTrash == nil {
		return serveNotionDataSourceRetrieve(event, publicID)
	}
	rest := input
	rest.InTrash = nil
	if !rest.changes() {
		return serveNotionDataSourceTrashChange(event, ygo, bearer, dataSourceID, *input.InTrash)
	}
	change := func() error { return serveNotionDataSourceChange(event, ygo, bearer, dataSourceID, publicID, rest) }
	trash := func(inTrash bool) func() error {
		return func() error { return serveNotionDataSourceTrashChange(event, ygo, bearer, dataSourceID, inTrash) }
	}
	// Out of the trash already: the rest alone.
	if !place.trashed && restoring {
		return change()
	}
	if rest.Parent != nil {
		return notionPageValidationError(event, "Change the data source trash status in its own request.")
	}
	if !restoring {
		changed := recordNotionAnswer(event, change)
		if !changed.ok() {
			return changed.answer(event)
		}
		return trash(true)()
	}
	restored := recordNotionAnswer(event, trash(false))
	if !restored.ok() {
		return restored.answer(event)
	}
	changed := recordNotionAnswer(event, change)
	if !changed.ok() {
		recordNotionAnswer(event, trash(true))
	}
	return changed.answer(event)
}

// notionRecordedAnswer is what one step of a request answered, kept until
// the steps after it decide whether it is the answer.
type notionRecordedAnswer struct {
	recorder notionVersionRecorder
	err      error
}

// recordNotionAnswer runs a step that answers the request, keeping its
// answer instead of sending it.
func recordNotionAnswer(event *core.RequestEvent, step func() error) *notionRecordedAnswer {
	recorded := &notionRecordedAnswer{recorder: notionVersionRecorder{header: http.Header{}, status: http.StatusOK}}
	original := event.Response
	event.Response = &recorded.recorder
	recorded.err = step()
	event.Response = original
	return recorded
}

// ok is a step that answered 200.
func (recorded *notionRecordedAnswer) ok() bool {
	return recorded.err == nil && recorded.recorder.wrote && recorded.recorder.status == http.StatusOK
}

// answer sends what the step answered.
func (recorded *notionRecordedAnswer) answer(event *core.RequestEvent) error {
	if !recorded.recorder.wrote {
		return recorded.err
	}
	for key, values := range recorded.recorder.header {
		event.Response.Header()[key] = values
	}
	event.Response.WriteHeader(recorded.recorder.status)
	_, _ = event.Response.Write(recorded.recorder.body.Bytes())
	return recorded.err
}

func serveNotionDataSourceUpdated(event *core.RequestEvent, authorization notionDatabaseRowPageAuthorization) error {
	response, err := loadNotionDataSourceResponse(
		event.App, event.Request, authorization.Identity, authorization.Container.DataSource,
		permissionWorkspaceRead,
	)
	if err != nil {
		return notionDataSourceReadError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}
