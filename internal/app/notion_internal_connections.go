package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A workspace's internal connection, as Notion's "API 토큰" connection (seen
// 2026-10-30, /developers): a name, a static token for the Notion-compatible
// API, what it may do ("기능") and the pages it reaches, which its page
// menus' "연결" add. It is a connection grant (notion_oauth_*.go) whose
// client is the connection itself, so its bot, its page list and how it is
// kept to them are a public connection's; it acts within its maker's rights,
// and stops when they can't act or leave. Its token is in
// notion_internal_connection_tokens.go.

const (
	internalConnectionCollectionPath = "/workspaces/{workspaceId}/internal-connections"
	internalConnectionItemPath       = internalConnectionCollectionPath + "/{connectionId}"
	internalConnectionTokenPath      = internalConnectionItemPath + "/token"
	internalConnectionPagesPath      = internalConnectionItemPath + "/pages"
	internalConnectionRevealPath     = internalConnectionItemPath + "/token/reveal"
	internalConnectionSecretPrefix   = "ntn_"
	internalConnectionSecretSize     = 32
	internalConnectionMaxNameRunes   = 100
	internalConnectionMaxPerSpace    = 50
	internalConnectionBodyBytes      = 4 << 10

	errorCodeInternalConnectionInvalid  = "LEAFEN_INTERNAL_CONNECTION_INVALID"
	errorCodeInternalConnectionNotFound = "LEAFEN_INTERNAL_CONNECTION_NOT_FOUND"
	errorCodeInternalConnectionLimit    = "LEAFEN_INTERNAL_CONNECTION_LIMIT"
	errorCodeInternalConnectionFailed   = "LEAFEN_INTERNAL_CONNECTION_FAILED"
)

var (
	errInternalConnectionInvalid  = errors.New(errorCodeInternalConnectionInvalid)
	errInternalConnectionNotFound = errors.New(errorCodeInternalConnectionNotFound)
	errInternalConnectionLimit    = errors.New(errorCodeInternalConnectionLimit)
)

// internalConnectionCapabilities are Notion's "기능": content read, update
// and insert, comments read and insert, and how much of its people it reads.
type internalConnectionCapabilities struct {
	ReadContent    bool   `json:"readContent"`
	UpdateContent  bool   `json:"updateContent"`
	InsertContent  bool   `json:"insertContent"`
	ReadComments   bool   `json:"readComments"`
	InsertComments bool   `json:"insertComments"`
	UserInfo       string `json:"userInfo"`
}

// A new connection reads, updates and inserts content and reads people
// without their email, as Notion's starts.
var defaultInternalConnectionCapabilities = internalConnectionCapabilities{
	ReadContent: true, UpdateContent: true, InsertContent: true, UserInfo: "no_email",
}

// errInternalConnectionName is a connection's name that can't be kept.
var errInternalConnectionName = refuse(errInternalConnectionInvalid, "name is 1–%d characters", internalConnectionMaxNameRunes)

func (capabilities internalConnectionCapabilities) valid() bool {
	return capabilities.UserInfo == "none" || capabilities.UserInfo == "no_email" || capabilities.UserInfo == "email"
}

// scopes are the connection scopes its capabilities allow: updating and
// inserting are one write here.
func (capabilities internalConnectionCapabilities) scopes() []string {
	scopes := []string{}
	if capabilities.ReadContent {
		scopes = append(scopes, "pages:read", "databases:read", "files:read")
	}
	if capabilities.UpdateContent || capabilities.InsertContent {
		scopes = append(scopes, "pages:write", "databases:write", "files:write")
	}
	if capabilities.UpdateContent {
		scopes = append(scopes, notionContentUpdateScope)
	}
	if capabilities.InsertContent {
		scopes = append(scopes, notionContentInsertScope)
	}
	if capabilities.ReadComments {
		scopes = append(scopes, "comments:read")
	}
	if capabilities.InsertComments {
		scopes = append(scopes, "comments:write")
	}
	if capabilities.UserInfo != "none" {
		scopes = append(scopes, "workspace:read")
	}
	return scopes
}

type storedInternalConnection struct {
	ID               string `db:"id"`
	WorkspaceID      string `db:"workspace_id"`
	CreatorID        string `db:"creator_id"`
	GrantID          string `db:"grant_id"`
	Name             string `db:"name"`
	Icon             string `db:"icon"`
	CapabilitiesJSON string `db:"capabilities_json"`
	TokenHint        string `db:"token_hint"`
	TokenMadeAt      int64  `db:"token_made_at"`
	PreviousUntil    int64  `db:"previous_token_until"`
	CreatedAt        int64  `db:"created_at"`
	UpdatedAt        int64  `db:"updated_at"`
	CreatorName      string `db:"creator_name"`
	Pages            int    `db:"pages"`
}

type internalConnectionResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// An emoji or a picture's link, or "" for its first letter (connection_icon.go).
	Icon string `json:"icon"`
	// The grant a page's "연결" adds it to.
	GrantID      string                         `json:"grantId"`
	Capabilities internalConnectionCapabilities `json:"capabilities"`
	TokenHint    string                         `json:"tokenHint"`
	// When the token was made, or last made again: Notion's "마지막으로
	// 새로고침함".
	TokenMadeAt int64 `json:"tokenMadeAt"`
	// Until when the token it had before still works, after a refresh that
	// revokes it in seven days; absent once it has stopped.
	PreviousUntil int64                     `json:"previousUntil,omitzero"`
	Creator       workspaceConnectionPerson `json:"creator"`
	Pages         int                       `json:"pages"`
	CreatedAt     int64                     `json:"createdAt"`
	// The token, in the answer that makes it only.
	Secret string `json:"secret,omitempty"`
}

