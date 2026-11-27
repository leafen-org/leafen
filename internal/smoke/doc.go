// Package smoke holds Leafen's public smoke tests: one short walk along the
// core path a person and their agent take, from a server started on an empty
// folder to its backup restored into a new one with the data still there.
// The walk runs the built server program as its own process and reaches it
// only as a person's commands and an agent's API and MCP calls do, so it
// checks the program as it ships, not its insides.
//
//	go test ./internal/smoke
//
// builds the program from this checkout first (the web build in web/dist
// must be there: npm --prefix web run build); LEAFEN_SMOKE_BINARY names a
// program built already (a relative path from the repository's root, such
// as dist/leafen), and LEAFEN_SMOKE_PORTS ("<http>,<door>") the two
// loopback ports to serve on instead of free ones. go test -short skips it.
//
// These tests check that the core path works; how closely Leafen follows
// Notion's behaviour is checked by the maintainers' own suite.
package smoke
