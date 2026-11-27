package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

const (
	mcpAttachmentInlineMaximumBytes = 200 << 10
	mcpAttachmentURLMaximumBytes    = 5 << 20
)

type notionMCPCreateFileUploadInput struct {
	Filename    string `json:"filename" jsonschema:"Filename, including an extension"`
	ContentType string `json:"content_type,omitempty" jsonschema:"Optional MIME type matching the filename"`
}

type notionMCPCreateFileUploadOutput struct {
	UploadURL     string            `json:"upload_url"`
	UploadHeaders map[string]string `json:"upload_headers"`
	FileUploadID  string            `json:"file_upload_id"`
	ExpiresAt     string            `json:"expires_at"`
	// UploadRequest is how the file goes to upload_url (mcp_file_upload_request.go).
	UploadRequest *mcpFileUploadRequest `json:"upload_request,omitempty" jsonschema:"How to send the file to upload_url: the method, the body and its form field, and a curl line to run with the local file's path"`
}

type notionMCPCreateAttachmentInput struct {
	Filename     string  `json:"filename,omitempty" jsonschema:"Filename required for inline content or a source URL"`
	ContentType  string  `json:"content_type,omitempty" jsonschema:"Optional MIME type matching the filename"`
	Content      *string `json:"content,omitempty" jsonschema:"Inline UTF-8 text content"`
	SourceURL    *string `json:"source_url,omitempty" jsonschema:"Direct public HTTPS URL"`
	SourceFileID *string `json:"source_file_id,omitempty" jsonschema:"A finished upload's file_upload_id"`
}

type notionMCPDownloadAttachmentInput struct {
	FileUploadID string `json:"file_upload_id" jsonschema:"Attachment file upload ID"`
}

// notionMCPAttachmentOutput is the attachment as Notion's answer gives it
// (2026-11-21).
type notionMCPAttachmentOutput struct {
	FileUploadID      string `json:"file_upload_id"`
	Filename          string `json:"filename"`
	ContentType       string `json:"content_type"`
	ContentLength     int64  `json:"content_length"`
	Status            string `json:"status"`
	MarkdownSource    string `json:"markdown_source"`
	SuggestedMarkdown string `json:"suggested_markdown"`
}

type notionMCPDownloadAttachmentOutput struct {
	FileUploadID string `json:"file_upload_id"`
	Filename     string `json:"filename"`
	ContentType  string `json:"content_type"`
	Content      string `json:"content"`
}

func registerMCPFileTools(server *mcp.Server, app core.App, readOnly *mcp.ToolAnnotations) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-create-file-upload", Title: "Create a Leafen file upload URL",
		Description: "Create a short-lived URL for one multipart/form-data upload in the file field, limited to 20 MiB; it needs no key or header (upload_request gives a curl line). " +
			"Then attach its file_upload_id: in a files property by the property's name, as properties {\"Files\": [\"<file_upload_id>\"]} in notion-update-page or notion-create-pages, " +
			"or in a page's body as the upload's answer's suggested_markdown. " +
			"Attached to a page or row of another workspace this connection reaches, the file moves there. " +
			"A .md file's file_upload_id is a page's body too, one too long to write in a call: new_str_file_upload_id in notion-update-page, content_file_upload_id in notion-create-pages.",
	}, func(ctx context.Context, request *mcp.CallToolRequest, input notionMCPCreateFileUploadInput) (*mcp.CallToolResult, notionMCPCreateFileUploadOutput, error) {
		return createMCPFileUploadURL(ctx, request, app, input)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-create-attachment", Title: "Create a Leafen attachment",
		Description: "Create an attachment from exactly one inline UTF-8 value, direct public HTTPS URL, or finished upload. " +
			"To place it, pass its markdown_source to create-pages or update-page, or use its suggested_markdown. An HTML file goes in <embed source=\"file-upload://...\" />, which draws it in a sandbox; other files in <file source=\"file-upload://...\" />.",
	}, func(ctx context.Context, request *mcp.CallToolRequest, input notionMCPCreateAttachmentInput) (*mcp.CallToolResult, notionMCPAttachmentOutput, error) {
		return createMCPAttachment(ctx, request, app, input)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-download-attachment", Title: "Download a Leafen text attachment",
		Description: "Download at most 200 KiB of UTF-8 text from an uploaded file this connection may read.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPDownloadAttachmentInput) (*mcp.CallToolResult, notionMCPDownloadAttachmentOutput, error) {
		return downloadMCPAttachment(ctx, app, input)
	})
}

