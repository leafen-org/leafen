package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// A deadline's notices as its item shows them, with who acknowledged each
// and when, and a person's acknowledgement of one sent to them
// (deadline_notices.go).

var (
	errDeadlineNotYours = errors.New("deadline notice not for this person")
	errDeadlineNotSent  = errors.New("deadline notice not sent yet")
)

const deadlineStatesLimit = 200

func registerDeadlineRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+deadlineNoticeListPath, readDeadlineNotices)
	event.Router.POST(apiRoot+deadlineNoticeAcknowledgePath, acknowledgeDeadlineNotice)
	event.Router.POST(apiRoot+deadlineNoticeStatesPath, readDeadlineNoticeStates).Bind(apis.BodyLimit(32 << 10))
}

// deadlineNoticeView is a notice as someone who can open its item sees it.
func deadlineNoticeView(app core.App, principalID string, notice storedDeadlineNotice) (deadlineNoticeWire, error) {
	view := deadlineNoticeWire{
		ID: notice.ID, PropertyID: notice.PropertyID, Days: notice.Days, DueAt: notice.DueAt, DeadlineAt: notice.DeadlineAt,
		Status: notice.Status, Rounds: notice.Rounds, Acknowledgements: []deadlineAcknowledgementWire{},
	}
	recipients, err := deadlineRecipients(app, notice)
	if err != nil {
		return view, err
	}
	view.Recipient = sortedContains(recipients, principalID)
	acknowledgements := []struct {
		UserID string `db:"user_id"`
		Name   string `db:"name"`
		At     int64  `db:"acknowledged_at"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[ack]].[[user_id]], COALESCE(NULLIF(trim([[user]].[[name]]), ''), [[user]].[[email]], '') AS [[name]], [[ack]].[[acknowledged_at]]
		FROM {{leafen_deadline_acknowledgements}} [[ack]]
		LEFT JOIN {{users}} [[user]] ON [[user]].[[id]] = [[ack]].[[user_id]]
		WHERE [[ack]].[[notice_id]] = {:id} ORDER BY [[ack]].[[acknowledged_at]], [[ack]].[[user_id]]
	`).Bind(dbx.Params{"id": notice.ID}).All(&acknowledgements); err != nil {
		return view, err
	}
	for _, acknowledgement := range acknowledgements {
		view.Acknowledgements = append(view.Acknowledgements, deadlineAcknowledgementWire{
			UserID: acknowledgement.UserID, Name: acknowledgement.Name, At: acknowledgement.At,
		})
		if acknowledgement.UserID == principalID {
			view.AcknowledgedAt = acknowledgement.At
		}
	}
	return view, nil
}

func readDeadlineNotices(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	rowID := strings.TrimSpace(event.Request.PathValue("rowId"))
	if _, err := authorizeDatabaseRowPage(event.App, user.Id, rowID, permissionWorkspaceRead); err != nil {
		return deadlineRouteError(event, err, "authorize")
	}
	propertyID := strings.TrimSpace(event.Request.URL.Query().Get("propertyId"))
	notices := []storedDeadlineNotice{}
	if err := event.App.DB().NewQuery(`
		SELECT ` + storedDeadlineNoticeColumns + ` FROM {{leafen_deadline_notices}}
		WHERE [[row_id]] = {:rowId} AND ({:propertyId} = '' OR [[property_id]] = {:propertyId})
		ORDER BY [[property_id]], [[days]] DESC
	`).Bind(dbx.Params{"rowId": rowID, "propertyId": propertyID}).All(&notices); err != nil {
		return deadlineRouteError(event, err, "list")
	}
	response := deadlineNoticeListResponse{Notices: []deadlineNoticeWire{}}
	for _, notice := range notices {
		view, err := deadlineNoticeView(event.App, user.Id, notice)
		if err != nil {
			return deadlineRouteError(event, err, "view")
		}
		response.Notices = append(response.Notices, view)
	}
	return event.JSON(http.StatusOK, response)
}

