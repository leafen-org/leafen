package app

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An account's history (the audit decision of 2026-10-21 20:05, 1): a
// person's sign-ins, reads, edits, permission changes, tokens and
// connections and agent work, newest first in one list, from the sign-in
// records and the organization's audit events, which journal the workspaces'
// activity too (workspace.activity.*).
// Who sees what is the server's to say:
//   - the account itself: all of it, and what others did to it;
//   - those who keep the organization's audit log: the same;
//   - anyone else of the organization, unless it turned this off
//     (account_history_policy.go): of the account's work on pages, that on
//     pages they have full access to; sign-ins and the organization's own
//     matters they don't see.
// Opening another's history goes in the audit log; they aren't told, so it
// isn't in their own.

const (
	// accountHistoryFetch is how much of each source a pass reads, and
	// accountHistoryPasses how many passes a page of history takes at most.
	accountHistoryFetch  = 200
	accountHistoryPasses = 5
)

type accountHistoryCursor struct {
	At   int64
	Kind string
	ID   string
}

func (cursor accountHistoryCursor) String() string {
	if cursor.Kind == "" {
		return ""
	}
	return strconv.FormatInt(cursor.At, 10) + ":" + cursor.Kind + ":" + cursor.ID
}

func parseAccountHistoryCursor(raw string) (accountHistoryCursor, bool) {
	if raw == "" {
		return accountHistoryCursor{}, true
	}
	at, rest, ok := strings.Cut(raw, ":")
	kind, id, ok2 := strings.Cut(rest, ":")
	value, err := strconv.ParseInt(at, 10, 64)
	if !ok || !ok2 || err != nil || value < 0 || kind == "" || id == "" || len(raw) > organizationAuditMaxCursorBytes {
		return accountHistoryCursor{}, false
	}
	return accountHistoryCursor{At: value, Kind: kind, ID: id}, true
}

// older says an item comes after the cursor, newest first.
func (cursor accountHistoryCursor) older(item accountHistoryItem) bool {
	if cursor.Kind == "" {
		return true
	}
	if item.At != cursor.At {
		return item.At < cursor.At
	}
	if item.Kind != cursor.Kind {
		return item.Kind < cursor.Kind
	}
	return item.ID < cursor.ID
}

func readAccountHistoryRoute(event *core.RequestEvent) error {
	viewer, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	organizationID := strings.TrimSpace(event.Request.PathValue("organizationId"))
	userID := strings.TrimSpace(event.Request.PathValue("userId"))
	notFound := func() error {
		return apiError(event, http.StatusNotFound, errorCodeOrganizationAuditNotFound, "No history to show.")
	}
	if _, err := authorizeOrganization(event.App, viewer.Id, organizationID, permissionOrganizationActive); err != nil || !accountOfOrganization(event.App, organizationID, userID) {
		return notFound()
	}
	scope := ""
	switch {
	case viewer.Id == userID:
		scope = "self"
	case authorizedOrganizationAudit(event.App, viewer.Id, organizationID):
		scope = "all"
	case accountHistoryPolicyOf(event.App, organizationID).ScopedView:
		scope = "scoped"
	default:
		return notFound()
	}
	limit := accountHistoryPerPage
	if raw := strings.TrimSpace(event.Request.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > accountHistoryPerPage {
			return apiError(event, http.StatusBadRequest, errorCodeOrganizationAuditInvalid, "Check the history's page size.")
		}
		limit = parsed
	}
	cursor, ok := parseAccountHistoryCursor(strings.TrimSpace(event.Request.URL.Query().Get("cursor")))
	if !ok {
		return apiError(event, http.StatusBadRequest, errorCodeOrganizationAuditInvalid, "Check the history's position.")
	}
	// Reads gathered in the last second show too (read_log_buffer.go).
	flushReadLog(event.App)
	reader := accountHistoryReader{app: event.App, organizationID: organizationID, userID: userID, viewerID: viewer.Id, scope: scope,
		full: map[string]bool{}, readable: map[string]bool{}, titles: map[string]*string{}}
	items, next := reader.page(cursor, limit)
	if viewer.Id != userID && cursor.Kind == "" {
		if err := insertOrganizationAuditEvent(event.App, organizationAuditEvent{OrganizationID: organizationID, ActorID: viewer.Id,
			Action: "account_history.view", TargetType: "user", TargetID: userID, Metadata: map[string]any{"scope": scope}}); err != nil {
			return organizationAuditError(event, err, "account-history-view")
		}
	}
	return event.JSON(http.StatusOK, accountHistoryResponse{UserID: userID, Name: userDisplayName(event.App, userID),
		Scope: scope, Items: items, NextCursor: next})
}

