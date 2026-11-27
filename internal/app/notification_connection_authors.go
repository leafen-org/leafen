package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A notification a connection's comment made — of the comment, or of a
// mention in it — names its writer as the comment's header does: the
// connection, by the name it wrote as, on the connection's rounded square in
// the inbox, as Notion's inbox names an integration's mention (seen
// 2026-11-12). Its actor stays the person whose token wrote it, which the API
// and an agent's inbox read.
// The comment keeps how it was written (notionStoredCommentMetadata) and
// never changes it, and a notification shows only while its comment is
// there (commentShownSQL), so the notification reads it from the comment.

// notificationConnectionAuthors reads, of the comments the given
// notifications came from, how the ones a connection wrote as itself show
// their writer, by comment: its name, a custom one's with the connection
// named beside it. A person's comment, and one a connection wrote as its
// person, are not among them.
func notificationConnectionAuthors(app core.App, commentIDs []string) map[string]notificationActorConnection {
	ids := make([]any, 0, len(commentIDs))
	seen := map[string]bool{}
	for _, id := range commentIDs {
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	authors := map[string]notificationActorConnection{}
	if len(ids) == 0 {
		return authors
	}
	rows := []struct {
		ID   string `db:"id"`
		Body string `db:"body_json"`
	}{}
	if app.DB().Select("id", "body_json").From(commentsTable).
		Where(dbx.In("id", ids...)).
		AndWhere(dbx.NewExp("instr([[body_json]], '\"notion\"') > 0")).
		All(&rows) != nil {
		return authors
	}
	for _, row := range rows {
		body := struct {
			Notion *notionStoredCommentMetadata `json:"notion"`
		}{}
		if json.Unmarshal([]byte(row.Body), &body) != nil || body.Notion == nil {
			continue
		}
		name := strings.TrimSpace(body.Notion.ResolvedName)
		kind := body.Notion.DisplayNameType
		if name == "" || (kind != "integration" && kind != "custom") {
			continue
		}
		author := notificationActorConnection{DisplayNameType: kind, ResolvedName: name}
		if kind == "custom" {
			author.ConnectionName = strings.TrimSpace(body.Notion.ConnectionName)
		}
		authors[row.ID] = author
	}
	return authors
}

// notificationConnectionAuthorWords is a connection's name in a notice sent
// as words, as the comment's header says it: "알림봇 (〈연결〉에서 제공)",
// "Alert bot (via 〈connection〉)" for a custom name.
func notificationConnectionAuthorWords(language string, author notificationActorConnection) string {
	if author.DisplayNameType != "custom" || author.ConnectionName == "" {
		return author.ResolvedName
	}
	if language == accountLanguageKo {
		return author.ResolvedName + " (" + author.ConnectionName + "에서 제공)"
	}
	return author.ResolvedName + " (via " + author.ConnectionName + ")"
}

// notificationFromComment is whether a notice is a comment's: the comment
// or a mention in it (notificationCommentKindSQL).
func notificationFromComment(kind, commentID string) bool {
	return commentID != "" && (kind == "comment" || kind == "mention")
}

// notificationInboxConnectionAuthors names, on each inbox notice a
// connection's comment made, the connection that wrote it.
func notificationInboxConnectionAuthors(app core.App, items []notificationInboxRecord) {
	commentIDs := make([]string, 0, len(items))
	for _, item := range items {
		if notificationFromComment(item.Kind, item.CommentID) {
			commentIDs = append(commentIDs, item.CommentID)
		}
	}
	authors := notificationConnectionAuthors(app, commentIDs)
	for index := range items {
		if author, ok := authors[items[index].CommentID]; ok && notificationFromComment(items[index].Kind, items[index].CommentID) {
			items[index].ActorConnection = &author
		}
	}
}

// notificationConnectionActorKey is where loadNotificationEmailActors keeps
// the words naming a connection's comment's notice, beside its people's ids.
func notificationConnectionActorKey(commentID string) string {
	return "comment:" + commentID
}

// A notice another write of a connection's made — a mention in a page's
// text, a person a property newly names, a database item's change — keeps
// the connection with it (actor_connection), from the write's transaction
// (pageHistoryConnectionOf) or its block's last edit (pageBlockLastEdits),
// as the page's history names it: Notion's inbox names an integration's
// mention in a page and a person it sets in a property by the integration
// (seen 2026-11-12). An agent connected as its person writes as them.

// noticeConnection is the name a notice a connection's write made keeps:
// an integration's — a Notion-compatible connection or a personal access
// token, which Notion's screens name alone, as its bot — and "" for a
// person's write or an agent's.
func noticeConnection(connection pageHistoryConnection) string {
	if pageHistoryConnectionKind(connection.ID) != "integration" {
		return ""
	}
	return strings.TrimSpace(connection.Name)
}

// noticeKeptConnection is how a notice that keeps its writing connection
// names its writer, as a connection's comment's does; nil for a person's.
func noticeKeptConnection(name string) *notificationActorConnection {
	if name == "" {
		return nil
	}
	return &notificationActorConnection{DisplayNameType: "integration", ResolvedName: name}
}

// notificationEmailItemActor names who a notice sent as words — an email, a
// push, a Slack message, a desktop notice — came from: one a connection
// wrote the connection, any other its actor.
func notificationEmailItemActor(language string, actors map[string]string, item storedNotificationEmailItem) string {
	if name := notificationEmailLine(item.ActorConnection, 80); name != "" {
		return name
	}
	if notificationFromComment(item.Kind, item.CommentID) {
		if name := notificationEmailLine(actors[notificationConnectionActorKey(item.CommentID)], 80); name != "" {
			return name
		}
	}
	return notificationEmailActor(language, actors, item.ActorID)
}
