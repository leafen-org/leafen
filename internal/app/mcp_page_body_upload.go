package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
)

// A page body too long to write into one call comes from a file
// (2026-11-23: an agent replacing a 26,000-character report found no
// official way and sent it to Leafen's address by a way of its own). The
// agent uploads it as notion-create-file-upload uploads any file — the tool
// gives an address, the bytes go to it over HTTP, never through the
// conversation — and names the upload where the body goes: new_str_file_upload_id
// for notion-update-page's and leafen_update_page's replace_content,
// content_file_upload_id for a page of notion-create-pages. The file's text
// is then the body as if it were written in the call: the same reading, the
// same checks and limits, the same check of the version the agent saw
// (base_cursor) and the same page history to undo it by.

// mcpPageBodyUploads name, for each body argument, the argument that names
// an uploaded file in its place.
var mcpPageBodyUploads = map[string]string{"new_str": "new_str_file_upload_id", "content": "content_file_upload_id"}

// mcpPageBodyUploadTypes are the files a body is read from: Markdown or
// plain text.
var mcpPageBodyUploadTypes = map[string]bool{".md": true, ".markdown": true, ".txt": true}

// notionMCPCreatePageDefinition is one page notion-create-pages makes.
type notionMCPCreatePageDefinition struct {
	Properties map[string]any `json:"properties,omitempty" jsonschema:"Page properties: under a page only its title, in inline Markdown (left out, a first line # heading of content gives it); under a database its properties as SQLite values, a title or text in inline Markdown, a date's time without an offset in the person's time zone"`
	Content    string         `json:"content,omitempty" jsonschema:"Optional page content in Leafen page Markdown; notion://docs/notion-flavored-markdown-spec gives the syntax. A long one can come from a file: content_file_upload_id"`
	// ContentFileUploadID names an uploaded file whose text is the content.
	ContentFileUploadID string  `json:"content_file_upload_id,omitempty" jsonschema:"In place of content, for one too long to write in the call (tens of thousands of characters): the file_upload_id of a .md file this connection uploaded — notion-create-file-upload gives upload_url and file_upload_id, and the file goes to upload_url (curl -F file=@body.md <upload_url>). Its text is the content, read and checked as content is"`
	Icon                *string `json:"icon,omitempty" jsonschema:"An emoji, a custom emoji by name (:name:), a Notion icon identifier as fetch gives it (icons/pizza_blue), or an external image URL; none sets no icon"`
	Cover               *string `json:"cover,omitempty" jsonschema:"An external image URL for the page cover; none sets no cover"`
	IsSkill             *bool   `json:"is_skill,omitempty" jsonschema:"Leafen does not make pages Skills: true is refused as unsupported_feature, false changes nothing"`
	TemplateID          string  `json:"template_id,omitempty" jsonschema:"A template of the parent data source, from fetch's templates, that gives the page its values and body; leave content out with it"`
	// PreserveInternalLinks keeps Markdown links to the workspace's pages as
	// links; otherwise they become the pages' mentions
	// (mcp_page_internal_links.go).
	PreserveInternalLinks *bool `json:"preserve_internal_links,omitempty" jsonschema:"Keep Markdown links to this workspace's pages as links instead of turning them into page mentions; a page's address written alone becomes its mention either way"`
	// rowsStay refuses a database's item its content would move in
	// (mcpCreatePageMovesOf): the Leafen-native endpoint's without leave_database.
	rowsStay bool
}

// mcpPageBodyUploadGiven says whether a call named an upload in place of a
// body argument.
func mcpPageBodyUploadGiven(given map[string]json.RawMessage, argument string) bool {
	upload, named := mcpPageBodyUploads[argument]
	text := ""
	return named && json.Unmarshal(given[upload], &text) == nil && strings.TrimSpace(text) != ""
}

// mcpArgumentGiven says whether a call gave an argument, "" among values.
func mcpArgumentGiven(arguments json.RawMessage, name string) bool {
	given := map[string]json.RawMessage{}
	_ = json.Unmarshal(arguments, &given)
	raw := bytes.TrimSpace(given[name])
	return len(raw) > 0 && string(raw) != "null"
}

// mcpPageBody is the body a call gave: the text written in it, or the
// uploaded file's that uploadID names; a call gives one of them.
func mcpPageBody(ctx context.Context, app core.App, argument, written string, writtenGiven bool, uploadID string) (string, error) {
	uploadID = strings.TrimSpace(uploadID)
	if uploadID == "" {
		return written, nil
	}
	if writtenGiven {
		return "", mcpInvalid("give the body as %s or as %s, not both", argument, mcpPageBodyUploads[argument])
	}
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return "", err
	}
	record, err := mcpReadableUploadOf(app, authority, uploadID)
	if err != nil {
		return "", err
	}
	if !mcpPageBodyUploadTypes[strings.ToLower(filepath.Ext(record.GetString("original_name")))] {
		return "", mcpInvalid("%s names a .md or .txt file, its text the body's Markdown", mcpPageBodyUploads[argument])
	}
	read, err := authorizeProductFileRead(app, authority.PrincipalID, record.Id)
	if err != nil {
		return "", mcpFileToolError(err)
	}
	defer read.Filesystem.Close()
	reader, err := read.Filesystem.GetReader(record.BaseFilesPath() + "/" + record.GetString("blob"))
	if err != nil {
		return "", failedBecause(errMCPToolFailed, err)
	}
	defer reader.Close()
	content, err := io.ReadAll(io.LimitReader(reader, mcpFileUploadMaximumBytes+1))
	if err != nil {
		return "", failedBecause(errMCPToolFailed, err)
	}
	if int64(len(content)) > mcpFileUploadMaximumBytes || !utf8.Valid(content) {
		return "", mcpInvalid("%s names a file of UTF-8 text", mcpPageBodyUploads[argument])
	}
	// The file's text, as `leafen mcp secret --from-file` reads one: a
	// byte order mark and its last line break left off.
	content = bytes.TrimPrefix(content, []byte("\xef\xbb\xbf"))
	content = bytes.TrimSuffix(content, []byte("\n"))
	content = bytes.TrimSuffix(content, []byte("\r"))
	return string(content), nil
}

// mcpCreatePagesBodies reads each page's content from the file it names in
// its place.
func mcpCreatePagesBodies(ctx context.Context, app core.App, pages []notionMCPCreatePageDefinition) error {
	for index := range pages {
		page := &pages[index]
		content, err := mcpPageBody(ctx, app, "content", page.Content, page.Content != "", page.ContentFileUploadID)
		if err != nil {
			return err
		}
		page.Content, page.ContentFileUploadID = content, ""
	}
	return nil
}
