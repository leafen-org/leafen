package app

import (
	"strings"

	"github.com/google/uuid"
)

// A view or a data source the server makes — for the Public API, Markdown,
// a data source's move or a Sprints database — takes the shape of ID the
// screen gives its own (the web's newId) and an agent's (mcpEntityID): a
// UUID. The MCP names a view and a data source by its ID (view://,
// collection://), as Notion's names every one by a UUID (seen 2026-11-09:
// a database Markdown made has view://e666b5c1-… and collection://1887c732-…),
// so a database looks the same to an agent however it was made.

// newDatabaseEntityID is a new view's or data source's ID.
func newDatabaseEntityID() string {
	return uuid.NewString()
}

// derivedDatabaseEntityID is that shape for an ID derived from its parts, the
// same for the same parts, for a change that may be applied again.
func derivedDatabaseEntityID(parts ...string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("leafen:database-entity:v1\x00"+strings.Join(parts, "\x00"))).String()
}