type internalConnectionInput struct {
	Name         *string                               `json:"name"`
	Icon         *string                               `json:"icon"`
	Capabilities *internalConnectionCapabilitiesChange `json:"capabilities"`
}

// internalConnectionCapabilitiesChange is capabilities as a request writes
// them: those it names change, those it leaves out keep what they were —
// a new connection's, Notion's first.
type internalConnectionCapabilitiesChange struct {
	ReadContent    *bool   `json:"readContent"`
	UpdateContent  *bool   `json:"updateContent"`
	InsertContent  *bool   `json:"insertContent"`
	ReadComments   *bool   `json:"readComments"`
	InsertComments *bool   `json:"insertComments"`
	UserInfo       *string `json:"userInfo"`
}

// internalConnectionFields reads the name, icon and capabilities a request
// gives a connection over those it has — a new one's, none, no icon and
// Notion's first capabilities — making one as changing one does, and
// refuses what can't be kept.
func internalConnectionFields(
	input internalConnectionInput, request *http.Request, name, icon string, capabilities internalConnectionCapabilities,
) (string, string, internalConnectionCapabilities, error) {
	if input.Name != nil {
		name = strings.TrimSpace(*input.Name)
	}
	iconValid := true
	if input.Icon != nil {
		icon, iconValid = normalizeConnectionIcon(*input.Icon, request)
	}
	if change := input.Capabilities; change != nil {
		for _, given := range []struct {
			value *bool
			into  *bool
		}{
			{change.ReadContent, &capabilities.ReadContent}, {change.UpdateContent, &capabilities.UpdateContent},
			{change.InsertContent, &capabilities.InsertContent}, {change.ReadComments, &capabilities.ReadComments},
			{change.InsertComments, &capabilities.InsertComments},
		} {
			if given.value != nil {
				*given.into = *given.value
			}
		}
		if change.UserInfo != nil {
			capabilities.UserInfo = *change.UserInfo
		}
	}
	switch {
	case name == "" || utf8.RuneCountInString(name) > internalConnectionMaxNameRunes:
		return name, icon, capabilities, errInternalConnectionName
	case !iconValid:
		return name, icon, capabilities, refuse(errInternalConnectionInvalid, "icon is an emoji, an image address or an uploaded image of the workspace")
	case !capabilities.valid():
		return name, icon, capabilities, refuse(errInternalConnectionInvalid, "capabilities.userInfo is none, no_email or email, not %q", capabilities.UserInfo)
	}
	return name, icon, capabilities, nil
}

func registerInternalConnectionRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+internalConnectionCollectionPath, listInternalConnections)
	event.Router.POST(apiRoot+internalConnectionCollectionPath, createInternalConnection).Bind(apis.BodyLimit(internalConnectionBodyBytes))
	event.Router.PATCH(apiRoot+internalConnectionItemPath, updateInternalConnection).Bind(apis.BodyLimit(internalConnectionBodyBytes))
	event.Router.DELETE(apiRoot+internalConnectionItemPath, deleteInternalConnection)
	event.Router.POST(apiRoot+internalConnectionTokenPath, renewInternalConnectionToken).Bind(apis.BodyLimit(internalConnectionBodyBytes))
	event.Router.GET(apiRoot+internalConnectionPagesPath, listInternalConnectionPages)
	event.Router.POST(apiRoot+internalConnectionRevealPath, revealInternalConnectionToken).Bind(apis.BodyLimit(internalConnectionBodyBytes))
	registerInternalConnectionOwnerRoutes(event)
	registerInternalConnectionWebhookRoutes(event)
}

