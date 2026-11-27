package app

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Workspace analytics shows, as Notion's does, for the last 7, 30, 90 or
// 365 days: to the workspace owner how many members and guests were active
// against the window before, the members active each day, each
// member's page views, edits and last activity (marking members who keep
// their views out of analytics), the most viewed teamspaces and what
// members search for; and to everyone the pages they can read with their
// views, unique viewers and last edit, filtered by when and by whom they
// were created, teamspace and title.

const (
	workspaceAnalyticsDefaultDays   = 30
	workspaceAnalyticsMemberLimit   = 1000
	workspaceAnalyticsTopTeamspaces = 10

	errorCodeWorkspaceAnalyticsInvalid = "LEAFEN_WORKSPACE_ANALYTICS_INVALID"
	errorCodeWorkspaceAnalyticsFailed  = "LEAFEN_WORKSPACE_ANALYTICS_FAILED"
)

var workspaceAnalyticsRanges = []int{7, 30, 90, 365}

// workspaceAnalyticsRangeWords lists the ranges a request may ask for.
func workspaceAnalyticsRangeWords() string {
	words := make([]string, 0, len(workspaceAnalyticsRanges))
	for _, days := range workspaceAnalyticsRanges {
		words = append(words, strconv.Itoa(days))
	}
	return notionOrList(words)
}

type workspaceAnalyticsDayCount struct {
	Day   string `json:"day"`
	Count int    `json:"count"`
}

type workspaceAnalyticsMember struct {
	UserID            string `json:"userId"`
	Name              string `json:"name"`
	Email             string `json:"email"`
	Role              string `json:"role"`
	PageViews         int    `json:"pageViews"`
	PageEdits         int    `json:"pageEdits"`
	LastActiveAt      string `json:"lastActiveAt,omitempty"`
	ViewHistoryHidden bool   `json:"viewHistoryHidden"`
}

type workspaceAnalyticsTeamspace struct {
	TeamspaceID string `json:"teamspaceId"`
	Name        string `json:"name"`
	Views       int    `json:"views"`
}

type workspaceAnalyticsResponse struct {
	WorkspaceID   string `json:"workspaceId"`
	Days          int    `json:"days"`
	From          string `json:"from"`
	To            string `json:"to"`
	CanSeeMembers bool   `json:"canSeeMembers"`
	// RetentionDays is how long the organization keeps its analytics, 0
	// when it keeps them: the ranges offered go no further.
	RetentionDays int `json:"retentionDays"`
	// Engagement is the owner's: who was active in the window and before it.
	Engagement     *workspaceAnalyticsEngagement `json:"engagement,omitempty"`
	ActiveMembers  []workspaceAnalyticsDayCount  `json:"activeMembers"`
	Members        []workspaceAnalyticsMember    `json:"members"`
	MembersHasMore bool                          `json:"membersHasMore"`
	Teamspaces     []workspaceAnalyticsTeamspace `json:"teamspaces"`
	Content        []workspaceAnalyticsPage      `json:"content"`
	ContentHasMore bool                          `json:"contentHasMore"`
	// Searches are the owner's: queries members searched, never who did.
	Searches        []workspaceAnalyticsSearch `json:"searches"`
	SearchesHasMore bool                       `json:"searchesHasMore"`
}

// workspaceAnalyticsEngagement is Notion's user engagement: the members and
// guests who viewed or edited a page in the window, and in the window of the
// same length just before it.
type workspaceAnalyticsEngagement struct {
	Members         int `json:"members"`
	Guests          int `json:"guests"`
	PreviousMembers int `json:"previousMembers"`
	PreviousGuests  int `json:"previousGuests"`
}

type workspaceAnalyticsWindow struct {
	Days     int
	FromDay  string
	ToDay    string
	FromTime time.Time
	// The reader's offsets from UTC through the window, by which an edit
	// falls on a day (workspaceAnalyticsDaySQL).
	Offsets []workspaceAnalyticsOffset
}

// workspaceAnalyticsLocation is the reader's clock: the zone their screen
// names, else their own (personLocation), so an agent reading as them, which
// names none, reads the same days as their screen.
func workspaceAnalyticsLocation(app core.App, readerID, zone string) *time.Location {
	zone = strings.TrimSpace(zone)
	if zone != "" && zone != "Local" {
		if location, err := time.LoadLocation(zone); err == nil {
			return location
		}
	}
	return personLocation(app, readerID)
}

func registerWorkspaceAnalyticsRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/workspaces/{workspaceId}/analytics", readWorkspaceAnalytics)
	event.Router.POST(apiRoot+"/workspaces/{workspaceId}/search/events", recordWorkspaceSearchEvent)
}

func readWorkspaceAnalytics(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	query := event.Request.URL.Query()
	days := workspaceAnalyticsDefaultDays
	if raw := strings.TrimSpace(query.Get("days")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || !slices.Contains(workspaceAnalyticsRanges, parsed) {
			return apiError(event, http.StatusBadRequest, errorCodeWorkspaceAnalyticsInvalid,
				refusalMessage("Check the analytics range.", fmt.Sprintf("days is %s, not %q", workspaceAnalyticsRangeWords(), raw)))
		}
		days = parsed
	}
	filter, err := parseWorkspaceAnalyticsContentFilter(query)
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceAnalyticsInvalid, refusalMessage("Check the analytics filters.", refusalReason(err)))
	}
	authorization, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if err != nil {
		return workspaceAnalyticsFailure(event, err, "authorize")
	}
	// The window ends on the reader's today. Views are kept by the day of their
	// viewer's clock (page_analytics_clock.go) and fall on the reader's days as
	// they are; edits have their moment.
	location := workspaceAnalyticsLocation(event.App, user.Id, query.Get("timeZone"))
	now := time.Now().In(location)
	from := now.AddDate(0, 0, -(days - 1))
	fromTime := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, location)
	window := workspaceAnalyticsWindow{
		Days: days, FromDay: from.Format(time.DateOnly), ToDay: now.Format(time.DateOnly),
		FromTime: fromTime, Offsets: workspaceAnalyticsOffsets(fromTime, now),
	}
	response := workspaceAnalyticsResponse{
		WorkspaceID: workspaceID, Days: days, From: window.FromDay, To: window.ToDay,
		ActiveMembers: []workspaceAnalyticsDayCount{}, Members: []workspaceAnalyticsMember{},
		Teamspaces: []workspaceAnalyticsTeamspace{}, Searches: []workspaceAnalyticsSearch{},
	}
	retention, err := workspaceActivityRetention(event.App, workspaceID)
	if err != nil {
		return workspaceAnalyticsFailure(event, err, "retention")
	}
	response.RetentionDays = retention.AnalyticsDays
	// The workspace's owner sees every tab, and so does a member the
	// organization made an analytics admin, as Notion's Analytics viewer.
	if authorization.Role == workspaceRoleOwner || authorization.Organization.allows(permissionOrganizationAnalytics) {
		response.CanSeeMembers = true
		if response.Engagement, err = loadWorkspaceAnalyticsEngagement(event.App, workspaceID, window); err != nil {
			return workspaceAnalyticsFailure(event, err, "engagement")
		}
		if response.ActiveMembers, err = loadWorkspaceAnalyticsActiveMembers(event.App, workspaceID, window); err != nil {
			return workspaceAnalyticsFailure(event, err, "active-members")
		}
		if response.Members, response.MembersHasMore, err = loadWorkspaceAnalyticsMembers(event.App, workspaceID, window); err != nil {
			return workspaceAnalyticsFailure(event, err, "members")
		}
		if response.Teamspaces, err = loadWorkspaceAnalyticsTeamspaces(event.App, workspaceID, window); err != nil {
			return workspaceAnalyticsFailure(event, err, "teamspaces")
		}
		if response.Searches, response.SearchesHasMore, err = loadWorkspaceAnalyticsSearches(event.App, workspaceID, window); err != nil {
			return workspaceAnalyticsFailure(event, err, "searches")
		}
	}
	if response.Content, response.ContentHasMore, err = loadWorkspaceAnalyticsContent(
		event.App, authorization, user.Id, window, filter,
	); err != nil {
		return workspaceAnalyticsFailure(event, err, "content")
	}
	return event.JSON(http.StatusOK, response)
}

func workspaceAnalyticsFailure(event *core.RequestEvent, err error, stage string) error {
	return apiErrorWithCause(
		event, http.StatusInternalServerError, errorCodeWorkspaceAnalyticsFailed,
		"Couldn't load workspace analytics.", "workspace_analytics", stage,
		errorCodeWorkspaceAnalyticsFailed, err,
	)
}