// acknowledgeDeadline records that someone a notice went to has seen it:
// it comes to them no more, and their notifications of it are read.
func acknowledgeDeadline(app core.App, principalID, noticeID string, now time.Time) (storedDeadlineNotice, error) {
	notice := storedDeadlineNotice{}
	err := app.RunInTransaction(func(txApp core.App) error {
		if err := txApp.DB().NewQuery(`SELECT ` + storedDeadlineNoticeColumns + ` FROM {{leafen_deadline_notices}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": noticeID}).One(&notice); err != nil {
			return err
		}
		visible, err := deadlineRecipientCanSee(txApp, principalID, notice)
		if err != nil {
			return err
		}
		if !visible {
			return permissionDenied()
		}
		recipients, err := deadlineRecipients(txApp, notice)
		if err != nil {
			return err
		}
		if !sortedContains(recipients, principalID) {
			return errDeadlineNotYours
		}
		if notice.Rounds == 0 {
			return errDeadlineNotSent
		}
		if _, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_deadline_acknowledgements}} ([[notice_id]], [[user_id]], [[workspace_id]], [[page_id]], [[acknowledged_at]])
			VALUES ({:id}, {:userId}, {:workspaceId}, {:pageId}, {:now})
			ON CONFLICT ([[notice_id]], [[user_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"id": notice.ID, "userId": principalID, "workspaceId": notice.WorkspaceID, "pageId": notice.PageID, "now": now.UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_notifications}} SET [[read_at]] = {:now}, [[updated_at]] = {:now}
			WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:userId} AND [[read_at]] = 0
				AND substr([[activity_key]], 1, length({:prefix})) = {:prefix}
		`).Bind(dbx.Params{
			"now": now.UnixMilli(), "workspaceId": notice.WorkspaceID, "userId": principalID,
			"prefix": deadlineActivityPrefix + notice.ID + ":",
		}).Execute()
		return err
	})
	return notice, err
}

func acknowledgeDeadlineNotice(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	noticeID := strings.TrimSpace(event.Request.PathValue("noticeId"))
	notice, err := acknowledgeDeadline(event.App, user.Id, noticeID, time.Now().UTC())
	if err != nil {
		return deadlineRouteError(event, err, "acknowledge")
	}
	view, err := deadlineNoticeView(event.App, user.Id, notice)
	if err != nil {
		return deadlineRouteError(event, err, "view")
	}
	return event.JSON(http.StatusOK, deadlineNoticeAcknowledgeResponse{Notice: view})
}

// readDeadlineNoticeStates says which of the given notices the person has
// acknowledged, and which are gone, for the inbox's rows.
func readDeadlineNoticeStates(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := deadlineNoticeStatesRequest{}
	if err := event.BindBody(&input); err != nil || len(input.IDs) > deadlineStatesLimit {
		return apiError(event, http.StatusBadRequest, errorCodeDeadlineInvalid, "Name at most 200 notices.")
	}
	response := deadlineNoticeStatesResponse{Acknowledged: []string{}, Gone: []string{}}
	if len(input.IDs) == 0 {
		return event.JSON(http.StatusOK, response)
	}
	ids, _ := json.Marshal(input.IDs)
	if err := event.App.DB().NewQuery(`
		SELECT [[notice_id]] FROM {{leafen_deadline_acknowledgements}}
		WHERE [[user_id]] = {:userId} AND [[notice_id]] IN (SELECT [[value]] FROM json_each({:ids}))
		ORDER BY [[notice_id]]
	`).Bind(dbx.Params{"userId": user.Id, "ids": string(ids)}).Column(&response.Acknowledged); err != nil {
		return deadlineRouteError(event, err, "states")
	}
	// A notice its deadline's change or removal took away.
	if err := event.App.DB().NewQuery(`
		SELECT [[value]] FROM json_each({:ids})
		WHERE NOT EXISTS (SELECT 1 FROM {{leafen_deadline_notices}} WHERE [[id]] = [[value]]) ORDER BY [[value]]
	`).Bind(dbx.Params{"ids": string(ids)}).Column(&response.Gone); err != nil {
		return deadlineRouteError(event, err, "states")
	}
	return event.JSON(http.StatusOK, response)
}

func deadlineRouteError(event *core.RequestEvent, err error, step string) error {
	switch {
	case isPermissionDenied(err), errors.Is(err, errDatabaseRowPageNotFound), errors.Is(err, sql.ErrNoRows):
		return apiError(event, http.StatusNotFound, errorCodeDeadlineNotFound, "Deadline not found.")
	case errors.Is(err, errDeadlineNotYours):
		return apiError(event, http.StatusBadRequest, errorCodeDeadlineInvalid, "This deadline's notices don't go to you.")
	case errors.Is(err, errDeadlineNotSent):
		return apiError(event, http.StatusBadRequest, errorCodeDeadlineInvalid, "This notice hasn't been sent yet.")
	}
	return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeDeadlineFailed, "Couldn't read the deadline.", "deadlines", step, errorCodeDeadlineFailed, err)
}