func internalConnectionResponseOf(stored storedInternalConnection) internalConnectionResponse {
	capabilities := defaultInternalConnectionCapabilities
	_ = json.Unmarshal([]byte(stored.CapabilitiesJSON), &capabilities)
	return internalConnectionResponse{
		ID: stored.ID, Name: stored.Name, Icon: stored.Icon, GrantID: stored.GrantID, Capabilities: capabilities,
		TokenHint: stored.TokenHint, TokenMadeAt: stored.TokenMadeAt, PreviousUntil: previousTokenUntil(stored.PreviousUntil), Creator: workspaceConnectionPerson{ID: stored.CreatorID, Name: stored.CreatorName},
		Pages: stored.Pages, CreatedAt: stored.CreatedAt,
	}
}

// previousTokenUntil is until when the token before a refresh still works,
// or 0 once it has stopped.
func previousTokenUntil(until int64) int64 {
	if until <= time.Now().UTC().UnixMilli() {
		return 0
	}
	return until
}

const internalConnectionColumns = `[[connection]].[[id]], [[connection]].[[workspace_id]], [[connection]].[[creator_id]],
	[[connection]].[[grant_id]], [[connection]].[[name]], [[connection]].[[icon]], [[connection]].[[capabilities_json]], [[connection]].[[token_hint]],
	[[connection]].[[token_made_at]], [[connection]].[[previous_token_until]], [[connection]].[[created_at]], [[connection]].[[updated_at]], COALESCE([[person]].[[name]], '') AS [[creator_name]],
	(SELECT COUNT(*) FROM {{leafen_notion_oauth_grant_resources}} [[resource]] WHERE [[resource]].[[grant_id]] = [[connection]].[[grant_id]]) AS [[pages]]`

// listInternalConnections lists the workspace's internal connections its
// person manages — those they own, or all for the workspace's owner.
func listInternalConnections(event *core.RequestEvent) error {
	user, workspaceID, authorization, err := internalConnectionRequest(event, permissionWorkspaceRead)
	if err != nil {
		return internalConnectionError(event, err, "list")
	}
	rows := []storedInternalConnection{}
	if err := event.App.DB().NewQuery(`SELECT ` + internalConnectionColumns + `
		FROM {{leafen_notion_internal_connections}} [[connection]]
		LEFT JOIN {{users}} [[person]] ON [[person]].[[id]] = [[connection]].[[creator_id]]
		WHERE [[connection]].[[workspace_id]] = {:workspaceId} AND ({:owner} OR EXISTS (
			SELECT 1 FROM {{leafen_notion_internal_connection_owners}} [[owned]]
			WHERE [[owned]].[[connection_id]] = [[connection]].[[id]] AND [[owned]].[[user_id]] = {:userId}))
		ORDER BY [[connection]].[[created_at]], [[connection]].[[id]]`).Bind(dbx.Params{
		"workspaceId": workspaceID, "userId": user.Id, "owner": authorization.Role == "owner",
	}).All(&rows); err != nil {
		return internalConnectionError(event, err, "list")
	}
	items := make([]internalConnectionResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, internalConnectionResponseOf(row))
	}
	return event.JSON(http.StatusOK, map[string]any{"items": items})
}