// accountOfOrganization is whether someone is a member of the organization.
func accountOfOrganization(app core.App, organizationID, userID string) bool {
	found := 0
	_ = app.DB().NewQuery(`SELECT
		EXISTS (SELECT 1 FROM {{leafen_organization_memberships}} WHERE [[organization]] = {:organization} AND [[user]] = {:user})`).
		Bind(dbx.Params{"organization": organizationID, "user": userID}).Row(&found)
	return found == 1
}

func authorizedOrganizationAudit(app core.App, viewerID, organizationID string) bool {
	_, err := authorizeOrganization(app, viewerID, organizationID, permissionOrganizationAuditLog)
	return err == nil
}

type accountHistoryReader struct {
	app                                     core.App
	organizationID, userID, viewerID, scope string
	full, readable                          map[string]bool
	titles                                  map[string]*string
}

// page is the history after a cursor, as many items as the viewer may see up
// to limit, and where the next page starts.
func (reader accountHistoryReader) page(cursor accountHistoryCursor, limit int) ([]accountHistoryItem, string) {
	shown := []accountHistoryItem{}
	position := cursor
	for pass := 0; pass < accountHistoryPasses; pass++ {
		batch, safe, more := reader.batch(position)
		for _, item := range batch {
			// Past the safe cut a source may have more not read yet.
			if more && safe.older(item) {
				break
			}
			position = accountHistoryCursor{At: item.At, Kind: item.Kind, ID: item.ID}
			if seen, ok := reader.seen(item); ok {
				if shown = append(shown, seen); len(shown) > limit {
					break
				}
			}
		}
		if len(shown) > limit {
			break
		}
		if !more {
			return shown, ""
		}
	}
	if len(shown) > limit {
		last := shown[limit-1]
		return shown[:limit], accountHistoryCursor{At: last.At, Kind: last.Kind, ID: last.ID}.String()
	}
	// Passes ran out with nothing more to show yet: go on from where they got.
	return shown, position.String()
}

// batch reads each source's next items after a position, newest first; safe
// is the oldest item every source has been read to, when some had more.
func (reader accountHistoryReader) batch(position accountHistoryCursor) ([]accountHistoryItem, accountHistoryCursor, bool) {
	sources := [][]accountHistoryItem{reader.audits(position, false)}
	if reader.scope != "scoped" {
		sources = append(sources, reader.audits(position, true), reader.signIns(position))
	}
	merged := []accountHistoryItem{}
	safe, more := accountHistoryCursor{}, false
	for _, items := range sources {
		merged = append(merged, items...)
		if len(items) == accountHistoryFetch {
			last := items[len(items)-1]
			cut := accountHistoryCursor{At: last.At, Kind: last.Kind, ID: last.ID}
			// The newest of the cut points: past it some source may have more.
			if !more || !safe.older(last) {
				safe, more = cut, true
			}
		}
	}
	sort.Slice(merged, func(a, b int) bool {
		left, right := merged[a], merged[b]
		if left.At != right.At {
			return left.At > right.At
		}
		if left.Kind != right.Kind {
			return left.Kind > right.Kind
		}
		return left.ID > right.ID
	})
	return merged, safe, more
}

// after is the SQL for a source's items of a kind after a position.
func accountHistoryAfter(position accountHistoryCursor, kind, at, id string, params dbx.Params) string {
	if position.Kind == "" {
		return "1"
	}
	params["cursorAt"], params["cursorId"] = position.At, strings.TrimPrefix(position.ID, kind+":")
	switch {
	case kind < position.Kind:
		return at + " <= {:cursorAt}"
	case kind == position.Kind:
		return "(" + at + " < {:cursorAt} OR (" + at + " = {:cursorAt} AND " + id + " < {:cursorId}))"
	}
	return at + " < {:cursorAt}"
}

