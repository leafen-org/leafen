package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A person mention in page Markdown reads as Notion's reads one (seen
// 2026-11-11 through its public API, both versions, and its MCP fetch): by
// the person's public ID — their email after it, "/" between, for a reader
// that may read people's emails — with their name now: "v2" and the MCP
// <mention url="user://ID/EMAIL">NAME</mention>, "v1"
// <mention-user url="user://ID/EMAIL">NAME</mention-user>. One of an ID no
// person of the workspace has reads by the ID alone, unnamed, and so does
// every mention for a connection that may not read people. Leafen's own
// Markdown (an export's, an administrator's, an import's) names people by
// their Leafen IDs, as it does pages.
//
// Markdown written to a page names a person by that address, its email and
// words ignored: the mention reads with the person's name, and one of an ID
// no person here has — someone who has left too — stays a mention of it,
// named "@Anonymous", as Notion's keeps one (seen 2026-11-11). An address
// Notion's Markdown takes for no one's (notionMarkdownPersonAddress) is
// refused by "v2" and the MCP, and a "v1" tag of it reads as the app's
// address (version1MentionWords); standing bare in text it is words.

// notionUnknownPersonMentionText is the words a mention of an ID no person
// of the workspace has is kept with, as Notion's API reads one.
const notionUnknownPersonMentionText = "@Anonymous"

