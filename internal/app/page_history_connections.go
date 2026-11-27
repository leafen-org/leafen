package app

import (
	"context"
	"github.com/pocketbase/dbx"
	"strings"
	"sync"

	"github.com/pocketbase/pocketbase/core"
)

// A version a public connection or an MCP client makes names the connection
// beside the person, as Notion's version history does (seen 2026-10-01:
// "〈이름〉이(가) ChatGPT를 통해" on a change made through a connection, the
// person alone on one made in the app). The name is the one the connection
// had when it made the change. A personal access token acts as its person
// and names nothing.
//
// The connection rides on the request's context. A transaction that writes
// for such a request names it for the versions it records, and recording
// reads it by the transaction, so the write paths beneath need not carry it.

const (
	maxPageHistoryConnectionNameRunes = 100
)

// pageHistoryConnection is the connection a version was made through: its
// name as it was then, and which connection it is, so its changes can be
// found again however it is renamed.
type pageHistoryConnection struct {
	Name string
	ID   string
	// AppRun is the app's run a key's call writes for (page_history_app_runs.go).
	AppRun string
}

// pageHistoryConnectionName is the connection a request writes through: its
// MCP client's, its key's, its public connection's or its personal access
// token's (2026-10-16: a token's changes named only its person, though the
// organization's audit log names the token); none for the app. A connection
// without a name reads as its kind.
func pageHistoryConnectionName(app core.App, ctx context.Context) pageHistoryConnection {
	if ctx == nil {
		return pageHistoryConnection{}
	}
	if authority, ok := ctx.Value(mcpAuthorityContextKey{}).(mcpAuthority); ok {
		return pageHistoryConnection{Name: pageHistoryConnectionLabel(authority.ClientName, "MCP"), ID: "mcp:" + authority.ConnectionID}
	}
	if key, ok := ctx.Value(userKeyContextKey{}).(userKeyAuthority); ok {
		return pageHistoryConnection{Name: pageHistoryConnectionLabel(key.Name, "API"), ID: "mcp:" + key.ConnectionID, AppRun: appRunIDOf(ctx)}
	}
	if token, ok := ctx.Value(personalAccessTokenContextKey{}).(notionOAuthBearerIdentity); ok {
		var name string
		if err := app.DB().NewQuery(`SELECT [[name]] FROM {{leafen_notion_personal_access_tokens}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": token.GrantID}).Row(&name); err != nil {
			return pageHistoryConnection{}
		}
		return pageHistoryConnection{Name: pageHistoryConnectionLabel(name, "API"), ID: "pat:" + token.GrantID}
	}
	identity, ok := connectionAuthorFrom(ctx)
	if !ok {
		return pageHistoryConnection{}
	}
	grant, found, err := findNotionOAuthGrantByID(app, notionConnectionGrant(identity))
	if err != nil || !found {
		return pageHistoryConnection{}
	}
	return pageHistoryConnection{Name: pageHistoryConnectionLabel(grant.ClientName, "API"), ID: "notion:" + grant.ID}
}

// pageHistoryConnectionKind is what kind of connection made a version: an
// integration — a Notion-compatible connection or a personal access token,
// which Notion's screens name alone, as its bot (seen 2026-11-05) — or an
// agent connected as its person, named beside them; "" for the app.
func pageHistoryConnectionKind(connectionID string) string {
	switch {
	case strings.HasPrefix(connectionID, "notion:"), strings.HasPrefix(connectionID, "pat:"):
		return "integration"
	case strings.HasPrefix(connectionID, "mcp:"):
		return "agent"
	}
	return ""
}

func pageHistoryConnectionLabel(name, fallback string) string {
	runes := []rune(strings.TrimSpace(name))
	if len(runes) == 0 {
		return fallback
	}
	return string(runes[:min(len(runes), maxPageHistoryConnectionNameRunes)])
}

// pageHistoryConnections holds, by open transaction, the connection the
// versions it records name.
var pageHistoryConnections sync.Map

// namePageHistoryConnection makes the versions txApp records name the
// connection ctx writes through, and carry its call's moment
// (page_history_moments.go), until the returned release; a transaction
// already naming one keeps it.
func namePageHistoryConnection(txApp core.App, ctx context.Context) func() {
	releaseMoment := holdPageHistoryCallMoment(txApp, ctx)
	connection := pageHistoryConnectionName(txApp, ctx)
	if connection.Name == "" {
		return releaseMoment
	}
	if _, held := pageHistoryConnections.LoadOrStore(txApp, connection); held {
		return releaseMoment
	}
	return func() {
		pageHistoryConnections.Delete(txApp)
		releaseMoment()
	}
}

// withPageHistoryConnection makes the versions fn records in txApp name the
// connection, for one socket's update in a batch of several.
func withPageHistoryConnection(txApp core.App, connection pageHistoryConnection, fn func() error) error {
	if connection.Name == "" {
		return fn()
	}
	previous, held := pageHistoryConnections.Load(txApp)
	pageHistoryConnections.Store(txApp, connection)
	defer func() {
		if held {
			pageHistoryConnections.Store(txApp, previous)
		} else {
			pageHistoryConnections.Delete(txApp)
		}
	}()
	return fn()
}

// pageHistoryConnectionOf is the connection the versions app records name.
func pageHistoryConnectionOf(app core.App) pageHistoryConnection {
	if keyed, ok := app.(userKeyApp); ok {
		return pageHistoryConnectionName(keyed.App, keyed.ctx)
	}
	held, _ := pageHistoryConnections.Load(app)
	connection, _ := held.(pageHistoryConnection)
	return connection
}

// userKeyApp is the app a call made with a key runs against: each of its
// transactions names the key's connection for the versions it records.
type userKeyApp struct {
	core.App
	ctx context.Context
}

func (app userKeyApp) RunInTransaction(fn func(txApp core.App) error) error {
	return app.App.RunInTransaction(func(txApp core.App) error {
		defer namePageHistoryConnection(txApp, app.ctx)()
		return fn(txApp)
	})
}