// loadWorkspaceAnalyticsActiveMembers counts, for every day of the window,
// the workspace's members who viewed or edited a page that day; guests are
// apart, as on the cards.
func loadWorkspaceAnalyticsActiveMembers(
	app core.App, workspaceID string, window workspaceAnalyticsWindow,
) ([]workspaceAnalyticsDayCount, error) {
	rows := []workspaceAnalyticsDayCount{}
	editDay, params := workspaceAnalyticsDaySQL("[[created_at]]", window)
	params["workspaceId"], params["fromDay"], params["fromTime"] = workspaceID, window.FromDay, window.FromTime.UnixMilli()
	if err := app.DB().NewQuery(`
		SELECT [[day]], COUNT(DISTINCT [[user_id]]) AS [[count]] FROM (
			SELECT [[day]], [[user_id]] FROM {{leafen_workspace_analytics_member_daily}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[day]] >= {:fromDay}
			UNION
			SELECT ` + editDay + ` AS [[day]], [[actor_id]] AS [[user_id]]
			FROM {{leafen_page_history_versions}} WHERE ` + workspaceAnalyticsEditSQL("fromTime") + `
		) WHERE [[user_id]] IN (SELECT [[user_id]] FROM (` + workspaceAnalyticsPeopleSQL + `) WHERE [[guest]] = 0)
		GROUP BY [[day]]
	`).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		// A viewer ahead of the reader can be on a day after the reader's today.
		day := min(row.Day, window.ToDay)
		counts[day] = max(counts[day], row.Count)
	}
	result := make([]workspaceAnalyticsDayCount, 0, window.Days)
	for day := 0; day < window.Days; day++ {
		key := window.FromTime.AddDate(0, 0, day).Format(time.DateOnly)
		result = append(result, workspaceAnalyticsDayCount{Day: key, Count: counts[key]})
	}
	return result, nil
}

