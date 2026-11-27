package app

import "fmt"

// The operations an agent does another way, each with that way: one whose
// answer is in a form only Leafen's app reads, with where an agent reads the
// same in words (2026-11-16: an agent called a page's content-graph changes
// to learn what changed and got the editor's base64 Yjs updates); a file's
// upload, which an MCP connection makes without its key (2026-11-26: an
// agent attaching a file found POST /files and wrote a script reading the
// connection's key from its settings to send it). The operations list gives
// the note beside the operation.
var nativeOperationNotes = map[string]string{}

func init() {
	const graphChanges = "The editor's sync: binary Yjs updates in base64, not for reading. " +
		"Read what changed in words with the MCP tool notion-get-version-changes, or the page's versions with GET /pages/{pageId}/history " +
		"and one version's changes with GET /pages/{pageId}/history/{versionId}/changes."
	upload := fmt.Sprintf("From an MCP connection a file goes up without the connection's key: notion-create-file-upload gives an address "+
		"for one file of up to %d MiB, good for %d minutes (curl -F 'file=@<path>' '<upload_url>'), whose file_upload_id is the file's fileId — "+
		"in a files property by the property's name (notion-update-page, POST /database-rows/{rowId}/page/values) or in a page's file block.",
		mcpFileUploadMaximumBytes>>20, int(mcpFileUploadCapabilityTTL.Minutes()))
	for id, note := range map[string]string{
		"GET /pages/{id}/content-graph/changes": graphChanges,
		"POST " + fileTransferDirectUploadPath:  upload,
		"POST " + fileTransferPreparePath:       upload,
	} {
		nativeOperationNotes[nativeOperationShape(id)] = note
	}
}