// audits are the organization's audit events the account did, or, toMe,
// those others did to it.
func (reader accountHistoryReader) audits(position accountHistoryCursor, toMe bool) []accountHistoryItem {
	kind, who := "audit", "[[actor_id]] = {:user}"
	if toMe {
		kind, who = "to_me", "[[subject_id]] = {:user} AND [[actor_id]] != {:user}"
		// Who opened one's history one isn't told.
		if reader.scope == "self" {
			who += " AND [[action]] != 'account_history.view'"
		}
	}
	params := dbx.Params{"organization": reader.organizationID, "user": reader.userID, "limit": accountHistoryFetch}
	rows := []storedOrganizationAuditEvent{}
	_ = reader.app.DB().NewQuery(`SELECT [[id]], [[organization_id]], [[workspace_id]], [[actor_id]], [[action]], [[subject_type]],
		[[subject_id]], [[metadata_json]], [[created]], ` + organizationAuditGonePeopleSQL + ` FROM {{leafen_organization_audit_events}}
		WHERE [[organization_id]] = {:organization} AND ` + who + ` AND ` + accountHistoryAfter(position, kind, "[[created]]", "[[id]]", params) + `
		ORDER BY [[created]] DESC, [[id]] DESC LIMIT {:limit}`).Bind(params).All(&rows)
	items := make([]accountHistoryItem, 0, len(rows))
	for _, row := range rows {
		metadata := map[string]any{}
		_ = json.Unmarshal([]byte(row.MetadataJSON), &metadata)
		forgetGoneOrganizationAuditPeople(metadata, row.PersonGone, row.MemberGone)
		item := accountHistoryItem{ID: kind + ":" + row.ID, Kind: kind, Action: row.Action, At: row.OccurredAt, WorkspaceID: row.WorkspaceID,
			TargetType: row.TargetType, TargetID: row.TargetID, Metadata: metadata}
		if toMe {
			item.ActorID, item.ActorName = row.ActorID, userDisplayName(reader.app, row.ActorID)
		}
		switch row.TargetType {
		case "page", "row", "database_row":
			item.PageID = row.TargetID
		default:
			item.PageID, _ = metadata["pageId"].(string)
		}
		items = append(items, item)
	}
	return items
}

// signIns are the account's sign-in attempts.
func (reader accountHistoryReader) signIns(position accountHistoryCursor) []accountHistoryItem {
	params := dbx.Params{"user": reader.userID, "limit": accountHistoryFetch}
	// Sign-ins keep their time in nanoseconds.
	after := "1"
	if position.Kind != "" {
		params["from"], params["to"], params["cursorId"] = position.At*1_000_000, (position.At+1)*1_000_000, strings.TrimPrefix(position.ID, "sign_in:")
		switch {
		case "sign_in" < position.Kind:
			after = "[[occurred_at]] < {:to}"
		case "sign_in" == position.Kind:
			after = "([[occurred_at]] < {:from} OR ([[occurred_at]] < {:to} AND [[occurred_at]] >= {:from} AND [[id]] < {:cursorId}))"
		default:
			after = "[[occurred_at]] < {:from}"
		}
	}
	rows := []storedAuthAuditEvent{}
	_ = reader.app.DB().NewQuery(`SELECT [[id]], [[method]], [[phase]], [[outcome]], [[reason_code]], [[reason_status]], [[diagnostic_reference]], [[user_agent]], [[ip]], [[occurred_at]]
		FROM {{leafen_auth_audit_events}} WHERE [[owner_id]] = {:user} AND ` + after + `
		ORDER BY [[occurred_at]] DESC, [[id]] DESC LIMIT {:limit}`).Bind(params).All(&rows)
	items := make([]accountHistoryItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, accountHistoryItem{ID: "sign_in:" + row.ID, Kind: "sign_in", Action: row.Method, At: row.OccurredAt / 1_000_000,
			Outcome: row.Outcome, Reason: row.ReasonCode, ReasonStatus: row.ReasonStatus, Method: row.Method, Phase: row.Phase, Device: row.UserAgent})
	}
	return items
}

// seen is an item as the viewer may see it, or not at all.
func (reader accountHistoryReader) seen(item accountHistoryItem) (accountHistoryItem, bool) {
	if item.PageID != "" && item.TargetName == nil {
		item.TargetName = reader.title(item.PageID)
	}
	switch reader.scope {
	case "scoped":
		return item, item.PageID != "" && reader.hasFull(item.PageID)
	}
	return item, true
}

func (reader accountHistoryReader) hasFull(pageID string) bool {
	if known, ok := reader.full[pageID]; ok {
		return known
	}
	_, err := authorizePageAccessRole(reader.app, reader.viewerID, pageID, pageAccessRoleFullAccess)
	reader.full[pageID] = err == nil
	return err == nil
}

// title is a page's title now, "" for one without, or nil for a page gone,
// which the screen names as gone ("삭제됨") and an untitled one "제목 없음".
func (reader accountHistoryReader) title(pageID string) *string {
	if title, ok := reader.titles[pageID]; ok {
		return title
	}
	var title *string
	if page, err := reader.app.FindRecordById(pagesCollection, pageID); err == nil {
		name, _ := workspaceActivityPageName(reader.app, page)
		title = &name
	}
	reader.titles[pageID] = title
	return title
}