func createMCPFileUploadURL(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	app core.App,
	input notionMCPCreateFileUploadInput,
) (*mcp.CallToolResult, notionMCPCreateFileUploadOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPCreateFileUploadOutput{}, err
	}
	workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{AllowImplicit: true}, permissionWorkspaceWrite)
	if err != nil {
		return nil, notionMCPCreateFileUploadOutput{}, err
	}
	filename, contentType, err := normalizeMCPAttachmentMetadata(input.Filename, input.ContentType)
	if err != nil {
		return nil, notionMCPCreateFileUploadOutput{}, mcpInvalid("filename has an extension and no folders, at most 900 characters, and content_type, if given, matches it")
	}
	fileID := mcpFileMutationID(authority, "upload-capability")
	payload := mcpFileUploadCapabilityPayload{
		ConnectionID: authority.ConnectionID, CredentialHash: authority.CredentialHash,
		PrincipalID: authority.PrincipalID, WorkspaceID: workspace.Workspace.Id,
		FileID: fileID, RequestID: mcpFileRequestID(authority.ConnectionID, fileID),
		Filename: filename, ContentType: contentType,
	}
	token, expiresAt, err := sealMCPFileUploadCapability(app, payload)
	if err != nil {
		return nil, notionMCPCreateFileUploadOutput{}, failedBecause(errMCPToolFailed, err)
	}
	uploadURL, err := mcpFileUploadURL(authority.Resource, token)
	if err != nil {
		return nil, notionMCPCreateFileUploadOutput{}, failedBecause(errMCPToolFailed, err)
	}
	return nil, notionMCPCreateFileUploadOutput{
		UploadURL: uploadURL, UploadHeaders: map[string]string{}, FileUploadID: fileID,
		ExpiresAt: mcpTimestamp(expiresAt, authority.clock()), UploadRequest: mcpFileUploadRequestTo(uploadURL),
	}, nil
}

func createMCPAttachment(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	app core.App,
	input notionMCPCreateAttachmentInput,
) (*mcp.CallToolResult, notionMCPAttachmentOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPAttachmentOutput{}, err
	}
	sources := 0
	for _, present := range []bool{input.Content != nil, input.SourceURL != nil, input.SourceFileID != nil} {
		if present {
			sources++
		}
	}
	if sources != 1 {
		return nil, notionMCPAttachmentOutput{}, mcpInvalid("an attachment comes from one source: content, source_url or source_file_id")
	}
	if input.SourceFileID != nil {
		if strings.TrimSpace(input.Filename) != "" || strings.TrimSpace(input.ContentType) != "" {
			return nil, notionMCPAttachmentOutput{}, mcpInvalid("an attachment from source_file_id keeps that file's name and type: leave filename and content_type out")
		}
		record, err := mcpReadableUploadOf(app, authority, *input.SourceFileID)
		if err != nil {
			return nil, notionMCPAttachmentOutput{}, err
		}
		return nil, notionMCPAttachmentRecord(record), nil
	}

	workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{AllowImplicit: true}, permissionWorkspaceWrite)
	if err != nil {
		return nil, notionMCPAttachmentOutput{}, err
	}
	filename, contentType, err := normalizeMCPAttachmentMetadata(input.Filename, input.ContentType)
	if err != nil {
		return nil, notionMCPAttachmentOutput{}, mcpInvalid("filename has an extension and no folders, at most 900 characters, and content_type, if given, matches it")
	}
	var content []byte
	if input.Content != nil {
		content = []byte(*input.Content)
		if len(content) == 0 || len(content) > mcpAttachmentInlineMaximumBytes ||
			!utf8.Valid(content) || !mcpDownloadableTextType(filename, contentType) {
			return nil, notionMCPAttachmentOutput{}, mcpInvalid("content is text of at most %d bytes in a text file type (as .html, .md, .txt, .csv, .json, .yaml, .tsv, .ics); upload other files with notion-create-file-upload", mcpAttachmentInlineMaximumBytes)
		}
	} else {
		content, contentType, err = fetchMCPAttachmentURL(ctx, *input.SourceURL, filename, contentType)
		if err != nil {
			return nil, notionMCPAttachmentOutput{}, mcpFileToolError(err)
		}
	}
	fileID := mcpFileMutationID(authority, "attachment")
	record, err := storeMCPAttachment(app, authority, workspace.Workspace.Id, fileID, filename, contentType, content)
	if err != nil {
		return nil, notionMCPAttachmentOutput{}, mcpFileToolError(err)
	}
	return nil, notionMCPAttachmentRecord(record), nil
}