// loadWorkspaceAnalyticsEngagement counts the workspace's members and
// guests who viewed or edited a page in the window and in the one before
// it; people who left the workspace aren't counted.
func loadWorkspaceAnalyticsEngagement(
	app core.App, workspaceID string, window workspaceAnalyticsWindow,
) (*workspaceAnalyticsEngagement, error) {
	previous := window.FromTime.AddDate(0, 0, -window.Days)
	rows := []struct {
		Guest    int `db:"guest"`
		Current  int `db:"current"`
		Previous int `db:"previous"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT MAX([[member]].[[guest]]) AS [[guest]],
			MAX([[active]].[[current]]) AS [[current]], MAX([[active]].[[previous]]) AS [[previous]]
		FROM (
			SELECT [[user_id]], [[day]] >= {:fromDay} AS [[current]], [[day]] < {:fromDay} AS [[previous]]
			FROM {{leafen_workspace_analytics_member_daily}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[day]] >= {:previousDay}
			UNION ALL
			SELECT [[actor_id]] AS [[user_id]], [[created_at]] >= {:fromTime} AS [[current]],
				[[created_at]] < {:fromTime} AS [[previous]]
			FROM {{leafen_page_history_versions}} WHERE ` + workspaceAnalyticsEditSQL("previousTime") + `
		) [[active]]
		JOIN (` + workspaceAnalyticsPeopleSQL + `) [[member]] ON [[member]].[[user_id]] = [[active]].[[user_id]]
		GROUP BY [[active]].[[user_id]]
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "fromDay": window.FromDay, "fromTime": window.FromTime.UnixMilli(),
		"previousDay": previous.Format(time.DateOnly), "previousTime": previous.UnixMilli(),
	}).All(&rows); err != nil {
		return nil, err
	}
	engagement := &workspaceAnalyticsEngagement{}
	for _, row := range rows {
		guest := row.Guest > 0
		switch {
		case row.Current > 0 && guest:
			engagement.Guests++
		case row.Current > 0:
			engagement.Members++
		}
		switch {
		case row.Previous > 0 && guest:
			engagement.PreviousGuests++
		case row.Previous > 0:
			engagement.PreviousMembers++
		}
	}
	return engagement, nil
}

// loadWorkspaceAnalyticsMembers lists the workspace's members with their
// page views, edits and last activity in the window, most active first;
// guests are apart, as on the cards.
func loadWorkspaceAnalyticsMembers(
	app core.App, workspaceID string, window workspaceAnalyticsWindow,
) ([]workspaceAnalyticsMember, bool, error) {
	members := []struct {
		UserID string `db:"user_id"`
		Name   string `db:"name"`
		Email  string `db:"email"`
		Role   string `db:"role"`
		Hidden bool   `db:"hidden"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[member]].[[user_id]], COALESCE([[user]].[[name]], '') AS [[name]],
			COALESCE([[user]].[[email]], '') AS [[email]], [[member]].[[role]],
			EXISTS (
				SELECT 1 FROM {{leafen_page_analytics_member_preferences}} [[preference]]
				WHERE [[preference]].[[workspace_id]] = {:workspaceId}
					AND [[preference]].[[user_id]] = [[member]].[[user_id]]
					AND [[preference]].[[identified_history_enabled]] = 0
			) AS [[hidden]]
		FROM (` + workspaceAnalyticsPeopleSQL + `) [[member]]
		JOIN {{users}} [[user]] ON [[user]].[[id]] = [[member]].[[user_id]]
		LEFT JOIN (
			SELECT [[user_id]], SUM([[views]]) AS [[views]] FROM {{leafen_workspace_analytics_member_daily}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[day]] >= {:fromDay} GROUP BY [[user_id]]
		) [[viewed]] ON [[viewed]].[[user_id]] = [[member]].[[user_id]]
		LEFT JOIN (
			SELECT [[actor_id]], COUNT(*) AS [[edits]] FROM {{leafen_page_history_versions}}
			WHERE ` + workspaceAnalyticsEditSQL("fromTime") + ` GROUP BY [[actor_id]]
		) [[edited]] ON [[edited]].[[actor_id]] = [[member]].[[user_id]]
		WHERE [[member]].[[guest]] = 0
		GROUP BY [[member]].[[user_id]]
		-- The list is the most active first, so the page shown is the most
		-- active members, not whichever came first.
		ORDER BY COALESCE(MAX([[viewed]].[[views]]), 0) + COALESCE(MAX([[edited]].[[edits]]), 0) DESC,
			LOWER(COALESCE([[user]].[[name]], '') || COALESCE([[user]].[[email]], '')), [[member]].[[user_id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "fromDay": window.FromDay, "fromTime": window.FromTime.UnixMilli(),
		"limit": workspaceAnalyticsMemberLimit + 1,
	}).All(&members); err != nil {
		return nil, false, err
	}
	hasMore := len(members) > workspaceAnalyticsMemberLimit
	if hasMore {
		members = members[:workspaceAnalyticsMemberLimit]
	}
	views := []struct {
		UserID   string `db:"user_id"`
		Views    int    `db:"views"`
		LastSeen string `db:"last_seen"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[user_id]], SUM([[views]]) AS [[views]], MAX([[last_viewed_at]]) AS [[last_seen]]
		FROM {{leafen_workspace_analytics_member_daily}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[day]] >= {:fromDay}
		GROUP BY [[user_id]]
	`).Bind(dbx.Params{"workspaceId": workspaceID, "fromDay": window.FromDay}).All(&views); err != nil {
		return nil, false, err
	}
	edits := []struct {
		UserID   string `db:"actor_id"`
		Edits    int    `db:"edits"`
		LastEdit int64  `db:"last_edit"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[actor_id]], COUNT(*) AS [[edits]], MAX([[created_at]]) AS [[last_edit]]
		FROM {{leafen_page_history_versions}} WHERE ` + workspaceAnalyticsEditSQL("fromTime") + `
		GROUP BY [[actor_id]]
	`).Bind(dbx.Params{"workspaceId": workspaceID, "fromTime": window.FromTime.UnixMilli()}).All(&edits); err != nil {
		return nil, false, err
	}
	activity := map[string]*workspaceAnalyticsMember{}
	result := make([]workspaceAnalyticsMember, 0, len(members))
	for _, member := range members {
		result = append(result, workspaceAnalyticsMember{
			UserID: member.UserID, Name: member.Name, Email: member.Email, Role: member.Role,
			ViewHistoryHidden: member.Hidden,
		})
	}
	for index := range result {
		activity[result[index].UserID] = &result[index]
	}
	for _, row := range views {
		if member, found := activity[row.UserID]; found {
			member.PageViews = row.Views
			member.LastActiveAt = workspaceAnalyticsLater(member.LastActiveAt, workspaceAnalyticsInstant(row.LastSeen))
		}
	}
	for _, row := range edits {
		if member, found := activity[row.UserID]; found {
			member.PageEdits = row.Edits
			member.LastActiveAt = workspaceAnalyticsLater(
				member.LastActiveAt, time.UnixMilli(row.LastEdit).UTC().Format(time.RFC3339Nano),
			)
		}
	}
	slices.SortStableFunc(result, func(left, right workspaceAnalyticsMember) int {
		if left.PageViews+left.PageEdits != right.PageViews+right.PageEdits {
			return (right.PageViews + right.PageEdits) - (left.PageViews + left.PageEdits)
		}
		return strings.Compare(strings.ToLower(left.Name+left.Email), strings.ToLower(right.Name+right.Email))
	})
	return result, hasMore, nil
}

