package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// An MCP token's own page, as Notion's /developers/mcp-tokens/〈id〉 (seen
// 2026-11-09): its icon, name and description, its key made again with the old
// one revoked at once or after seven days, the tools it may use, whether it
// hands everything over to its agent — "모두 맡기기(묻지 않음)", off unless
// its person or, asking them once, its agent turned it on
// (mcp_token_hand_over.go) — and the pages it reaches. Its key shows again to its person, as Notion's
// eye shows it (decided 2026-11-13): kept sealed with the settings key, its
// hash checking each call (mcp_token_sealed_schema.go). Only its person sees
// and changes these, signed in — not an administrator, nor a key, which
// can't widen itself; its agent sets only its own "모두 맡기기", through its
// own route. An agent connection's page is the same less the tools and the
// pages: nothing narrows it (connection_person_rights.go).

const (
	errorCodeMCPTokenInvalid  = "LEAFEN_MCP_TOKEN_INVALID"
	errorCodeMCPTokenNotKept  = "LEAFEN_MCP_TOKEN_NOT_KEPT"
	mcpTokenDescriptionRunes  = 500
	mcpTokenMostPages         = 100
	mcpTokenRevokeLaterMillis = int64(7 * 24 * time.Hour / time.Millisecond)
	mcpTokenSettingsBodyBytes = 64 << 10
)

type mcpTokenToolView struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Enabled bool   `json:"enabled"`
}

type mcpTokenToolGroupView struct {
	ID    string             `json:"id"`
	Tools []mcpTokenToolView `json:"tools"`
}

type mcpTokenPageView struct {
	ID       string `db:"id" json:"id"`
	Title    string `db:"title" json:"title"`
	Icon     string `db:"icon" json:"icon"`
	IconType string `db:"icon_type" json:"iconType"`
	// Its kind names it while untitled (page_name_kind.go).
	PageKind     string                `db:"page_kind" json:"pageKind"`
	ViewBlock    bool                  `db:"view_block" json:"viewBlock,omitempty"`
	SourceTitles []databaseSourceTitle `db:"-" json:"sourceTitles,omitempty"`
}

type mcpTokenSettingsView struct {
	ID          string `json:"id"`
	ClientName  string `json:"clientName"`
	Description string `json:"description"`
	// An emoji or a picture's link, or "" for its first letter (connection_icon.go).
	Icon       string                       `json:"icon"`
	OAuth      bool                         `json:"oauth"`
	Workspaces []mcpConnectionListWorkspace `json:"workspaces"`
	// An agent connection, its person's in all their workspaces, rather than
	// an MCP token (connection_person_rights.go).
	Agent       bool   `json:"agent"`
	CreatedAt   int64  `json:"createdAt"`
	ExpiresAt   *int64 `json:"expiresAt"`
	TokenMadeAt int64  `json:"tokenMadeAt"`
	// Until when the key it had before still works, after a refresh that
	// revokes it later.
	PreviousUntil *int64                  `json:"previousUntil"`
	ToolGroups    []mcpTokenToolGroupView `json:"toolGroups"`
	// "모두 맡기기(묻지 않음)": the actions the key rules confirm go ahead
	// without its person's yes (mcp_action_approvals.go).
	HandsAllOver bool `json:"handsAllOver"`
	// "Notion에만 있는 도구 이름": the list names Notion's tools Leafen doesn't
	// have too (mcp_notion_only_tools.go).
	NotionOnlyTools bool `json:"notionOnlyTools"`
	// Its agent turned Notion's tools on for its list (mcp_token_notion_tools.go).
	NotionTools bool `json:"notionTools"`
	// "모든 페이지": every page its person reads, those made later too; off,
	// only Pages and what is under them (MCP token decision of 2026-11-10, 3).
	AllPages bool               `json:"allPages"`
	Pages    []mcpTokenPageView `json:"pages"`
	// Whether its key is kept to be shown again: not for one made before keys
	// were kept that the server couldn't make again (mcp_token_sealed_schema.go),
	// until it is refreshed, nor for an OAuth connection's.
	KeyKept bool `json:"keyKept"`
	// The key, in the answer that makes it again only.
	Key string `json:"key,omitempty"`
}