func downloadMCPAttachment(
	ctx context.Context,
	app core.App,
	input notionMCPDownloadAttachmentInput,
) (*mcp.CallToolResult, notionMCPDownloadAttachmentOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPDownloadAttachmentOutput{}, err
	}
	record, err := mcpReadableUploadOf(app, authority, input.FileUploadID)
	if err != nil {
		return nil, notionMCPDownloadAttachmentOutput{}, err
	}
	filename, contentType := record.GetString("original_name"), record.GetString("content_type")
	if int64(record.GetFloat("size")) > mcpAttachmentInlineMaximumBytes ||
		!mcpDownloadableTextType(filename, contentType) {
		return nil, notionMCPDownloadAttachmentOutput{}, mcpInvalid("only text files of at most %d bytes come back inline; fetch others by their address", mcpAttachmentInlineMaximumBytes)
	}
	read, err := authorizeProductFileRead(app, authority.PrincipalID, record.Id)
	if err != nil {
		return nil, notionMCPDownloadAttachmentOutput{}, mcpFileToolError(err)
	}
	defer read.Filesystem.Close()
	// The file is what it works on, where it is (mcp_answer_workspaces.go).
	authority.reaches(read.Record.GetString("workspace"))
	reader, err := read.Filesystem.GetReader(record.BaseFilesPath() + "/" + record.GetString("blob"))
	if err != nil {
		return nil, notionMCPDownloadAttachmentOutput{}, failedBecause(errMCPToolFailed, err)
	}
	defer reader.Close()
	content, err := io.ReadAll(io.LimitReader(reader, mcpAttachmentInlineMaximumBytes+1))
	if err != nil || len(content) > mcpAttachmentInlineMaximumBytes || !utf8.Valid(content) {
		return nil, notionMCPDownloadAttachmentOutput{}, failedBecause(errMCPToolFailed, err)
	}
	return nil, notionMCPDownloadAttachmentOutput{
		FileUploadID: record.Id, Filename: filename, ContentType: contentType, Content: string(content),
	}, nil
}

