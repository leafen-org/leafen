package app

import (
	"context"
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// A comment names the connection it was written through — an MCP client's,
// a key's, a public connection's or a personal access token's — as a version
// of the page does (page_history_connections.go), so that what counts it
// names the agent beside its person, as Notion's get-version-changes does
// (seen 2026-11-22, workers w2334 and w2339: a page comment and a block
// comment an agent left through Notion's MCP count as {author, source mcp,
// source_name} for the title or the block they were left on, and an
// integration's page comment as the integration, {source_name}). A comment
// written in the app names none. Every comment a request writes is named
// where it is stored (insertCommentRow), by the transaction that stores it.

// createConnectedComment stores a new comment the person writes, here or
// through a connection, as createCommentRecordForTarget does, naming the
// connection ctx writes through (createCommentRecordAs).
func createConnectedComment(
	ctx context.Context, app core.App, actorID string, target nativeCommentTarget, input commentCreateInput,
	authorize func(core.App) (commentAuthorization, error),
) (commentMutationResponse, error) {
	releases := []func(){}
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	return createCommentRecordAs(app, actorID, target, input, func(txApp core.App) (commentAuthorization, error) {
		releases = append(releases, namePageHistoryConnection(txApp, ctx))
		return authorize(txApp)
	}, true)
}

// createConnectedPageComment stores a new comment on a page as
// createCommentRecord does, naming the connection ctx writes through.
func createConnectedPageComment(
	ctx context.Context, app core.App, actorID, pageID string, input commentCreateInput,
) (commentMutationResponse, error) {
	return createConnectedComment(ctx, app, actorID, nativePageCommentTarget(pageID), input, func(txApp core.App) (commentAuthorization, error) {
		return authorizeCommentPage(txApp, actorID, pageID, permissionWorkspaceWrite, "")
	})
}

// commentConnectionOf is the connection the comments a transaction stores
// were written through; none for the app's.
func commentConnectionOf(txApp core.App) pageHistoryConnection {
	connection, _ := pageHistoryConnections.Load(txApp)
	named, _ := connection.(pageHistoryConnection)
	return named
}

// ensureNativeCommentConnectionsSchema gives each comment the connection it
// was written through: none for those written before.
func ensureNativeCommentConnectionsSchema(app core.App) error {
	for _, statement := range []string{
		`ALTER TABLE {{leafen_comments}} ADD COLUMN [[connection_id]] TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE {{leafen_comments}} ADD COLUMN [[connection_name]] TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := app.DB().NewQuery(statement).Execute(); err != nil {
			return err
		}
	}
	return validateNativeCommentConnectionsSchema(app)
}

func validateNativeCommentConnectionsSchema(app core.App) error {
	var columns int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM pragma_table_info('leafen_comments')
		WHERE [[name]] IN ('connection_id', 'connection_name') AND [[notnull]] = 1`).Row(&columns); err != nil {
		return err
	}
	if columns != 2 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}
