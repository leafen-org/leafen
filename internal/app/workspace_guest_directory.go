package app

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace's guests, as Notion's 설정 → 사람 → 게스트 lists them: the
// people outside the workspace whom its pages are shared with, by account
// or by an email address no member has, each with how many pages they can
// open there. Members read the list as they read the members; guests read
// neither. Pages in the trash and shares of deleted pages don't count. The
// pages themselves are named only to a reader with full access to them, as
// Notion's list names the pages its reader may manage (seen 2026-11-09):
// the others are counted.

type storedWorkspaceGuestShare struct {
	PageID    string `db:"page_id"`
	Title     string `db:"title"`
	Icon      string `db:"icon"`
	IconType  string `db:"icon_type"`
	Role      string `db:"role"`
	CreatedAt int64  `db:"created_at"`
}

func readWorkspaceGuestDirectory(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	query := event.Request.URL.Query()
	cursor, cursorValid := decodeWorkspaceGuestCursor(query.Get("cursor"))
	if workspaceID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength || !cursorValid ||
		len(query) > 1 || (len(query) == 1 && !query.Has("cursor")) {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceMemberListInvalid, "Check the guest query.")
	}
	authorization, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead)
	if err != nil {
		if isPermissionDenied(err) {
			return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
		}
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceGuestListFailed,
			"Couldn't load the workspace guests.", "workspace_guest_directory", "authorize", errorCodeWorkspaceGuestListFailed, err)
	}
	response, err := loadWorkspaceGuestDirectory(event.App, workspaceID, user.Id, authorization.Restricted, cursor)
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceGuestListFailed,
			"Couldn't load the workspace guests.", "workspace_guest_directory", "read", errorCodeWorkspaceGuestListFailed, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	event.Response.Header().Set("Pragma", "no-cache")
	return event.JSON(http.StatusOK, response)
}

// workspaceGuestSharesSQL is a common table of the shares naming someone
// outside the workspace bound as workspaceId, on pages out of the trash with
// every page above them (pageOutOfTrashPathSQL, the one trash rule: a share
// on a page under one in the trash makes no guest until it is restored): an
// account that is neither its owner nor a member, by its own share or an
// email address it verified and proved (account_email_identity.go: an
// unproven one opens nothing it names), or an address no account has yet.
// An account is there while active in the workspace's organization, as a
// page shared with it opens (decidePageGrantWorkspace): a share left to
// someone removed from it or deactivated there opens nothing and makes no
// guest. guest_key names the guest: the account, or the address.
var workspaceGuestSharesSQL = guestSharesSQL(`[[permission]].[[workspace_id]] = {:workspaceId}`)

// guestSharesSQL is that table over the shares the filter keeps (an
// expression on [[permission]], a row of leafen_page_permissions), in any
// workspace: each share's own workspace decides who is outside it, kept as
// share_workspace_id — the one rule a workspace's guests and the server
// console's account places (administrator_account_places.go) both read.
func guestSharesSQL(filter string) string {
	return `
	WITH [[guest_share]] AS (
		SELECT [[share]].*, COALESCE([[share]].[[account_id]], 'email:' || lower(trim([[share]].[[principal_id]]))) AS [[guest_key]]
		FROM (
			SELECT [[permission]].[[principal_type]], [[permission]].[[principal_id]], [[permission]].[[role]], [[permission]].[[created_at]], [[permission]].[[created_by]],
				[[permission]].[[workspace_id]] AS [[share_workspace_id]],
				[[page]].[[id]] AS [[page_id]], [[page]].[[title]], [[page]].[[icon]], [[page]].[[icon_type]],
				[[workspace]].[[workspace_owner]],
				CASE WHEN [[permission]].[[principal_type]] = 'user' THEN [[permission]].[[principal_id]] ELSE (
					SELECT [[email]].[[user_id]] FROM {{leafen_account_emails}} [[email]]
					WHERE lower(trim([[email]].[[email]])) = lower(trim([[permission]].[[principal_id]])) AND ` + provenAccountEmailSQL("[[email]].") + `
					ORDER BY [[email]].[[user_id]] LIMIT 1
				) END AS [[account_id]]
			FROM {{leafen_page_permissions}} [[permission]]
			JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[permission]].[[page_id]]
			JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[permission]].[[workspace_id]]
			WHERE ` + filter + `
				AND [[permission]].[[principal_type]] IN ('user','email')
				AND [[page]].[[notion_import_staging]] = 0 AND ` + pageOutOfTrashPathSQL("[[page]].[[id]]") + `
		) AS [[share]]
		WHERE [[share]].[[account_id]] IS NULL OR (NOT (
			[[share]].[[account_id]] = [[share]].[[workspace_owner]]
			OR EXISTS (SELECT 1 FROM {{leafen_workspace_memberships}} [[membership]]
				WHERE [[membership]].[[workspace_id]] = [[share]].[[share_workspace_id]] AND [[membership]].[[user_id]] = [[share]].[[account_id]])
		) AND EXISTS (SELECT 1 FROM {{leafen_workspaces}} [[guest_workspace]]
			JOIN {{leafen_organization_memberships}} [[standing]] ON [[standing]].[[organization]] = [[guest_workspace]].[[organization]]
			WHERE [[guest_workspace]].[[id]] = [[share]].[[share_workspace_id]] AND [[standing]].[[user]] = [[share]].[[account_id]]
				AND [[standing]].[[status]] = 'active'))
	)`
}