type storedMCPToken struct {
	ID            string `db:"id"`
	ClientName    string `db:"client_name"`
	Description   string `db:"description"`
	Icon          string `db:"icon"`
	OAuthClientID string `db:"oauth_client_id"`
	OAuthResource string `db:"oauth_resource"`
	Created       int64  `db:"created"`
	ExpiresAt     int64  `db:"token_expires_at"`
	TokenMadeAt   int64  `db:"token_made_at"`
	PreviousUntil int64  `db:"previous_token_until"`
	AccessScope   string `db:"access_scope"`
	DisabledTools string `db:"disabled_tools"`
	PageIDs       string `db:"page_ids"`
	AllPages      int64  `db:"all_pages"`
	AsksAction    bool   `db:"asks_irreversible"`
	KeyKept       bool   `db:"key_kept"`
	NotionOnly    bool   `db:"notion_only_tools"`
	NotionTools   bool   `db:"notion_tools"`
	Agent         bool   `db:"agent_connection"`
}

func registerMCPTokenSettingsRoutes(event *core.ServeEvent) {
	path := apiRoot + "/mcp/connections/{id}/settings"
	event.Router.GET(path, func(event *core.RequestEvent) error { return mcpTokenSettingsRoute(event, "read") })
	event.Router.PATCH(path, func(event *core.RequestEvent) error { return mcpTokenSettingsRoute(event, "change") }).
		Bind(apis.BodyLimit(mcpTokenSettingsBodyBytes))
	event.Router.POST(apiRoot+"/mcp/connections/{id}/token/reveal", revealMCPTokenRoute).
		Bind(apis.BodyLimit(mcpTokenSettingsBodyBytes))
	event.Router.POST(apiRoot+"/mcp/connections/{id}/token", func(event *core.RequestEvent) error { return mcpTokenSettingsRoute(event, "refresh") }).
		Bind(apis.BodyLimit(mcpTokenSettingsBodyBytes))
	event.Router.PUT(apiRoot+"/mcp/connections/{id}/pages", func(event *core.RequestEvent) error { return mcpTokenSettingsRoute(event, "pages") }).
		Bind(apis.BodyLimit(mcpTokenSettingsBodyBytes))
	registerMCPHandsAllOverRoutes(event)
	registerMCPNotionToolsRoutes(event)
	registerMCPOwnConnectionRoutes(event)
	registerMCPOwnCodeRoute(event)
	// Before a key ends, its person and its agent hear of it (mcp_token_expiry_notice.go).
	registerMCPTokenExpiryNotices(event)
}

// ownMCPToken is the token the request names, of the person signed in or
// their key's (user_key_account_routes.go: its key shown again and its pages
// are their screen's alone); answered says the request was already
// answered, refused.
func ownMCPToken(event *core.RequestEvent, screenOnly bool) (stored storedMCPToken, userID string, answered bool, err error) {
	if _, keyed := userKeyOf(event); keyed && screenOnly {
		return stored, "", true, apiError(event, http.StatusForbidden, errorCodeUserKeyClosed, "A token's page is changed by its person, logged in.")
	}
	user, ok := authenticatedUser(event)
	if !ok {
		return stored, "", true, apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	stored, err = loadMCPToken(event.App, user.Id, strings.TrimSpace(event.Request.PathValue("id")))
	if errors.Is(err, sql.ErrNoRows) {
		return stored, "", true, apiError(event, http.StatusNotFound, errorCodeMCPConnectionMissing, "MCP connection not found.")
	}
	if err != nil {
		return stored, "", true, mcpTokenSettingsFailure(event, err)
	}
	return stored, user.Id, false, nil
}

func mcpTokenSettingsRoute(event *core.RequestEvent, action string) error {
	stored, userID, answered, err := ownMCPToken(event, action == "pages")
	if answered {
		return err
	}
	key := ""
	switch action {
	case "change":
		err = changeMCPToken(event, stored)
	case "refresh":
		key, err = refreshMCPToken(event, stored)
	case "pages":
		err = chooseMCPTokenPages(event, userID, stored)
	}
	if errors.Is(err, errMCPTokenInvalid) {
		return apiError(event, http.StatusBadRequest, errorCodeMCPTokenInvalid, "Check the token's settings.")
	}
	if errors.Is(err, errAgentConnectionNotNarrowed) {
		return agentConnectionNotNarrowedRefusal(event)
	}
	// A key's call its person was asked about is answered already.
	if errors.Is(err, errUserKeyAsked) {
		return nil
	}
	if err != nil {
		return mcpTokenSettingsFailure(event, err)
	}
	if action != "read" {
		if stored, err = loadMCPToken(event.App, userID, stored.ID); err != nil {
			return mcpTokenSettingsFailure(event, err)
		}
	}
	view, err := mcpTokenSettingsViewOf(event.App, stored)
	if err != nil {
		return mcpTokenSettingsFailure(event, err)
	}
	view.Key = key
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, view)
}