// notionIdentityReadsPeople is how much of the workspace's people a Public
// API token reads, as a connection's "user capabilities" say in Notion: an
// internal connection set to read user information with their email
// addresses reads both, one set to read it without them their names alone,
// and one set to read none — any token without workspace:read — no one.
// Another token reading people (an OAuth connection, a personal access
// token) reads their emails, as it always has.
func notionIdentityReadsPeople(app core.App, identity notionOAuthBearerIdentity) (people, emails bool) {
	if !notionOAuthHasScope(identity, "workspace:read") {
		return false, false
	}
	if !notionInternalConnection(identity) {
		return true, true
	}
	stored := ""
	if err := app.DB().NewQuery(`SELECT [[capabilities_json]] FROM {{leafen_notion_internal_connections}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": strings.TrimPrefix(identity.ClientID, internalConnectionClientPrefix)}).Row(&stored); err != nil {
		return true, false
	}
	capabilities := internalConnectionCapabilities{}
	if json.Unmarshal([]byte(stored), &capabilities) != nil {
		return true, false
	}
	return true, capabilities.UserInfo == "email"
}

// notionIdentityEmail is a person's email as a token reads it: none for one
// that may not read people's emails.
func notionIdentityEmail(app core.App, identity notionOAuthBearerIdentity, email string) string {
	if _, emails := notionIdentityReadsPeople(app, identity); !emails {
		return ""
	}
	return email
}

// person names a mentioned person as the reader reads them
// (notionMarkdownMentions.Person); a failure to read who they are is kept
// in failed.
func (reader notionMarkdownReader) person(app core.App, workspaceID string, failed *error) func(string, string) (string, string, bool) {
	people := map[string]storedNotionDirectoryUser{}
	return func(userID, text string) (string, string, bool) {
		if userID == "" {
			return "", "", false
		}
		// A connection's bot, or a personal access token's, reads by its ID
		// and its name, whoever reads it, so what is read and written back
		// keeps it (notion_mention_held.go) — Notion's Markdown writes
		// nothing for one (seen 2026-11-10), and its write-back lost the
		// mention. One page Markdown kept as no one's reads by its ID
		// (notionUnknownPersonMention).
		if name, bot := notionMarkdownBotMentionName(app, workspaceID, userID); bot && !notionUnknownPersonMention(userID, text) {
			if reader.NoPeople {
				name = ""
			}
			return "user://" + strings.TrimSpace(userID), name, true
		}
		if reader.InternalIDs && !reader.APIForm && !reader.Version2 {
			return "user://" + userID, "", true
		}
		publicID, err := notionCompatibleUserID(userID)
		if err != nil {
			// One kept by an ID no person here has reads by that ID, as
			// kept (notionKeptUUID).
			if _, isUUID := normalizeNotionUUIDRequest(userID); isUUID {
				return "user://" + strings.TrimSpace(userID), "", true
			}
			return "", "", false
		}
		address := "user://" + publicID
		if reader.NoPeople {
			return address, "", true
		}
		person, known := people[userID]
		if !known {
			found, err := notionPeopleInWorkspace(app, workspaceID, []string{userID})
			if err != nil && *failed == nil {
				*failed = err
			}
			person = found[userID]
			// One a copy or a move brought from another workspace reads by
			// the name it kept, for a reader of what keeps it
			// (workspace_carried_people.go).
			if person.Name == "" && reader.PrincipalID != "" {
				carried, _, err := carriedNotionDirectoryUser(app, reader.PrincipalID, workspaceID, userID)
				if err != nil && *failed == nil {
					*failed = err
				}
				person.Name = carried.Name
			}
			people[userID] = person
		}
		if email := strings.TrimSpace(person.Email); reader.PeopleEmails && email != "" {
			address += "/" + email
		}
		return address, strings.TrimSpace(person.Name), true
	}
}

// notionMarkdownBotMentionName is the name of the workspace's connection's
// or personal access token's bot a mention names; false for no bot of one.
func notionMarkdownBotMentionName(app core.App, workspaceID, userID string) (string, bool) {
	if bot, found := notionMentionBot(app, workspaceID, userID); found {
		return bot.Name, true
	}
	bot, found := notionTokenMentionBot(app, workspaceID, userID)
	return bot.Name, found
}

// notionKeptUUID is an ID no person or page here has, as a mention keeps it,
// as Notion's do (seen 2026-11-11): one written with its dashes as written,
// in the case written ("57268221-03E3-…" stays so in the mention and its
// address read back), and one written without them in their place, in
// small letters. False for one that is no UUID. Compared, an ID is the same
// in any case (normalizeNotionUUIDRequest): an ID of a person or a page that
// is here names them in any case, where Notion's names no one.
func notionKeptUUID(written string) (string, bool) {
	written = strings.TrimSpace(written)
	normalized, isUUID := normalizeNotionUUIDRequest(written)
	if isUUID && len(written) == len(normalized) {
		return written, true
	}
	return normalized, isUUID
}

// notionUnknownPersonMention is whether a person mention is one kept by an
// ID no person of the workspace has, as page Markdown keeps one — of a
// connection's bot too, which Notion's Markdown takes for no one's (seen
// 2026-11-11: user://<a bot's ID> is "@Anonymous", read back by the ID):
// a UUID kept with notionUnknownPersonMentionText, as the editor tells one.
func notionUnknownPersonMention(userID, text string) bool {
	_, isUUID := normalizeNotionUUIDRequest(userID)
	return text == notionUnknownPersonMentionText && isUUID
}

// notionMarkdownUserAddressID is the person a user:// address names: the ID
// after its scheme, before any "/" and email Notion writes after it.
func notionMarkdownUserAddressID(address string) (string, bool) {
	rest, user := strings.CutPrefix(strings.TrimSpace(address), "user://")
	id, _, _ := strings.Cut(rest, "/")
	return strings.TrimSpace(id), user
}

// notionMarkdownUserAddressValid is whether "v2" and the MCP read an address
// as a mention: any but a user:// one (in any case, after spaces), and that
// one only if it names a person (notionMarkdownPersonAddress).
func notionMarkdownUserAddressValid(address string) bool {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(address)), "user://") {
		return true
	}
	_, person := notionMarkdownPersonAddress(address)
	return person
}

// notionMarkdownPersonAddress reads a person's address as Notion's Markdown
// reads one, in both versions and the MCP (seen 2026-11-11): "user://"
// written so, an ID it takes for a person's (notionMarkdownPersonUUID), and
// after a "/" maybe the email its reader writes there — anything holding an
// "@" and no other "/". It gives the ID; false for any other address
// ("USER://…", " user://…", "user://ID/", "user://ID/x"), which is no one's.
func notionMarkdownPersonAddress(address string) (string, bool) {
	rest, user := strings.CutPrefix(address, "user://")
	if !user {
		return "", false
	}
	id, email, emailed := strings.Cut(rest, "/")
	if emailed && (!strings.Contains(email, "@") || strings.Contains(email, "/")) {
		return "", false
	}
	return id, notionMarkdownPersonUUID(id)
}

// notionMarkdownPersonUUID is whether Notion's Markdown takes an ID for a
// person's (seen 2026-11-11): a UUID with its dashes, in either case, of
// version 4 or 8 and the RFC 4122 variant — as Notion's and Leafen's
// people's IDs are — or one of Notion's own accounts'
// (00000000-0000-0000-0000-000000000000 to …00a, Leafen's automations' one
// among them) or the max UUID. A known person's ID without its dashes, or a
// UUID of another version or variant, names no one.
func notionMarkdownPersonUUID(id string) bool {
	id = strings.ToLower(id)
	if _, isUUID := normalizeNotionUUIDRequest(id); !isUUID || len(id) != 36 {
		return false
	}
	if account, own := strings.CutPrefix(id, "00000000-0000-0000-0000-00000000000"); own {
		return strings.Contains("0123456789a", account)
	}
	return id == "ffffffff-ffff-ffff-ffff-ffffffffffff" ||
		(id[14] == '4' || id[14] == '8') && strings.IndexByte("89ab", id[19]) >= 0
}

// resolveMarkdownPersonMentions names the person mentions of Markdown
// someone writes through the MCP tools, which name a person by the public
// ID their people lookup gives (or, as before, by their Leafen ID), by the
// person's Leafen ID, with their name now: one of an ID no person of the
// workspace has — a connection's bot's too, as Notion's Markdown takes it —
// kept by that ID as "@Anonymous", and an address naming no one its words;
// one the page holds already, as it is held (notion_mention_held.go).
func resolveMarkdownPersonMentions(app core.App, workspaceID string, held *notionHeldMentions, updates []graphTextUpdate) error {
	mentioned := []*graphTextSpan{}
	userIDs := []string{}
	for updateIndex := range updates {
		for spanIndex := range updates[updateIndex].Spans {
			span := &updates[updateIndex].Spans[spanIndex]
			if span.Mention != "person" {
				continue
			}
			written := strings.ToLower(strings.TrimSpace(span.UserID))
			switch internal, err := resolveNotionCompatibleUserID(written); {
			case validNotionCompatibleInternalID(strings.TrimSpace(span.UserID)):
				span.UserID = strings.TrimSpace(span.UserID)
			case err == nil:
				span.UserID = internal
			default:
				if kept, _ := notionKeptUUID(span.UserID); notionMarkdownPersonUUID(written) {
					span.UserID, span.Text = kept, notionUnknownPersonMentionText
					if err := keepHeldPersonMention(held, span, written); err != nil {
						return err
					}
					continue
				}
				span.Mention, span.UserID = "", ""
				continue
			}
			mentioned = append(mentioned, span)
			userIDs = append(userIDs, span.UserID)
		}
	}
	if len(mentioned) == 0 {
		return nil
	}
	people, err := notionPeopleInWorkspace(app, workspaceID, userIDs)
	if err != nil {
		return err
	}
	for _, span := range mentioned {
		// One without a name reads by their ID, as the Public API's writes
		// name one (notionMentionPreparation).
		person, found := people[span.UserID]
		switch name := strings.TrimSpace(person.Name); {
		case name != "":
			span.Text = "@" + strings.TrimPrefix(name, "@")
		case found:
			publicID, _ := notionCompatibleUserID(span.UserID)
			span.Text = "@" + publicID
		default:
			// One who has left is kept by their public ID, as one no
			// person here has (notionUnknownPersonMention).
			written := span.UserID
			if publicID, err := notionCompatibleUserID(span.UserID); err == nil {
				span.UserID = publicID
			}
			span.Text = notionUnknownPersonMentionText
			if err := keepHeldPersonMention(held, span, written); err != nil {
				return err
			}
		}
	}
	return nil
}

// keepHeldPersonMention keeps a mention the page holds of the person an ID
// written names as it is held, by the ID it keeps and its words.
func keepHeldPersonMention(held *notionHeldMentions, span *graphTextSpan, written string) error {
	userID, words, found, err := held.mention(written)
	if found {
		span.UserID, span.Text = userID, words
	}
	return err
}
