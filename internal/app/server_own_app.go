package app

import "github.com/pocketbase/pocketbase/core"

// A part a call first makes and keeps past it — the read log's buffer, the
// sign-in sessions' and the agents' connections' last uses — writes later as
// the server, never through the app the call worked with: that app may be a
// request's or an MCP call's, a transaction's or a read snapshot's, and a
// transaction's or a snapshot's answers every write after it ended
// "transaction has already been committed or rolled back" (2026-11-19: an
// agent's SQL query was the first read the read log noted after a start,
// inside its turn's transaction; every later second's records failed and
// were lost, and the server's stop ended as failed with that bare line). The
// server notes its own app in the store every app over it shares, as it is
// made (NewWithDataDir).

const serverOwnAppStoreKey = "leafen.serverOwnApp"

// keepServerOwnApp notes app as the server's own.
func keepServerOwnApp(app core.App) {
	app.Store().Set(serverOwnAppStoreKey, app)
}

// serverAppOf is the server's own app under any app a call works with.
// Where none was noted (an app not made by NewWithDataDir), it unwraps the
// request's and the MCP call's apps and a read snapshot's. It reads the
// store, so it is asked before a store's GetOrSet, never inside the part it
// makes (the store holds its lock there).
func serverAppOf(app core.App) core.App {
	if own, ok := app.Store().Get(serverOwnAppStoreKey).(core.App); ok {
		return own
	}
	switch kept := app.(type) {
	case requestWriteApp:
		return serverAppOf(kept.App)
	case *mcpCallApp:
		return serverAppOf(kept.App)
	case *readSnapshotApp:
		return serverAppOf(kept.App)
	}
	return app
}