// createInternalConnection makes a connection for a workspace's member,
// within its policy on tokens, with the icon and capabilities it is given —
// Notion's first for those it leaves out — and its token.
func createInternalConnection(event *core.RequestEvent) error {
	user, workspaceID, authorization, err := internalConnectionRequest(event, permissionWorkspaceWrite)
	if err != nil {
		return internalConnectionError(event, err, "create")
	}
	input := internalConnectionInput{}
	if err := event.BindBody(&input); err != nil {
		return internalConnectionError(event, bodyRefusal(errInternalConnectionInvalid, "connection", err), "create-bind")
	}
	if input.Name == nil {
		return internalConnectionError(event, errInternalConnectionName, "create-bind")
	}
	name, icon, capabilities, err := internalConnectionFields(input, event.Request, "", "", defaultInternalConnectionCapabilities)
	if err != nil {
		return internalConnectionError(event, err, "create-validate")
	}
	if err := requireWorkspacePolicy(event.App, workspaceID, user.Id, authorization.Role, personalAccessTokensLimit, errPersonalAccessTokenOwners); err != nil {
		return internalConnectionError(event, err, "create-policy")
	}
	secret, err := newInternalConnectionSecret()
	if err != nil {
		return internalConnectionError(event, err, "create-secret")
	}
	encoded, _ := json.Marshal(capabilities)
	id, grantID, now := security.RandomString(24), security.RandomString(24), time.Now().UTC().UnixMilli()
	err = event.App.RunInTransaction(func(txApp core.App) error {
		count := 0
		if err := txApp.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_notion_internal_connections}} WHERE [[workspace_id]] = {:workspaceId}`).
			Bind(dbx.Params{"workspaceId": workspaceID}).Row(&count); err != nil {
			return err
		}
		if count >= internalConnectionMaxPerSpace {
			return errInternalConnectionLimit
		}
		if _, err := txApp.DB().Insert(notionOAuthGrantsTable, dbx.Params{
			"id": grantID, "owner_id": user.Id, "client_id": internalConnectionClientPrefix + id, "client_name": name,
			"workspace_id": workspaceID, "scope": strings.Join(capabilities.scopes(), " "),
			"credential_generation": 1, "status": "active", "created_at": now, "updated_at": now, "revoked_at": 0,
		}).Execute(); err != nil {
			return err
		}
		sealed, err := sealInternalConnectionSecret(txApp, secret)
		if err != nil {
			return err
		}
		_, err = txApp.DB().Insert("leafen_notion_internal_connections", dbx.Params{
			"id": id, "workspace_id": workspaceID, "creator_id": user.Id, "grant_id": grantID, "name": name, "icon": icon,
			"capabilities_json": string(encoded), "token_hash": notionPersonalAccessTokenHash(secret),
			"token_hint": internalConnectionTokenHint(secret), "token_sealed": sealed, "token_made_at": now, "created_at": now, "updated_at": now,
		}).Execute()
		if err != nil {
			return err
		}
		return addInternalConnectionOwnerRow(txApp, id, workspaceID, user.Id, user.Id)
	})
	if err != nil {
		return internalConnectionError(event, err, "create")
	}
	stored, err := loadInternalConnection(event.App, workspaceID, id)
	if err != nil {
		return internalConnectionError(event, err, "create-read")
	}
	response := internalConnectionResponseOf(stored)
	response.Secret = secret
	return event.JSON(http.StatusCreated, response)
}

// updateInternalConnection names the connection, sets its icon or sets its
// capabilities, which its grant's scopes follow at once.
func updateInternalConnection(event *core.RequestEvent) error {
	stored, err := managedInternalConnection(event)
	if err != nil {
		return internalConnectionError(event, err, "update")
	}
	input := internalConnectionInput{}
	if err := event.BindBody(&input); err != nil {
		return internalConnectionError(event, bodyRefusal(errInternalConnectionInvalid, "connection change", err), "update-bind")
	}
	if input.Name == nil && input.Icon == nil && input.Capabilities == nil {
		return internalConnectionError(event, refuse(errInternalConnectionInvalid, "the change names none of name, icon and capabilities"), "update-bind")
	}
	name, icon, capabilities, err := internalConnectionFields(
		input, event.Request, stored.Name, stored.Icon, internalConnectionResponseOf(stored).Capabilities,
	)
	if err != nil {
		return internalConnectionError(event, err, "update-validate")
	}
	encoded, _ := json.Marshal(capabilities)
	now := time.Now().UTC().UnixMilli()
	err = event.App.RunInTransaction(func(txApp core.App) error {
		if _, err := txApp.DB().NewQuery(`UPDATE {{leafen_notion_internal_connections}}
			SET [[name]] = {:name}, [[icon]] = {:icon}, [[capabilities_json]] = {:capabilities}, [[updated_at]] = {:now} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"name": name, "icon": icon, "capabilities": string(encoded), "now": now, "id": stored.ID}).Execute(); err != nil {
			return err
		}
		_, err := txApp.DB().NewQuery(`UPDATE {{leafen_notion_oauth_grants}}
			SET [[client_name]] = {:name}, [[scope]] = {:scope}, [[updated_at]] = {:now} WHERE [[id]] = {:grantId}`).
			Bind(dbx.Params{"name": name, "scope": strings.Join(capabilities.scopes(), " "), "now": now, "grantId": stored.GrantID}).Execute()
		return err
	})
	if err != nil {
		return internalConnectionError(event, err, "update")
	}
	// Its bot's mentions read with the new name; the search index reads them again.
	if name != stored.Name {
		refreshBotMentionsSoon(event.App, stored.WorkspaceID, notionOAuthBotID(stored.GrantID), stored.Name)
	}
	updated, err := loadInternalConnection(event.App, stored.WorkspaceID, stored.ID)
	if err != nil {
		return internalConnectionError(event, err, "update-read")
	}
	return event.JSON(http.StatusOK, internalConnectionResponseOf(updated))
}