func storeMCPAttachment(
	app core.App,
	authority mcpAuthority,
	workspaceID, fileID, filename, contentType string,
	content []byte,
) (*core.Record, error) {
	file, err := filesystem.NewFileFromBytes(content, filename)
	if err != nil {
		return nil, err
	}
	digest, err := fileDigest(file.Reader)
	if err != nil {
		return nil, err
	}
	input := fileUploadPrepareInput{
		RequestID: mcpFileRequestID(authority.ConnectionID, fileID), WorkspaceID: workspaceID,
		Name: filename, Size: int64(len(content)), ContentType: contentType,
	}
	if err := normalizeProductFileUploadInput(&input); err != nil {
		return nil, err
	}
	release := acquireProductFileOperationLock(
		productFileOperationScopedNamespace(productFileOperationUploadRequest, authority.PrincipalID),
		input.RequestID,
	)
	defer release()
	upload, _, err := admitDirectProductFileUploadWithID(app, authority.PrincipalID, input, digest, fileID)
	if err != nil {
		return nil, err
	}
	uploadRelease := acquireProductFileOperationLock(productFileOperationResumable, upload.UploadID)
	defer uploadRelease()
	if upload.Status == fileUploadStatusCompleted {
		return completedDirectProductFileUpload(app, upload)
	}
	return publishDirectProductFileUpload(app, upload, file, digest)
}

// mcpMarkdownReader is a connection reading page Markdown, written as the
// public API's "v2" writes it, as Notion's MCP fetch now writes a page
// (seen 2026-11-11: the same text, byte for byte, as its API's "v2"): a
// stored file is named by its stable address, as Notion's MCP names one
// (notion_page_markdown_file_blocks.go), whose download addresses the file
// tools give under the server it connected to; a person mention names its
// person by the public ID notion-get-users gives, with their email and name,
// as Notion's MCP names one (notion_markdown_person_mentions.go). A token narrowed to chosen pages
// can't open a page outside them, so a mention, link, sub-page, synced block
// or database of one names nothing it may not read (mcp_reader_mentions.go).
func mcpMarkdownReader(authority mcpAuthority) notionMarkdownReader {
	reader := principalMarkdownReader(authority.PrincipalID)
	reader.Version2, reader.PeopleEmails, reader.StableFiles, reader.NamesUntitled = true, true, true, true
	reader.Origin, _ = mcpServerURL(authority.Resource, "")
	if authority.PagesNarrowed {
		readable := reader.Readable
		reader.Readable = func(app core.App, pageID string) (bool, error) {
			open, err := readable(app, pageID)
			return open && err == nil && mcpAuthorityReachesPageID(app, authority, pageID), err
		}
	}
	return reader
}

// mcpMarkdownUploads finds the file Markdown places with file-upload://<ID>
// in the page's workspace, by the one rule for files named by ID
// (mcp_file_attach.go); the write brought one from another workspace before
// it started (mcpBringMarkdownUploads).
func mcpMarkdownUploads(authority mcpAuthority) func(core.App, string, string) (string, error) {
	return func(app core.App, workspaceID, uploadID string) (string, error) {
		record, err := mcpAttachableFile(app, authority, uploadID, workspaceID)
		if errors.As(err, &mcpUploadsToBring{}) {
			return "", mcpInvalid("file_upload %s is in another workspace than the page, and a write of a synced block's original can't move it there: place it on the original's page itself", uploadID)
		}
		if err != nil {
			return "", err
		}
		return record.Id, nil
	}
}

func notionMCPAttachmentRecord(record *core.Record) notionMCPAttachmentOutput {
	source := notionMarkdownUploadReference + record.Id
	contentType := record.GetString("content_type")
	return notionMCPAttachmentOutput{
		FileUploadID: record.Id,
		Filename:     record.GetString("original_name"), ContentType: contentType,
		ContentLength: int64(record.GetFloat("size")), Status: "uploaded",
		MarkdownSource: source, SuggestedMarkdown: "<" + mcpAttachmentTag(contentType) + ` source="` + source + `" />`,
	}
}

// mcpAttachmentTag is the tag that places a file by what it holds, as
// Notion's suggested Markdown names it (seen 2026-11-04): an HTML file in an
// embed, which draws it.
func mcpAttachmentTag(contentType string) string {
	mediaType, _, _ := strings.Cut(strings.ToLower(contentType), ";")
	switch mediaType = strings.TrimSpace(mediaType); {
	case mediaType == sandboxedHTMLContentType:
		return "embed"
	case mediaType == "application/pdf":
		return "pdf"
	case strings.HasPrefix(mediaType, "image/"):
		return "image"
	case strings.HasPrefix(mediaType, "video/"):
		return "video"
	case strings.HasPrefix(mediaType, "audio/"):
		return "audio"
	}
	return "file"
}

