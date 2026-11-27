package app

import (
	"database/sql"
	"errors"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A profile's cover, as Notion's profile page shows it (seen 2026-10-12):
// "변경하기", "위치 변경" and the download over it for the person and for the
// workspace's owner, who changes anyone's. Until one is chosen the person's
// gallery picture stands in. The same migration finds a person's comments
// for "최근 활동" ("편집 및 댓글 작성함").

type personProfileCover struct {
	Cover         string `json:"cover" db:"cover"`
	CoverPosition int    `json:"coverPosition" db:"cover_position"`
}

// personCoverEditable is whether the reader changes the person's cover: their own, or anyone's as the workspace's owner.
func personCoverEditable(authorization workspaceAuthorization, actorID, personID string) bool {
	return actorID == personID || authorization.Role == workspaceRoleOwner
}

// saveProfileCover sets a person's cover or its position, as the person or the workspace's owner.
func saveProfileCover(event *core.RequestEvent, authorization workspaceAuthorization, actorID, personID string, cover *string, position *int) error {
	if !personCoverEditable(authorization, actorID, personID) {
		return apiError(event, http.StatusForbidden, errorCodePersonProfileNotFound, "Only the person or the workspace's owner changes the cover.")
	}
	if cover != nil && utf8.RuneCountInString(*cover) > maxPageCoverRunes || position != nil && (*position < 0 || *position > 100) {
		return apiError(event, http.StatusBadRequest, errorCodePersonProfileInvalid, "Check the cover.")
	}
	if _, err := loadPersonCard(event.App, authorization, personID); err != nil {
		return personProfileError(event, err, "cover-person")
	}
	saved := personProfileCover{CoverPosition: 50}
	err := event.App.RunInTransaction(func(txApp core.App) error {
		if err := txApp.DB().NewQuery(`SELECT [[cover]], [[cover_position]] FROM {{leafen_workspace_person_profiles}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:personId}`).
			Bind(dbx.Params{"workspaceId": authorization.WorkspaceID, "personId": personID}).One(&saved); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// An uploaded cover is a picture (look_picture_files.go).
		if err := checkGivenLookFiles(txApp, authorization.WorkspaceID, givenCover("cover", cover, saved.Cover)); err != nil {
			return err
		}
		if cover != nil {
			saved.Cover = *cover
		}
		if position != nil {
			saved.CoverPosition = *position
		}
		_, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_workspace_person_profiles}} ([[workspace_id]], [[user_id]], [[cover]], [[cover_position]], [[updated_at]])
			VALUES ({:workspaceId}, {:personId}, {:cover}, {:position}, {:now})
			ON CONFLICT ([[workspace_id]], [[user_id]]) DO UPDATE SET [[cover]] = excluded.[[cover]],
				[[cover_position]] = excluded.[[cover_position]], [[updated_at]] = excluded.[[updated_at]]
		`).Bind(dbx.Params{"workspaceId": authorization.WorkspaceID, "personId": personID, "cover": saved.Cover,
			"position": saved.CoverPosition, "now": time.Now().UnixMilli()}).Execute()
		return err
	})
	if err != nil {
		return personProfileError(event, err, "cover-save")
	}
	return event.JSON(http.StatusOK, saved)
}