type storedWorkspaceGuest struct {
	GuestKey    string  `db:"guest_key"`
	AccountID   *string `db:"account_id"`
	DisplayName string  `db:"display_name"`
	Email       string  `db:"email"`
	Avatar      *string `db:"avatar"`
	PageCount   int     `db:"page_count"`
	FirstShared int64   `db:"first_shared"`
}

// workspaceGuestCursor is where a guest directory window starts: after the
// guest first shared with at At, whose key is Key.
type workspaceGuestCursor struct {
	At  int64  `json:"a"`
	Key string `json:"k"`
}

func decodeWorkspaceGuestCursor(raw string) (*workspaceGuestCursor, bool) {
	if raw = strings.TrimSpace(raw); raw == "" {
		return nil, true
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	cursor := workspaceGuestCursor{}
	if len(raw) > 1024 || err != nil || json.Unmarshal(decoded, &cursor) != nil || cursor.Key == "" {
		return nil, false
	}
	return &cursor, true
}

// loadWorkspaceGuestDirectory reads a window of the workspace's guests in the
// order they were first shared with, from the cursor on, each with how many
// pages they can open, and how many guests there are; nextCursor goes on
// while more follow. A restricted reader sees the guests they share a place with
// (restricted_member_people.go).
func loadWorkspaceGuestDirectory(app core.App, workspaceID, readerID string, restricted bool, cursor *workspaceGuestCursor) (workspaceGuestDirectoryResponse, error) {
	visible := ""
	if restricted {
		visible = `WHERE [[guest_share]].[[account_id]] IN (` + restrictedVisiblePeopleSQL() + `)`
	}
	params := dbx.Params{"workspaceId": workspaceID, "readerId": readerID, "limit": maxWorkspaceGuestDirectoryGuests + 1}
	after := ""
	if cursor != nil {
		after = `HAVING (MIN([[guest_share]].[[created_at]]), [[guest_share]].[[guest_key]]) > ({:afterAt}, {:afterKey})`
		params["afterAt"], params["afterKey"] = cursor.At, cursor.Key
	}
	guests := []storedWorkspaceGuest{}
	if err := app.DB().NewQuery(workspaceGuestSharesSQL + `
		SELECT [[guest_share]].[[guest_key]], MAX([[guest_share]].[[account_id]]) AS [[account_id]],
			COALESCE(NULLIF(trim(MAX([[user]].[[name]])),''), MAX([[user]].[[email]]), MAX([[guest_share]].[[principal_id]])) AS [[display_name]],
			COALESCE(MAX([[user]].[[email]]), MAX([[guest_share]].[[principal_id]])) AS [[email]],
			NULLIF(MAX([[user]].[[profile_avatar]]),'') AS [[avatar]],
			COUNT(DISTINCT [[guest_share]].[[page_id]]) AS [[page_count]],
			MIN([[guest_share]].[[created_at]]) AS [[first_shared]]
		FROM [[guest_share]]
		LEFT JOIN {{users}} [[user]] ON [[user]].[[id]] = [[guest_share]].[[account_id]]
		` + visible + `
		GROUP BY [[guest_share]].[[guest_key]]
		` + after + `
		ORDER BY MIN([[guest_share]].[[created_at]]), [[guest_share]].[[guest_key]]
		LIMIT {:limit}
	`).Bind(params).All(&guests); err != nil {
		return workspaceGuestDirectoryResponse{}, err
	}
	response := workspaceGuestDirectoryResponse{WorkspaceID: workspaceID, Guests: []workspaceGuestDirectoryItem{}}
	if err := app.DB().NewQuery(workspaceGuestSharesSQL + `
		SELECT COUNT(DISTINCT [[guest_share]].[[guest_key]]) FROM [[guest_share]] ` + visible,
	).Bind(params).Row(&response.Total); err != nil {
		return workspaceGuestDirectoryResponse{}, err
	}
	if len(guests) > maxWorkspaceGuestDirectoryGuests {
		guests = guests[:maxWorkspaceGuestDirectoryGuests]
		last := guests[len(guests)-1]
		encoded, _ := json.Marshal(workspaceGuestCursor{At: last.FirstShared, Key: last.GuestKey})
		response.NextCursor = base64.RawURLEncoding.EncodeToString(encoded)
	}
	for _, guest := range guests {
		item := workspaceGuestDirectoryItem{
			DisplayName: guest.DisplayName, Email: guest.Email, Avatar: guest.Avatar, PageCount: guest.PageCount,
		}
		if guest.AccountID != nil {
			item.UserID = *guest.AccountID
		}
		response.Guests = append(response.Guests, item)
	}
	return response, nil
}

// readWorkspaceGuestPages lists the pages one guest can open in the
// workspace, a window at a time from the cursor, for the guest's page menu
// (GET .../guests/pages?guest=<account or email:address>): those the reader
// has full access to by title, the others only counted as noAccess, as
// Notion's menu ends with "공유된 페이지 또는 개인 페이지 N개 · 사용 권한 없음".
func readWorkspaceGuestPages(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	query := event.Request.URL.Query()
	guest := normalizedWorkspaceGuestKey(query.Get("guest"))
	afterAt, afterPage := int64(0), ""
	if raw := strings.TrimSpace(query.Get("cursor")); raw != "" {
		at, page, found := strings.Cut(raw, ":")
		parsed, err := strconv.ParseInt(at, 10, 64)
		if !found || err != nil || page == "" {
			guest = ""
		}
		afterAt, afterPage = parsed, page
	}
	if workspaceID == "" || guest == "" || len(guest) > 512 {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceMemberListInvalid, "Check the guest query.")
	}
	authorization, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead)
	if err != nil {
		if isPermissionDenied(err) {
			return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
		}
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceGuestListFailed,
			"Couldn't load the guest's pages.", "workspace_guest_directory", "authorize", errorCodeWorkspaceGuestListFailed, err)
	}
	visible := ""
	if authorization.Restricted {
		visible = `AND [[guest_share]].[[account_id]] IN (` + restrictedVisiblePeopleSQL() + `)`
	}
	// A page shared with the guest's account and an address they proved is one page.
	shares := []storedWorkspaceGuestShare{}
	if err := event.App.DB().NewQuery(workspaceGuestSharesSQL + `
		SELECT [[page_id]], MAX([[title]]) AS [[title]], MAX([[icon]]) AS [[icon]], MAX([[icon_type]]) AS [[icon_type]],
			MAX([[role]]) AS [[role]], MIN([[created_at]]) AS [[created_at]]
		FROM [[guest_share]]
		WHERE [[guest_key]] = {:guest} ` + visible + `
		GROUP BY [[page_id]]
		HAVING (MIN([[created_at]]), [[page_id]]) > ({:afterAt}, {:afterPage})
		ORDER BY MIN([[created_at]]), [[page_id]] LIMIT {:limit}
	`).Bind(dbx.Params{"workspaceId": workspaceID, "readerId": user.Id, "guest": guest, "afterAt": afterAt, "afterPage": afterPage,
		"limit": maxWorkspaceGuestDirectoryGuests + 1}).All(&shares); err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceGuestListFailed,
			"Couldn't load the guest's pages.", "workspace_guest_directory", "pages", errorCodeWorkspaceGuestListFailed, err)
	}
	response := workspaceGuestPagesResponse{Pages: []workspaceGuestPage{}}
	if len(shares) > maxWorkspaceGuestDirectoryGuests {
		shares = shares[:maxWorkspaceGuestDirectoryGuests]
		last := shares[len(shares)-1]
		response.NextCursor = strconv.FormatInt(last.CreatedAt, 10) + ":" + last.PageID
	}
	for _, share := range shares {
		access, err := authorizePageAccessRole(event.App, user.Id, share.PageID, pageAccessRoleView)
		if isPermissionDenied(err) || (err == nil && access.AccessRole < pageAccessRoleFullAccess) {
			response.NoAccess++
			continue
		}
		if err != nil {
			return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceGuestListFailed,
				"Couldn't load the guest's pages.", "workspace_guest_directory", "page-access", errorCodeWorkspaceGuestListFailed, err)
		}
		response.Pages = append(response.Pages, workspaceGuestPage{
			PageID: share.PageID, Title: share.Title, Icon: share.Icon, IconType: share.IconType, Role: share.Role,
		})
	}
	// Each named as the app names a page with no title, by its kind.
	pageIDs := make([]string, 0, len(response.Pages))
	for _, page := range response.Pages {
		pageIDs = append(pageIDs, page.PageID)
	}
	kinds, err := loadPageNameKinds(event.App, pageIDs)
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceGuestListFailed,
			"Couldn't load the guest's pages.", "workspace_guest_directory", "page-kinds", errorCodeWorkspaceGuestListFailed, err)
	}
	for index := range response.Pages {
		named := kinds[response.Pages[index].PageID]
		response.Pages[index].PageKind, response.Pages[index].ViewBlock, response.Pages[index].SourceTitles = named.Kind, named.ViewBlock, named.SourceTitles
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

// normalizedWorkspaceGuestKey is a guest as the directory names them: an
// account id, or "email:" and an address in lower case.
func normalizedWorkspaceGuestKey(raw string) string {
	guest := strings.TrimSpace(raw)
	if address, isEmail := strings.CutPrefix(guest, "email:"); isEmail {
		address = strings.ToLower(strings.TrimSpace(address))
		if address == "" {
			return ""
		}
		return "email:" + address
	}
	return guest
}
