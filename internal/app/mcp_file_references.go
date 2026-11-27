package app

import "strings"

// An uploaded file is named the same way by every tool that takes one: by
// its ID alone, as create-file-upload's file_upload_id and a files value's
// file_upload object give it, as file-upload://<ID>, its markdown_source,
// or as leafen-file:<ID>, an import's (2026-11-16: a files value refused
// file-upload://, get-file-download-urls a bare ID and file-upload://).

// mcpUploadedFileID is the file a reference names by its ID in any of those
// forms, or "" when it doesn't name one that way.
func mcpUploadedFileID(raw string) string {
	raw = strings.TrimSpace(raw)
	for _, prefix := range []string{notionMarkdownUploadReference, workspaceImportFileReference} {
		if id, found := strings.CutPrefix(raw, prefix); found {
			raw = strings.TrimSpace(id)
			break
		}
	}
	if raw == "" || strings.ContainsAny(raw, "/:#?<>\"' ") {
		return ""
	}
	return raw
}
