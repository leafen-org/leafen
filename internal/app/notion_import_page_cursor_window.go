package app

import (
	"errors"
	"math"

	"github.com/pocketbase/pocketbase/core"
)

type pageCursorReservation struct {
	next      int
	remaining int
}

type notionApplyPageCursors struct {
	reservation *pageCursorReservation
}

var errPageCursorReservation = errors.New("page cursor reservation failed")

func reserveNotionApplyPageCursors(
	app core.App,
	ownerID string,
	sources []storedNotionSource,
	mappings *notionApplyMappings,
) (*notionApplyPageCursors, error) {
	required := 0
	for _, source := range sources {
		if source.Kind != "page" {
			continue
		}
		if _, found := mappings.nativeID(source.SourceID, "page"); !found {
			required++
		}
	}
	reservation, err := reservePageCursors(app, ownerID, required)
	if err != nil {
		return nil, errors.New(errorCodeNotionImportCreateFailed)
	}
	return &notionApplyPageCursors{reservation: reservation}, nil
}

func reservePageCursors(
	app core.App,
	ownerID string,
	required int,
) (*pageCursorReservation, error) {
	if required < 0 {
		return nil, errPageCursorReservation
	}
	cursors := &pageCursorReservation{remaining: required}
	if required == 0 {
		return cursors, nil
	}
	state, err := workspaceUserState(app, ownerID)
	if err != nil {
		return nil, err
	}
	current := state.GetInt("cursor")
	if current < 0 || current > math.MaxInt-required {
		return nil, errPageCursorReservation
	}
	state.Set("cursor", current+required)
	if err := app.Save(state); err != nil {
		return nil, err
	}
	cursors.next = current
	return cursors, nil
}

func (cursors *pageCursorReservation) take() (int, error) {
	if cursors == nil || cursors.remaining <= 0 {
		return 0, errPageCursorReservation
	}
	cursors.next++
	cursors.remaining--
	return cursors.next, nil
}

func (cursors *notionApplyPageCursors) take() (int, error) {
	if cursors == nil {
		return 0, errors.New(errorCodeNotionImportCreateFailed)
	}
	value, err := cursors.reservation.take()
	if err != nil {
		return 0, errors.New(errorCodeNotionImportCreateFailed)
	}
	return value, nil
}