var mcpAttachmentTextTypes = map[string]string{
	".csv": "text/csv", ".htm": "text/html", ".html": "text/html", ".ics": "text/calendar", ".json": "application/json",
	".markdown": "text/markdown", ".md": "text/markdown", ".tsv": "text/tab-separated-values", ".txt": "text/plain",
	".yaml": "application/yaml", ".yml": "application/yaml",
}

func normalizeMCPAttachmentMetadata(filename, contentType string) (string, string, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" || len([]rune(filename)) > 900 || filepath.Base(filename) != filename ||
		filepath.Ext(filename) == "" {
		return "", "", mcpInvalid("filename has an extension and no folders, at most 900 characters")
	}
	extension := strings.ToLower(filepath.Ext(filename))
	// The system's own table may not know these (2026-11-04: .md on a Mac).
	inferred, known := mcpAttachmentTextTypes[extension]
	if !known {
		inferred = mime.TypeByExtension(extension)
	}
	if base, _, err := mime.ParseMediaType(inferred); err == nil {
		inferred = base
	}
	explicit := strings.TrimSpace(contentType)
	if explicit != "" {
		normalized, err := normalizeStoredFileContentType(explicit)
		if err != nil {
			return "", "", err
		}
		if inferred != "" {
			inferredNormalized, err := normalizeStoredFileContentType(inferred)
			if err == nil && normalized != inferredNormalized {
				return "", "", mcpInvalid("content_type doesn't match the filename's extension: leave it out, or make them agree")
			}
		}
		contentType = normalized
	} else if inferred != "" {
		contentType = inferred
	} else {
		contentType = defaultFileContentType
	}
	contentType, err := admitStoredFileType(filename, contentType)
	if err != nil {
		return "", "", err
	}
	return filename, contentType, nil
}

func mcpDownloadableTextType(filename, contentType string) bool {
	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	allowed := map[string]struct{}{
		"csv": {}, "htm": {}, "html": {}, "ics": {}, "json": {}, "md": {}, "markdown": {}, "txt": {},
		"tsv": {}, "yaml": {}, "yml": {},
	}
	if _, ok := allowed[extension]; !ok {
		return false
	}
	normalized, err := normalizeStoredFileContentType(contentType)
	return err == nil && (strings.HasPrefix(normalized, "text/") ||
		normalized == "application/json" || normalized == "application/yaml" ||
		normalized == "application/x-yaml")
}

func mcpFileMutationID(authority mcpAuthority, purpose string) string {
	seed := mcpCallSeed()
	digest := sha256.Sum256([]byte("leafen:mcp-file-id:v1\x00" + authority.ConnectionID + "\x00" + purpose + "\x00" + seed))
	return hex.EncodeToString(digest[:])[:15]
}

func mcpFileRequestID(connectionID, fileID string) string {
	digest := sha256.Sum256([]byte("leafen:mcp-file-request:v1\x00" + connectionID + "\x00" + fileID))
	return "mcp-file-request-" + hex.EncodeToString(digest[:])
}

func mcpFileToolError(err error) error {
	if err == nil {
		return nil
	}
	if isPermissionDenied(err) {
		return mcpDenied("this account can't add files there")
	}
	if errors.Is(err, errMCPAttachmentInvalid) || errors.Is(err, errFileUploadInvalid) ||
		errors.Is(err, errFileUploadRequestIDReused) || storedFileTypeAdmissionCode(err) != "" {
		return mcpInvalid("the file wasn't taken: its type is one the workspace refuses, it is too large, or this request's ID was used for another")
	}
	return mcpReasoned(err)
}
