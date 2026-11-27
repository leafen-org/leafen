package app

// paced says whether a client's requests go at its connection's pace —
// Notion's three requests a second and its Retry-After, shared by everyone
// reading the connection. Notion's do; a bundle another tool made, staged in
// a workspace of its own, is read through Leafen's own router in this
// process (notionBundleTransport), where that pace only made the scan and the
// run wait between requests (2026-11-24: an Obsidian vault of 2,401 pages
// scanned at about 0.9 pages a second, some 45 minutes, while the server's
// CPU sat at 0.7%). Such a client keeps its connection's other shared state
// (one top-level read at a time, notion_import_scope_walk.go); the scan reads
// one step after another (notion_migration_scan_on.go), and what asks several
// at once asks at most notionProviderMaximumConcurrentRequests.
func (client notionProviderClient) paced() bool {
	if client.admission == nil || client.connectionID == "" {
		return false
	}
	_, inProcess := client.client.Transport.(notionBundleTransport)
	return !inProcess
}
