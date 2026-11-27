package app

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A comment's Markdown attaches a file that notion-create-file-upload or
// notion-create-attachment made by the suggested_markdown they give, on a
// line of its own, as Notion's MCP takes one (seen 2026-11-11): the line
// leaves the comment's text, at most three such lines, the tag naming what
// the file is — <image> a picture, <pdf> a PDF, <video> and <audio> theirs,
// <file> and <embed> any file. Among words, or with spaces before it, the tag
// stays words. A file given twice is attached once, as a comment holds each
// of its files once in the app and the API.

var mcpCommentAttachmentLine = regexp.MustCompile(
	`^<(file|image|pdf|video|audio|embed)\s+source="file-upload://([^"\s]+)"\s*(?:/>|></(?:file|image|pdf|video|audio|embed)>)$`,
)

// mcpCommentAttachments are the files a comment's Markdown attaches, by their
// IDs, and its text without their lines.
func mcpCommentAttachments(
	app core.App, authority mcpAuthority, current pageAuthorization, markdown string,
) (string, []string, error) {
	type attached struct{ tag, id string }
	lines := strings.Split(markdown, "\n")
	kept := make([]string, 0, len(lines))
	found := []attached{}
	for _, line := range lines {
		match := mcpCommentAttachmentLine.FindStringSubmatch(line)
		if match == nil {
			kept = append(kept, line)
			continue
		}
		found = append(found, attached{tag: match[1], id: match[2]})
	}
	if len(found) == 0 {
		return markdown, nil, nil
	}
	if len(found) > notionCommentMaximumFiles {
		return "", nil, mcpInvalid("Comments support at most %d file attachments.", notionCommentMaximumFiles)
	}
	ids := make([]string, 0, len(found))
	seen := map[string]bool{}
	for _, file := range found {
		// Taken into the page's workspace by the one rule (mcp_file_attach.go).
		record, err := mcpAttachableFile(app, authority, file.id, current.Workspace.WorkspaceID)
		if err != nil {
			return "", nil, err
		}
		// As Notion's refuses a picture's tag on a text file (seen 2026-11-11).
		category := notionCommentAttachmentCategory(record.GetString("content_type"))
		if wanted := mcpCommentAttachmentKind(file.tag); wanted != "" && category != wanted {
			return "", nil, mcpInvalid("File upload %s is a %s file, and only %s files go here.", file.id, category, wanted)
		}
		if !seen[record.Id] {
			seen[record.Id] = true
			ids = append(ids, record.Id)
		}
	}
	return strings.Join(kept, "\n"), ids, nil
}

// mcpCommentAttachmentKind is the kind of file an attachment's tag takes, as
// a comment's file is told apart (notionCommentAttachmentCategory); "" for
// any file.
func mcpCommentAttachmentKind(tag string) string {
	switch tag {
	case "image", "pdf", "video", "audio":
		return tag
	}
	return ""
}

// mcpCommentFileAddress is a comment's file as get-comments names it for the
// connection's person: a download address of theirs, as a page's file is
// named in fetch, with the file's and the comment's IDs and its name after
// "#", which get-file-download-urls renews; "" when none can be sealed.
func mcpCommentFileAddress(app core.App, authority mcpAuthority, commentID string, file commentAttachment) string {
	secret, err := fileDownloadCapabilitySecret(app)
	if err != nil {
		return ""
	}
	token, _, err := sealFileDownloadCapabilityToken(secret, authority.PrincipalID, file.ID, time.Now().UTC())
	if err != nil {
		return ""
	}
	address, err := mcpServerURL(authority.Resource, apiRoot+strings.Replace(fileTransferDownloadCapabilityPath, "{token}", url.PathEscape(token), 1))
	if err != nil {
		return ""
	}
	fragment := url.Values{"file": {file.ID}, "comment": {commentID}}
	if file.Name != "" {
		fragment.Set("name", file.Name)
	}
	return address + "#" + fragment.Encode()
}

// mcpCommentFileReadable says a comment the connection may read holds a file.
func mcpCommentFileReadable(app core.App, authority mcpAuthority, commentID, fileID string) bool {
	comment, err := loadCommentByID(app, commentID)
	if err != nil {
		return false
	}
	ids, err := commentAttachmentIDs([]byte(comment.BodyJSON))
	if err != nil || !slices.Contains(ids, fileID) {
		return false
	}
	_, err = authorizeMCPCommentTarget(app, authority, comment.TargetID, permissionWorkspaceRead)
	return err == nil
}
