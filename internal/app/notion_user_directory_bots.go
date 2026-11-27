package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace's public connections are bot users in its user directory, as
// Notion lists a workspace's integrations and finds them by ID: the asking
// connection's own bot in full, as GET /v1/users/me gives it, and the others
// with an empty bot object. The list gives the people first, then the bots
// in their connections' order, however many.

const (
	// notionUserDirectoryBotPhase follows every user ID, so a cursor past the
	// people reads bots only.
	notionUserDirectoryBotPhase = "~"
	// A bot is found by its ID a window of connections at a time.
	notionDirectoryBotWindow = 500
)

type notionDirectoryBot struct {
	ID      string
	GrantID string
	Name    string
}

// loadNotionDirectoryBots is up to limit bots of a workspace's active
// connections after the connection given, in their connections' order.
func loadNotionDirectoryBots(app core.App, workspaceID, afterGrantID string, limit int) ([]notionDirectoryBot, error) {
	grants := []struct {
		ID   string `db:"id"`
		Name string `db:"client_name"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[client_name]] FROM {{leafen_notion_oauth_grants}}
		WHERE [[workspace_id]]={:workspaceId} AND [[status]]='active' AND [[id]] > {:after}
		ORDER BY [[id]] LIMIT {:limit}
	`).Bind(dbx.Params{"workspaceId": workspaceID, "after": afterGrantID, "limit": limit}).All(&grants); err != nil {
		return nil, err
	}
	bots := make([]notionDirectoryBot, 0, len(grants))
	for _, grant := range grants {
		bots = append(bots, notionDirectoryBot{ID: notionOAuthBotID(grant.ID), GrantID: grant.ID, Name: grant.Name})
	}
	return bots, nil
}

// notionSameUserID tells whether a public user ID names the one given, with
// or without its dashes.
func notionSameUserID(publicID, id string) bool {
	wanted := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(publicID)), "-", "")
	return len(wanted) == 32 && wanted == strings.ReplaceAll(id, "-", "")
}

// findNotionDirectoryBot is the workspace bot a public user ID names, with
// or without its dashes.
func findNotionDirectoryBot(app core.App, workspaceID, publicID string) (notionDirectoryBot, bool, error) {
	wanted := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(publicID)), "-", "")
	if len(wanted) != 32 {
		return notionDirectoryBot{}, false, nil
	}
	// A bot's ID comes from its connection's, so every connection is looked at.
	after := ""
	for {
		bots, err := loadNotionDirectoryBots(app, workspaceID, after, notionDirectoryBotWindow)
		if err != nil {
			return notionDirectoryBot{}, false, err
		}
		for _, bot := range bots {
			if strings.ReplaceAll(bot.ID, "-", "") == wanted {
				return bot, true, nil
			}
		}
		if len(bots) < notionDirectoryBotWindow {
			return notionDirectoryBot{}, false, nil
		}
		after = bots[len(bots)-1].GrantID
	}
}

// notionDirectoryBotObject is a bot's user object for the asker: in full
// for the asking connection's own bot, else with an empty bot object.
func notionDirectoryBotObject(
	app core.App, bot notionDirectoryBot, identity notionOAuthBearerIdentity, origin string,
) (map[string]any, error) {
	if identity.ClientID != "personal-access-token" && bot.GrantID == identity.GrantID {
		user, err := app.FindRecordById("users", identity.PrincipalID)
		if err != nil {
			return nil, err
		}
		owner, err := notionUserObject(user, origin)
		if err != nil {
			return nil, err
		}
		return notionOAuthBotUser(app, bot.GrantID, owner)
	}
	var name any
	if bot.Name != "" {
		name = bot.Name
	}
	return map[string]any{
		"object": "user", "id": bot.ID, "type": "bot", "name": name, "avatar_url": nil,
		"bot": map[string]any{},
	}, nil
}

// serveNotionDirectoryBotRetrieve answers GET /v1/users/{id} for a bot of
// the asker's workspace — a personal access token's own bot too, as GET
// /v1/users/me gives it, as Notion's token reads its own bot by its ID (seen
// 2026-11-12) — and not found for any other ID.
func serveNotionDirectoryBotRetrieve(
	event *core.RequestEvent, identity notionOAuthBearerIdentity, publicUserID string,
) error {
	if _, err := authorizeWorkspace(event.App, identity.PrincipalID, identity.WorkspaceID, permissionWorkspaceRead); err != nil {
		if isPermissionDenied(err) {
			return notionUnauthorized(event)
		}
		return notionRequestNotCompleted(event, err)
	}
	if identity.ClientID == "personal-access-token" && notionSameUserID(publicUserID, notionPersonalAccessTokenBotID(identity.GrantID)) {
		return serveNotionOAuthUsersMe(event)
	}
	bot, found, err := findNotionDirectoryBot(event.App, identity.WorkspaceID, publicUserID)
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	if !found {
		return notionUserNotFound(event)
	}
	response, err := notionDirectoryBotObject(event.App, bot, identity, requestOrigin(event.Request))
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}