var errMCPTokenInvalid = errors.New(errorCodeMCPTokenInvalid)

func mcpTokenSettingsFailure(event *core.RequestEvent, err error) error {
	return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeMCPConnectionFailed,
		"Couldn't change the MCP token.", "mcp_token_settings", "settings", errorCodeMCPConnectionFailed, err)
}

// loadMCPToken is the person's connection, live, that no app holds.
func loadMCPToken(app core.App, ownerID, id string) (storedMCPToken, error) {
	stored := storedMCPToken{}
	err := app.DB().NewQuery(`SELECT [[id]], [[client_name]], [[description]], [[icon]], [[oauth_client_id]], [[oauth_resource]], [[created]],
			[[token_expires_at]], [[token_made_at]], [[previous_token_until]], [[access_scope]], [[disabled_tools]], [[page_ids]], [[all_pages]],
			[[asks_irreversible]], [[token_sealed]] <> '' AS [[key_kept]], [[notion_only_tools]], [[notion_tools]], [[agent_connection]]
		FROM {{leafen_mcp_connections}}
		WHERE [[id]] = {:id} AND [[owner_id]] = {:owner} AND [[revoked_at]] = 0 AND [[app_install_id]] = ''`).
		Bind(dbx.Params{"id": id, "owner": ownerID}).One(&stored)
	return stored, err
}

func mcpTokenSettingsViewOf(app core.App, stored storedMCPToken) (mcpTokenSettingsView, error) {
	narrowing := connectionNarrowingOf(stored.Agent, stored.DisabledTools, stored.PageIDs, stored.AllPages)
	view := mcpTokenSettingsView{
		ID: stored.ID, ClientName: stored.ClientName, Description: stored.Description, Icon: stored.Icon,
		OAuth: stored.OAuthClientID != "", CreatedAt: stored.Created, TokenMadeAt: stored.TokenMadeAt,
		HandsAllOver: !stored.AsksAction, NotionOnlyTools: stored.NotionOnly, NotionTools: stored.NotionTools, AllPages: !narrowing.PagesNarrowed,
		Agent:      stored.Agent,
		KeyKept:    stored.KeyKept && stored.OAuthClientID == "",
		Workspaces: []mcpConnectionListWorkspace{}, ToolGroups: []mcpTokenToolGroupView{}, Pages: []mcpTokenPageView{},
	}
	if view.TokenMadeAt == 0 {
		view.TokenMadeAt = stored.Created
	}
	if stored.ExpiresAt > 0 && !view.OAuth {
		view.ExpiresAt = &stored.ExpiresAt
	}
	if stored.PreviousUntil > time.Now().UnixMilli() {
		view.PreviousUntil = &stored.PreviousUntil
	}
	if err := app.DB().NewQuery(`SELECT [[workspace]].[[id]], [[workspace]].[[name]]
		FROM {{leafen_mcp_connection_workspaces}} [[scope]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[scope]].[[workspace_id]]
		WHERE [[scope]].[[connection_id]] = {:id} ORDER BY [[workspace]].[[name]]`).
		Bind(dbx.Params{"id": stored.ID}).All(&view.Workspaces); err != nil {
		return view, err
	}
	// A tool beyond the scope it was made with shows off, as it works: a
	// token made with a lower scope before scopes gave way to its tools
	// (connection_person_rights.go).
	scope, _ := parseUserKeyScope(stored.AccessScope)
	disabled := narrowing.DisabledTools
	for _, group := range mcpToolGroups {
		tools := []mcpTokenToolView{}
		for _, tool := range mcpToolCatalog(app) {
			if tool.Group == group {
				tools = append(tools, mcpTokenToolView{Name: tool.Name, Title: tool.Title,
					Enabled: !slices.Contains(disabled, tool.Name) && mcpToolScope(tool.Name) <= scope})
			}
		}
		view.ToolGroups = append(view.ToolGroups, mcpTokenToolGroupView{ID: group, Tools: tools})
	}
	pageIDs := narrowing.PageIDs
	if len(pageIDs) > 0 {
		encoded, _ := json.Marshal(pageIDs)
		if err := app.DB().NewQuery(`SELECT [[page]].[[id]], [[page]].[[title]], [[page]].[[icon]], [[page]].[[icon_type]],
				COALESCE([[page]].[[kind]], '') AS [[page_kind]], ` + viewBlockDatabasePageSQL + ` AS [[view_block]] FROM {{leafen_pages}} [[page]]
			WHERE [[page]].[[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND ` + pageOutOfTrashPathSQL("[[page]].[[id]]") + `
			ORDER BY [[page]].[[title]], [[page]].[[id]]`).
			Bind(dbx.Params{"ids": string(encoded)}).All(&view.Pages); err != nil {
			return view, err
		}
	}
	return view, namePagesBySources(app, len(view.Pages), func(index int) string { return view.Pages[index].ID },
		func(index int, titles []databaseSourceTitle) { view.Pages[index].SourceTitles = titles })
}

