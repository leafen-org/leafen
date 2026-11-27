package app

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A person value may name a bot, as Notion's people values take an
// integration's bot user — its own and any other of the workspace (seen
// 2026-11-12: written by ID with or without dashes, read back as the bot,
// found by a people filter): here a bot of one of the workspace's
// connections, public or internal, and a personal access token's own bot.
// The value keeps the bot's public ID, as a mention of one does
// (notion_block_mentions.go). The API reads it as the bot and the app names
// it as its connection is, with the connection's icon. Another person's
// token's bot is known only to that token: it is not taken, and
// reads by its ID alone to anyone but that token.

type peopleValueBot struct {
	ID   string
	Name string
	// Icon is an internal connection's emoji or picture link, "" for its
	// first letter (connection_icon.go).
	Icon string
	// TokenID is a personal access token's, "" for a connection's bot.
	TokenID string
}

// peopleValueBotIDs are the IDs among these shaped as a bot's.
func peopleValueBotIDs(ids []string) map[string]struct{} {
	bots := map[string]struct{}{}
	for _, id := range ids {
		if notionBotUserIDShape(id) {
			bots[id] = struct{}{}
		}
	}
	return bots
}

// loadPeopleValueBots reads the bots of these IDs that are the workspace's:
// its active connections' and its active personal access tokens'.
func loadPeopleValueBots(app core.App, workspaceID string, ids []string) (map[string]peopleValueBot, error) {
	wanted := peopleValueBotIDs(ids)
	found := make(map[string]peopleValueBot, len(wanted))
	if len(wanted) == 0 {
		return found, nil
	}
	// A bot's ID comes from its connection's or token's, so each is looked at.
	connections := []struct {
		ID   string `db:"id"`
		Name string `db:"client_name"`
		Icon string `db:"icon"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[grant]].[[id]], [[grant]].[[client_name]], COALESCE([[internal]].[[icon]], '') AS [[icon]]
		FROM {{leafen_notion_oauth_grants}} [[grant]]
		LEFT JOIN {{leafen_notion_internal_connections}} [[internal]] ON [[internal]].[[grant_id]] = [[grant]].[[id]]
		WHERE [[grant]].[[workspace_id]] = {:workspaceId} AND [[grant]].[[status]] = 'active'
	`).Bind(dbx.Params{"workspaceId": workspaceID}).All(&connections); err != nil {
		return nil, err
	}
	for _, connection := range connections {
		id := notionOAuthBotID(connection.ID)
		if _, asked := wanted[id]; asked {
			found[id] = peopleValueBot{ID: id, Name: connection.Name, Icon: connection.Icon}
		}
	}
	if len(found) == len(wanted) {
		return found, nil
	}
	tokens := []struct {
		ID   string `db:"id"`
		Name string `db:"name"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[name]] FROM {{leafen_notion_personal_access_tokens}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[status]] = 'active'
	`).Bind(dbx.Params{"workspaceId": workspaceID}).All(&tokens); err != nil {
		return nil, err
	}
	for _, token := range tokens {
		id := notionPersonalAccessTokenBotID(token.ID)
		if _, asked := wanted[id]; asked {
			found[id] = peopleValueBot{ID: id, Name: token.Name, TokenID: token.ID}
		}
	}
	return found, nil
}

// peopleValueBotKnownTo says whether a token may name or read the bot: a
// connection's to any, a personal access token's to that token alone.
func peopleValueBotKnownTo(bot peopleValueBot, identity notionOAuthBearerIdentity) bool {
	return bot.TokenID == "" || identity.ClientID == "personal-access-token" && identity.GrantID == bot.TokenID
}

// notionPeopleValueBotObject is a bot as a person value reads it, as Notion's
// (seen 2026-11-12): its name and an empty bot object, or the ID alone for a
// token that may not read users.
func notionPeopleValueBotObject(identity notionOAuthBearerIdentity, bot peopleValueBot) map[string]any {
	if !notionOAuthHasScope(identity, "workspace:read") || !peopleValueBotKnownTo(bot, identity) {
		return map[string]any{"object": "user", "id": bot.ID}
	}
	var name any
	if bot.Name != "" {
		name = bot.Name
	}
	return map[string]any{"object": "user", "id": bot.ID, "name": name, "avatar_url": nil, "type": "bot", "bot": map[string]any{}}
}

// notionPeopleValueUserShaped says whether Notion reads a UUID it can't find
// as a person's: an older random one (version 4), or one laid out as its own
// people's — version 8, its next digit 0 to 3, the standard variant. Any
// other it reads as a group's and refuses (seen 2026-11-12, 15 shapes and
// 24 version-8 ones: as no permission group for versions 0–3, 5,
// 7, 9 and f, a version 8 with 4 to f after it, and another variant).
func notionPeopleValueUserShaped(id string) bool {
	id = strings.ToLower(id)
	if len(id) != 36 {
		return false
	}
	standard := strings.IndexByte("89ab", id[19]) >= 0
	return id[14] == '4' || id[14] == '8' && strings.IndexByte("0123", id[15]) >= 0 && standard
}

// notionPeopleValueGroupRefused is Notion's refusal of a person value's ID it
// reads as no one's group (notionPeopleValueUserShaped).
const notionPeopleValueGroupRefused = "The request isn't valid: the ID names no permission group"
