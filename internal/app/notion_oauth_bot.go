package app

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A public connection is its own bot user, as a Notion public connection
// is: GET /v1/users/me answers with the bot, whose owner is the person who
// connected it (an internal connection's, its workspace), with the
// workspace it reaches and the largest file it may upload there, and the
// token exchange names the bot by the same ID. A
// personal access token acts as the person who made it, and its own
// GET /v1/users/me answers with the token's bot user, owned by that person,
// as Notion's does ("Retrieve token's bot user", 2026-10-29).

var errNotionOAuthBotMissing = errors.New("Notion-compatible connection bot missing")

// notionOAuthBotID is a connection's bot user ID, a UUID its grant fixes.
// Scheme nibble 3 keeps it apart from object identities (2) and the
// reversible codecs (1).
func notionOAuthBotID(grantID string) string {
	return notionBotUserID("leafen:notion-oauth-bot:v1\x00" + grantID)
}

// notionPersonalAccessTokenBotID is a token's bot user ID, the same way.
func notionPersonalAccessTokenBotID(tokenID string) string {
	return notionBotUserID("leafen:notion-pat-bot:v1\x00" + tokenID)
}

// notionBotUserIDShape tells a public user ID made as a bot's is: scheme
// nibble 3, the version and variant bits a bot's ID carries.
func notionBotUserIDShape(id string) bool {
	decoded, err := parseNotionUserUUID(notionPublicUserID(id))
	return err == nil && decoded[0]>>4 == 3 && decoded[6]>>4 == 8 && decoded[8]>>6 == 2
}

// notionMentionBot is the workspace's connection bot a mention names, as
// Notion's text mentions an integration's bot user (seen 2026-11-10).
func notionMentionBot(app core.App, workspaceID, userID string) (notionDirectoryBot, bool) {
	if !notionBotUserIDShape(userID) {
		return notionDirectoryBot{}, false
	}
	bot, found, err := findNotionDirectoryBot(app, workspaceID, userID)
	return bot, err == nil && found
}

// notionMentionBotFor is the bot a mention names, as whoever writes or reads
// it knows it: a connection's (notionMentionBot), or a personal access
// token's own, which that token alone knows (peopleValueBotKnownTo)
// and mentions in text as Notion's integration mentions its own bot.
func notionMentionBotFor(app core.App, identity notionOAuthBearerIdentity, workspaceID, userID string) (notionDirectoryBot, bool) {
	if bot, found := notionMentionBot(app, workspaceID, userID); found {
		return bot, true
	}
	if bot, found := notionTokenMentionBot(app, workspaceID, userID); found && peopleValueBotKnownTo(bot, identity) {
		return notionDirectoryBot{ID: bot.ID, Name: bot.Name}, true
	}
	return notionDirectoryBot{}, false
}

// notionTokenMentionBot is the bot of the workspace's personal access token a
// mention names.
func notionTokenMentionBot(app core.App, workspaceID, userID string) (peopleValueBot, bool) {
	id, valid := normalizeNotionUUIDRequest(strings.TrimSpace(userID))
	if id = strings.ToLower(id); !valid || !notionBotUserIDShape(id) {
		return peopleValueBot{}, false
	}
	bots, err := loadPeopleValueBots(app, workspaceID, []string{id})
	bot, found := bots[id]
	return bot, err == nil && found && bot.TokenID != ""
}

func notionBotUserID(seed string) string {
	digest := sha256.Sum256([]byte(seed))
	digest[0] = digest[0]&0x0f | 0x30
	digest[6] = digest[6]&0x0f | 0x80
	digest[8] = digest[8]&0x3f | 0x80
	var encoded [16]byte
	copy(encoded[:], digest[:16])
	return formatNotionUserUUID(encoded)
}

// notionOAuthBotUser is a connection's bot user object; owner is the person
// who connected it, as a user object, which an internal connection's bot
// doesn't name.
func notionOAuthBotUser(app core.App, grantID string, owner map[string]any) (map[string]any, error) {
	grant, found, err := findNotionOAuthGrantByID(app, grantID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errNotionOAuthBotMissing
	}
	// An internal connection's is owned by its workspace, not by the person
	// who made it, as Notion's internal integration's is (seen 2026-11-11):
	// no person, and so no email, its user capability might not read.
	if strings.HasPrefix(grant.ClientID, internalConnectionClientPrefix) {
		owner = nil
	}
	return notionBotUser(app, notionOAuthBotID(grant.ID), grant.ClientName, grant.WorkspaceID, owner)
}

// notionPersonalAccessTokenBotUser is a token's bot user object, named as
// the token is; owner is the person who made it.
func notionPersonalAccessTokenBotUser(app core.App, tokenID string, owner map[string]any) (map[string]any, error) {
	token := storedNotionPersonalAccessToken{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[workspace_id]], [[name]] FROM {{leafen_notion_personal_access_tokens}}
		WHERE [[id]] = {:tokenId} LIMIT 1
	`).Bind(dbx.Params{"tokenId": tokenID}).One(&token)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotionOAuthBotMissing
	}
	if err != nil {
		return nil, err
	}
	return notionBotUser(app, notionPersonalAccessTokenBotID(token.ID), token.Name, token.WorkspaceID, owner)
}

func notionBotUser(app core.App, id, displayName, workspaceRecordID string, owner map[string]any) (map[string]any, error) {
	workspace, err := app.FindRecordById(workspacesCollection, workspaceRecordID)
	if err != nil {
		return nil, err
	}
	workspaceID, err := notionCompatibleWorkspaceID(workspaceRecordID)
	if err != nil {
		return nil, err
	}
	var name any
	if displayName != "" {
		name = displayName
	}
	ownedBy := map[string]any{"type": "user", "user": owner}
	if owner == nil {
		ownedBy = map[string]any{"type": "workspace", "workspace": true}
	}
	return map[string]any{
		"object": "user", "id": id, "type": "bot",
		"name": name, "avatar_url": nil,
		"bot": map[string]any{
			"owner":          ownedBy,
			"workspace_name": workspace.GetString("name"),
			"workspace_id":   workspaceID,
			"workspace_limits": map[string]any{
				"max_file_upload_size_in_bytes": fileTransferMaxFileBytes,
			},
		},
	}, nil
}