// changeMCPToken renames it, describes it, sets its icon, chooses an MCP
// token's tools (an agent connection keeps them all), whether it lists
// Notion's tools or those Leafen doesn't have, or whether it hands
// everything over, asking before no action the key rules confirm; the scope
// it holds on the API follows the tools left on. Its person changes it on
// their screen, or a key of theirs in the conversation, turning "모두
// 맡기기" on only with their yes (mcpTokenSettingsStopAsking).
func changeMCPToken(event *core.RequestEvent, stored storedMCPToken) error {
	input := struct {
		ClientName    *string   `json:"clientName"`
		Description   *string   `json:"description"`
		Icon          *string   `json:"icon"`
		DisabledTools *[]string `json:"disabledTools"`
		// Its person, here, turns it on or off at once.
		HandsAllOver    *bool `json:"handsAllOver"`
		NotionOnlyTools *bool `json:"notionOnlyTools"`
		NotionTools     *bool `json:"notionTools"`
	}{}
	if event.BindBody(&input) != nil {
		return errMCPTokenInvalid
	}
	change := dbx.Params{}
	if input.ClientName != nil {
		name := strings.TrimSpace(*input.ClientName)
		if name == "" || utf8.RuneCountInString(name) > 255 {
			return errMCPTokenInvalid
		}
		change["client_name"] = name
	}
	if input.Description != nil {
		description := strings.TrimSpace(*input.Description)
		if utf8.RuneCountInString(description) > mcpTokenDescriptionRunes {
			return errMCPTokenInvalid
		}
		change["description"] = description
	}
	if input.Icon != nil {
		icon, ok := normalizeConnectionIcon(*input.Icon, event.Request)
		if !ok {
			return errMCPTokenInvalid
		}
		change["icon"] = icon
	}
	if input.DisabledTools != nil {
		// Its tools are an MCP token's to choose; an agent connection has them all.
		if stored.Agent {
			return errAgentConnectionNotNarrowed
		}
		known := map[string]bool{}
		for _, tool := range mcpToolCatalog(event.App) {
			known[tool.Name] = true
		}
		disabled := []string{}
		for _, name := range uniqueNonemptyStrings(*input.DisabledTools) {
			if !known[name] {
				return errMCPTokenInvalid
			}
			disabled = append(disabled, name)
		}
		encoded, err := json.Marshal(disabled)
		if err != nil {
			return err
		}
		change["disabled_tools"] = string(encoded)
		change["access_scope"] = userKeyScopeNames[mcpScopeForTools(event.App, disabled)]
	}
	if input.HandsAllOver != nil {
		change["asks_irreversible"] = !*input.HandsAllOver
	}
	if input.NotionOnlyTools != nil {
		change["notion_only_tools"] = *input.NotionOnlyTools
	}
	if input.NotionTools != nil {
		change["notion_tools"] = *input.NotionTools
	}
	if len(change) == 0 {
		return nil
	}
	if asked, err := askBeforeActing(event); asked {
		return errors.Join(errUserKeyAsked, err)
	}
	_, err := event.App.DB().Update(mcpConnectionsTable, change, dbx.HashExp{"id": stored.ID}).Execute()
	return err
}

