package app

// mcpFetchOutput is a page read's answer (leafen_fetch, and the page
// notion-fetch writes as text): the page, its body as Markdown, and a
// database page's values.
type mcpFetchOutput struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Title       string `json:"title"`
	Content     string `json:"content"`
	URL         string `json:"url"`
	Revision    int    `json:"revision"`
	Cursor      int    `json:"cursor" jsonschema:"The page's version, a name and not an order: any change to the page gives it another; leafen_update_page takes it as base_cursor"`
	// Truncated says the content stops before the page's end: it holds more
	// blocks than one read writes, and isn't written back whole.
	Truncated bool `json:"truncated,omitempty" jsonschema:"The content stops before the page's end: the page holds more blocks than one read gives"`
	// That the page is material, not instructions (mcp_session_instructions.go).
	Notice  string `json:"notice,omitempty"`
	InTrash bool   `json:"in_trash,omitzero" jsonschema:"The page is in the trash; its content stays until it is deleted"`
	// Properties are a database page's values, as notion-fetch gives them
	// (mcpFetchedPageProperties); leafen_fetch alone fills them in.
	Properties map[string]any `json:"properties,omitempty" jsonschema:"A database page's values by property, as notion-fetch gives them: formulas and rollups as their values, a date's and a place's parts in columns of their own, empty values left out"`
	// FileVersions are its stored files' versions (mcp_fetch_file_versions.go).
	FileVersions []mcpFetchedFileVersion `json:"file_versions,omitempty" jsonschema:"Each stored file the read holds — a file, image, PDF, video or audio block its content names, by block_id, or a file in a database page's files property, by page_id, property and file_id — with base_version, which leafen_replace_file takes with the same names to replace it while it is still that version"`
}
