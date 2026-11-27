package app

import (
	"strconv"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A one-time code gives its key once, and so does an allowed device request
// — but not to nobody (user 2026-11-21: on a Windows PC without bash, an
// agent ran the connection text's command rewritten into PowerShell; the
// code was traded, the key's save into its settings broke on PowerShell's
// quotes, the retry was refused as a used code, and a connection whose key
// no one held stayed in Settings for its person to delete). While the key
// it gave was never used, the same code trades again within its ten
// minutes — traded by the same program (its name) from the same address,
// never by another agent the same text went to — and the same request
// gives a key again within its own (its device code is its agent's alone):
// the earlier connection is disconnected in that step and a new one made,
// so a command whose save failed runs again with no new text and leaves
// nothing behind. A key used once — by any call, a connect command's check of the
// connections an agent holds among them (GET /mcp/connections/current) —
// is never taken back: the code or request is used for good then, as
// before. A connection its person disconnected stays so: its code gives
// nothing more.

// mcpConnectionNeverUsed says whether the owner's connection is live and its
// key was never used, and, with a name, that it is that program's; false
// when it can't be read.
func mcpConnectionNeverUsed(app core.App, ownerID, connectionID, clientName string) bool {
	var stored struct {
		ClientName string `db:"client_name"`
		RevokedAt  int64  `db:"revoked_at"`
		LastUsedAt int64  `db:"last_used_at"`
	}
	if connectionID == "" || app.DB().NewQuery(`SELECT [[client_name]], [[revoked_at]], [[last_used_at]] FROM {{leafen_mcp_connections}}
		WHERE [[id]] = {:id} AND [[owner_id]] = {:ownerId}`).
		Bind(dbx.Params{"id": connectionID, "ownerId": ownerID}).One(&stored) != nil {
		return false
	}
	return stored.RevokedAt == 0 && stored.LastUsedAt == 0 && (clientName == "" || stored.ClientName == clientName) &&
		!mcpConnectionUseNoted(app, connectionID)
}

// takeBackUnusedMCPConnection disconnects the owner's connection whose key
// was never used, for its code or request to give a key again ("" for any
// program's, else clientName's alone); false, with nothing changed, when it
// is gone, disconnected, another program's or was used.
func takeBackUnusedMCPConnection(txApp core.App, ownerID, connectionID, clientName string, now int64) (bool, error) {
	if !mcpConnectionNeverUsed(txApp, ownerID, connectionID, clientName) {
		return false, nil
	}
	_, found, err := revokeMCPConnectionsInTransaction(txApp,
		mcpConnectionRevocationFilter{ConnectionID: connectionID, OwnerID: ownerID}, now)
	return found && err == nil, err
}

// mcpRetradeRequestID is the request of a connection a code or device
// request makes again, apart from the one it made first.
func mcpRetradeRequestID(first string, now int64) string {
	return first + ":again:" + strconv.FormatInt(now, 36)
}