// refreshMCPToken makes its key again. The old key stops at once, or works
// seven more days for the agents to be given the new one, as Notion's
// "7일 후에 철회" · "즉시 철회".
func refreshMCPToken(event *core.RequestEvent, stored storedMCPToken) (string, error) {
	input := struct {
		RevokeOld string `json:"revokeOld"`
	}{}
	if event.BindBody(&input) != nil || input.RevokeOld != "now" && input.RevokeOld != "later" {
		return "", errMCPTokenInvalid
	}
	// An OAuth connection's agent renews its own access.
	if stored.OAuthClientID != "" || stored.OAuthResource != "" {
		return "", errMCPTokenInvalid
	}
	// A key making it again asks, as its old key's end can't be undone.
	if asked, err := askBeforeActing(event); asked {
		return "", errors.Join(errUserKeyAsked, err)
	}
	key, hash, err := newHashedMCPToken("lmc_")
	if err != nil {
		return "", err
	}
	sealed, err := encryptAdministratorSetting(event.App, cipherMCPToken, key)
	if err != nil {
		return "", err
	}
	now := time.Now().UnixMilli()
	// The new key lives as long as the one it replaces did, from now
	// (mcp_token_lifetime.go).
	workspaceIDs, err := loadMCPConnectionWorkspaces(event.App, stored.ID)
	if err != nil {
		return "", err
	}
	madeAt := stored.TokenMadeAt
	if madeAt == 0 {
		madeAt = stored.Created
	}
	expiresAt, err := mcpTokenRenewedExpiry(event.App, workspaceIDs, madeAt, stored.ExpiresAt, now)
	if err != nil {
		return "", err
	}
	err = event.App.RunInTransaction(func(tx core.App) error {
		previous, until := "", int64(0)
		if input.RevokeOld == "later" {
			if err := tx.DB().NewQuery(`SELECT [[token_hash]] FROM {{leafen_mcp_connections}} WHERE [[id]] = {:id}`).
				Bind(dbx.Params{"id": stored.ID}).Row(&previous); err != nil {
				return err
			}
			until = now + mcpTokenRevokeLaterMillis
		}
		if _, err := tx.DB().Update(mcpConnectionsTable, dbx.Params{
			"token_hash": hash, "token_sealed": sealed, "token_made_at": now, "previous_token_hash": previous, "previous_token_until": until,
			"token_expires_at": expiresAt,
		}, dbx.HashExp{"id": stored.ID}).Execute(); err != nil {
			return err
		}
		// Sessions held open with the old key end now.
		notifyMCPCredentialRegistryAfterCommit(tx)
		return nil
	})
	return key, err
}

// revealMCPTokenRoute shows its key to its person, as Notion's eye does. One made
// before keys were kept that the server couldn't make again, or sealed with a
// settings key this server no longer has (a database moved without its
// .secrets), isn't kept: refreshing it makes one that is.
func revealMCPTokenRoute(event *core.RequestEvent) error {
	stored, _, answered, err := ownMCPToken(event, true)
	if answered {
		return err
	}
	sealed := ""
	if stored.OAuthClientID == "" {
		if err := event.App.DB().NewQuery(`SELECT [[token_sealed]] FROM {{leafen_mcp_connections}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": stored.ID}).Row(&sealed); err != nil {
			return mcpTokenSettingsFailure(event, err)
		}
	}
	notKept := func() error {
		return apiError(event, http.StatusNotFound, errorCodeMCPTokenNotKept, "This MCP token can't be shown again; refresh it to make one that can.")
	}
	if sealed == "" {
		return notKept()
	}
	secret, err := administratorSettingsSecret(event.App)
	if err != nil {
		return mcpTokenSettingsFailure(event, err)
	}
	key, err := security.Decrypt(sealed, secret)
	if err != nil || len(key) == 0 {
		return notKept()
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, map[string]string{"key": string(key)})
}