// loadWorkspaceAnalyticsTeamspaces ranks the live teamspaces by the views
// of the pages and database pages inside them.
func loadWorkspaceAnalyticsTeamspaces(
	app core.App, workspaceID string, window workspaceAnalyticsWindow,
) ([]workspaceAnalyticsTeamspace, error) {
	views := []struct {
		PageID string `db:"page_id"`
		Views  int    `db:"views"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[page_id]], SUM([[views]]) AS [[views]] FROM (
			SELECT [[page_id]], [[total_views]] AS [[views]] FROM {{leafen_page_analytics_daily}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[day]] >= {:fromDay}
			UNION ALL
			SELECT [[row]].[[page_id]], [[daily]].[[total_views]] AS [[views]]
			FROM {{leafen_row_page_analytics_daily}} [[daily]]
			JOIN {{leafen_database_rows}} [[row]] ON [[row]].[[id]] = [[daily]].[[row_id]]
			WHERE [[daily]].[[workspace_id]] = {:workspaceId} AND [[daily]].[[day]] >= {:fromDay}
		) GROUP BY [[page_id]]
	`).Bind(dbx.Params{"workspaceId": workspaceID, "fromDay": window.FromDay}).All(&views); err != nil {
		return nil, err
	}
	pageIDs := make([]string, 0, len(views))
	for _, row := range views {
		pageIDs = append(pageIDs, row.PageID)
	}
	places, err := loadWorkspaceSearchPlaces(app, pageIDs)
	if err != nil {
		return nil, err
	}
	totals := map[string]int{}
	for _, row := range views {
		for _, teamspaceID := range places[row.PageID].Teamspaces {
			totals[teamspaceID] += row.Views
		}
	}
	teamspaces := []workspaceAnalyticsTeamspace{}
	if err := app.DB().NewQuery(`
		SELECT [[id]] AS [[teamspace_id]], [[name]], 0 AS [[views]] FROM {{leafen_teamspaces}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[deleted_at]] = 0 AND [[archived_at]] = 0
	`).Bind(dbx.Params{"workspaceId": workspaceID}).All(&teamspaces); err != nil {
		return nil, err
	}
	for index := range teamspaces {
		teamspaces[index].Views = totals[teamspaces[index].TeamspaceID]
	}
	teamspaces = slices.DeleteFunc(teamspaces, func(teamspace workspaceAnalyticsTeamspace) bool {
		return teamspace.Views == 0
	})
	slices.SortStableFunc(teamspaces, func(left, right workspaceAnalyticsTeamspace) int {
		if left.Views != right.Views {
			return right.Views - left.Views
		}
		return strings.Compare(left.Name, right.Name)
	})
	if len(teamspaces) > workspaceAnalyticsTopTeamspaces {
		teamspaces = teamspaces[:workspaceAnalyticsTopTeamspaces]
	}
	return teamspaces, nil
}

func workspaceAnalyticsInstant(value string) string {
	parsed, err := time.Parse(pageAnalyticsTimeLayout, value)
	if err != nil {
		return ""
	}
	return parsed.UTC().Format(time.RFC3339Nano)
}

func workspaceAnalyticsLater(current, candidate string) string {
	if candidate == "" {
		return current
	}
	if current == "" {
		return candidate
	}
	left, leftErr := time.Parse(time.RFC3339Nano, current)
	right, rightErr := time.Parse(time.RFC3339Nano, candidate)
	if leftErr != nil || (rightErr == nil && right.After(left)) {
		return candidate
	}
	return current
}