// deleteInternalConnection removes the connection; its grant is revoked,
// so its token and page list stop with it.
func deleteInternalConnection(event *core.RequestEvent) error {
	stored, err := managedInternalConnection(event)
	if err != nil {
		return internalConnectionError(event, err, "delete")
	}
	// A key's call asks its person once it is found (user_key_acting_ask.go).
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	err = event.App.RunInTransaction(func(txApp core.App) error {
		if _, err := txApp.DB().NewQuery(`UPDATE {{leafen_notion_oauth_grants}}
			SET [[status]] = 'revoked', [[revoked_at]] = {:now}, [[updated_at]] = {:now} WHERE [[id]] = {:grantId}`).
			Bind(dbx.Params{"now": now, "grantId": stored.GrantID}).Execute(); err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_notion_oauth_grant_resources}} WHERE [[grant_id]] = {:grantId}`).
			Bind(dbx.Params{"grantId": stored.GrantID}).Execute(); err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_notion_internal_connection_owners}} WHERE [[connection_id]] = {:id}`).
			Bind(dbx.Params{"id": stored.ID}).Execute(); err != nil {
			return err
		}
		// Its webhook subscriptions go with it.
		if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_webhook_subscriptions}} WHERE [[connection_id]] = {:connection}
			AND [[request_id]] LIKE {:request}`).Bind(dbx.Params{"connection": internalConnectionWebhookConnectionID(stored),
			"request": internalConnectionWebhookRequest + "%"}).Execute(); err != nil {
			return err
		}
		_, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_notion_internal_connections}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": stored.ID}).Execute()
		return err
	})
	if err != nil {
		return internalConnectionError(event, err, "delete")
	}
	return event.NoContent(http.StatusNoContent)
}

// listInternalConnectionPages lists the pages the connection reaches as its
// "콘텐츠 사용 권한" names them: those its page menus added, with what is
// under them.
func listInternalConnectionPages(event *core.RequestEvent) error {
	stored, err := managedInternalConnection(event)
	if err != nil {
		return internalConnectionError(event, err, "pages")
	}
	pages := []struct {
		ID       string `db:"id" json:"id"`
		Title    string `db:"title" json:"title"`
		Icon     string `db:"icon" json:"icon"`
		IconType string `db:"icon_type" json:"iconType"`
		// Its kind names it while untitled (page_name_kind.go).
		PageKind     string                `db:"page_kind" json:"pageKind"`
		ViewBlock    bool                  `db:"view_block" json:"viewBlock,omitempty"`
		SourceTitles []databaseSourceTitle `db:"-" json:"sourceTitles,omitempty"`
	}{}
	if err := event.App.DB().NewQuery(`SELECT [[page]].[[id]], [[page]].[[title]], [[page]].[[icon]], [[page]].[[icon_type]],
			COALESCE([[page]].[[kind]], '') AS [[page_kind]], ` + viewBlockDatabasePageSQL + ` AS [[view_block]]
		FROM {{leafen_notion_oauth_grant_resources}} [[resource]]
		JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[resource]].[[resource_id]] AND NOT [[page]].[[in_trash]]
		WHERE [[resource]].[[grant_id]] = {:grantId} ORDER BY [[page]].[[title]], [[page]].[[id]]`).
		Bind(dbx.Params{"grantId": stored.GrantID}).All(&pages); err != nil {
		return internalConnectionError(event, err, "pages")
	}
	if err := namePagesBySources(event.App, len(pages), func(index int) string { return pages[index].ID },
		func(index int, titles []databaseSourceTitle) { pages[index].SourceTitles = titles }); err != nil {
		return internalConnectionError(event, err, "pages")
	}
	return event.JSON(http.StatusOK, map[string]any{"items": pages})
}
