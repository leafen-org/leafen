package app

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The packaged desktop app's notifications. Its window has no push service,
// so while it is open it asks for the person's personal notifications that
// came since it last asked and are still unread ten seconds later, as a
// browser's push would bring them (Notion's desktop app shows comments and
// mentions while it is open), worded as those pushes are, with the unread
// count its icon shows.

const (
	desktopNoticeWorkspaces = 64
	desktopNoticeLimit      = 20
)

type desktopNotice struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	URL        string `json:"url"`
	OccurredAt int64  `json:"occurredAt"`
}

type desktopNoticesResponse struct {
	Notices     []desktopNotice `json:"notices"`
	UnreadCount int             `json:"unreadCount"`
	// What came up to here is answered; the app asks after it next.
	Through int64 `json:"through"`
	// What the two say, for whoever reads the answer without the app
	// (2026-11-16: an agent read unreadCount 1 beside no notices as a fault).
	About string `json:"about"`
}

const desktopNoticesAbout = "notices are the unread personal notifications that came after the time asked after (after) and up to through, " +
	"each given once — none when after is left out, as the app starts; unreadCount counts every unread one, as the inbox does."

func registerDesktopNoticeRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/account/desktop-notices", listDesktopNoticesRoute)
}

func listDesktopNoticesRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	query := event.Request.URL.Query()
	workspaceIDs := query["workspaceId"]
	// Without a time to ask after, the app is starting: it hears where to
	// begin and the unread count, and nothing that came before.
	after := int64(-1)
	if raw := query.Get("after"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "The time to ask after is not a time.")
		}
		after = parsed
	}
	if len(workspaceIDs) == 0 || len(workspaceIDs) > desktopNoticeWorkspaces {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Name up to 64 workspaces.")
	}
	language, err := serverTextLanguage(event.App, user.Id, event.Request)
	if err != nil {
		return err
	}
	response, err := readDesktopNotices(event.App, user.Id, language, workspaceIDs, after, time.Now())
	if err != nil {
		return notificationInboxAPIError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func readDesktopNotices(
	app core.App,
	userID string,
	language string,
	workspaceIDs []string,
	after int64,
	now time.Time,
) (desktopNoticesResponse, error) {
	due := now.Add(-webPushDelay).UnixMilli()
	response := desktopNoticesResponse{Notices: []desktopNotice{}, Through: due, About: desktopNoticesAbout}
	shown := []storedNotificationEmailItem{}
	seen := map[string]bool{}
	for _, raw := range workspaceIDs {
		workspaceID := strings.TrimSpace(raw)
		if workspaceID == "" || seen[workspaceID] {
			continue
		}
		seen[workspaceID] = true
		// The unread count the inbox shows, personal notifications only.
		unread, err := notificationUnreadCount(app, userID, workspaceID, true)
		if isPermissionDenied(err) {
			// A workspace the person can no longer read tells them nothing,
			// and keeps the others' notices coming.
			continue
		}
		if err != nil {
			return response, err
		}
		response.UnreadCount += unread
		if after < 0 {
			continue
		}
		rows := []storedNotificationEmailItem{}
		if err := app.DB().NewQuery(`
			SELECT [[id]], [[kind]], [[activity_key]], [[page_id]], [[block_id]], [[comment_id]], [[actor_id]], [[title]], [[preview]],
				[[preview_spans]], [[target]], [[occurred_at]], [[read_at]], [[archived_at]], [[actor_connection]]
			FROM {{leafen_notifications}}
			INDEXED BY [[idx_leafen_notifications_user_activity]]
			WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:userId}
				AND [[occurred_at]] > {:after} AND [[occurred_at]] <= {:due}
				AND [[personal]] = 1 AND [[read_at]] = 0 AND [[archived_at]] = 0 AND ` + notificationShownSQL("") + `
			ORDER BY [[occurred_at]] DESC, [[id]] DESC
			LIMIT {:limit}
		`).Bind(dbx.Params{
			"workspaceId": workspaceID, "userId": userID, "after": after, "due": due, "limit": desktopNoticeLimit,
		}).All(&rows); err != nil {
			return response, err
		}
		if len(rows) == 0 {
			continue
		}
		notices := make([]notificationSubjectRef, 0, len(rows))
		for _, row := range rows {
			notices = append(notices, notificationSubjectRef{PageID: row.PageID, Target: row.Target})
		}
		open, err := shownNotificationSubjects(app, userID, workspaceID, notices)
		if err != nil {
			return response, err
		}
		for index, row := range rows {
			if open[index] {
				shown = append(shown, row)
			}
		}
	}
	if len(shown) == 0 {
		return response, nil
	}
	notificationEmailItemsAsRead(app, userID, shown)
	actors, err := loadNotificationEmailActors(app, userID, language, shown)
	if err != nil {
		return response, err
	}
	location := accountLocation(app, userID)
	for _, item := range shown {
		response.Notices = append(response.Notices, desktopNotice{
			ID:         item.ID,
			Title:      notificationEmailHeadline(language, item, notificationEmailItemActor(language, actors, item)),
			Body:       inviteMessageQuoted(item.Kind, notificationEmailLine(notificationLineAsItStands(item.Preview, item.PreviewSpans, language, location, now), notificationEmailPreviewRunes)),
			URL:        item.Target,
			OccurredAt: item.OccurredAt,
		})
	}
	return response, nil
}
