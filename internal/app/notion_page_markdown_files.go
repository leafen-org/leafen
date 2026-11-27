package app

import (
	"encoding/json"
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Files in a page's Notion Markdown, as Notion's page Markdown writes them:
// an image is ![caption](address) and a file, PDF, video or audio file
// <file source="address">caption</file>, <pdf …>, <video …> and <audio …>
// (src= as Leafen wrote it before reads too). As in Notion, a line neither
// writes nor takes the block's color.
//
// Notion's page Markdown keeps only a web address written from its scheme on,
// "http://" or "https://", as given, spaces and all (notionMarkdownWebAddress);
// a line naming any other — "a b.com", "example.com/x", "HTTPS://…", " https://…",
// mailto:, javascript:, ftp:, data: — or none makes its block empty, its
// caption kept, whatever the kind and in "v1", "v2" and the MCP alike (seen
// 2026-11-11). An empty block reads back as Notion's: <file src="">caption</file>
// and ![caption]() in "v1", <file /> and <image>, its caption on its own
// line, in "v2" and the MCP's.
//
// A read names a stored file by a download address of its reader's — short
// lived, as Notion's signed addresses are — with "#block=<ID>&file=<ID>&name=<name>"
// after it, which the server never sees: the block is the one the file tools
// take, and a line read back finds its file by it, so a line kept as it was
// keeps its block whenever it was read. Written
// Markdown places a stored file only where the writer had it already: one on
// the page, named by such an address (or leafen-file:<ID>), or an upload
// their connection finished, file-upload://<ID>; as for a copied Notion-hosted
// file, an address alone never brings another page's file in. Any other web
// address is an external file.

var (
	notionMarkdownFileTagTypes = map[string]string{
		"file": graphBlockTypeFile, "pdf": graphBlockTypePDF, "video": graphBlockTypeVideo, "audio": graphBlockTypeAudio,
	}
	notionMarkdownFileTagNames = map[string]string{
		graphBlockTypeFile: "file", graphBlockTypePDF: "pdf", graphBlockTypeVideo: "video", graphBlockTypeAudio: "audio",
	}
	notionMarkdownFileTagPattern = regexp.MustCompile(
		`^<(file|pdf|video|audio)\s+src="([^"]*)"(?:\s+color="([^"]*)")?\s*(?:/>|>(.*?)</(?:file|pdf|video|audio)>|>)$`)
	// ![caption](address): the spaces before its address, the address, bare or
	// in angle brackets, a title after it, which goes, the spaces before its
	// ), and a color.
	notionMarkdownImagePattern = regexp.MustCompile(
		`^!\[(.*)\]\(([ \t]*)(<[^<>\n]*>|\S*?)(?:[ \t]+"[^"\n]*")?([ \t]*)\)(?:\s*\{color="([^"]*)"\})?$`)
	// The links CommonMark doesn't take, which Notion's "v2" reads as no image.
	notionMarkdownUnsafeLink  = regexp.MustCompile(`^(?:vbscript|javascript|file|data):`)
	notionMarkdownPictureData = regexp.MustCompile(`^data:image/(?:gif|png|jpeg|webp);`)
)

// notionMarkdownWebAddress tells whether Notion's page Markdown keeps an
// address a file's, a picture's, a bookmark's or another medium's line gives:
// one written from "http://" or "https://" on, as given (seen 2026-11-11).
func notionMarkdownWebAddress(address string) bool {
	return strings.HasPrefix(address, "https://") || strings.HasPrefix(address, "http://")
}

// notionMarkdownImageTaken tells whether Notion's page Markdown reads
// ![caption](destination) as a picture, the address it gives (seen
// 2026-11-11): "v1" reads one written against both its parentheses, a title
// between them too, in angle brackets without spaces too, and any other as
// "!" and a link (version1Link: "![a]( https://…)", "![a](https://… )" and
// "![a](a b.com)" are "!" and "a" linked to the address as written); "v2" and
// the MCP's one after spaces and one in angle brackets with spaces, but, as
// CommonMark's links, no javascript:, vbscript:, file: or data: one save a
// picture's, data:image/png;….
func notionMarkdownImageTaken(destination string, spaced, version1 bool) (string, bool) {
	if len(destination) > 1 && strings.HasPrefix(destination, "<") && strings.HasSuffix(destination, ">") {
		destination = destination[1 : len(destination)-1]
		if version1 && strings.ContainsAny(destination, " \t") {
			return "", false
		}
	}
	if version1 {
		return destination, !spaced
	}
	lower := strings.ToLower(strings.TrimSpace(destination))
	return destination, !notionMarkdownUnsafeLink.MatchString(lower) || notionMarkdownPictureData.MatchString(lower)
}

const notionMarkdownUploadReference = "file-upload://"

// notionMarkdownFileRequest is a stored file a Markdown line names, until
// the writer's change places it (notionMarkdownFiles.place).
type notionMarkdownFileRequest struct {
	File    notionMarkdownFilePlacement `json:"markdownFile"`
	Caption []graphTextSpan             `json:"caption,omitempty"`
}

// notionMarkdownFilePlacement names the file by its ID, for one on the page,
// or by the upload that made it; neither is an address that names no file.
type notionMarkdownFilePlacement struct {
	FileID string `json:"fileId,omitempty"`
	Upload string `json:"upload,omitempty"`
	Name   string `json:"name,omitempty"`
}

// notionMarkdownStoredFile reads an address that names a stored file.
func notionMarkdownStoredFile(address string) (notionMarkdownFilePlacement, bool) {
	address = strings.TrimSpace(address)
	if id, named := strings.CutPrefix(address, notionMarkdownUploadReference); named {
		if !workspaceImportReferenceIDPattern.MatchString(id) {
			id = ""
		}
		return notionMarkdownFilePlacement{Upload: id}, true
	}
	if id, named := strings.CutPrefix(address, workspaceImportFileReference); named {
		if !workspaceImportReferenceIDPattern.MatchString(id) {
			id = ""
		}
		return notionMarkdownFilePlacement{FileID: id}, true
	}
	if _, fileID, name, stable := notionMarkdownFileBlockReference(address); stable {
		if len([]rune(name)) > maxGraphExternalFileNameRunes {
			name = ""
		}
		return notionMarkdownFilePlacement{FileID: fileID, Name: name}, true
	}
	parsed, err := url.Parse(address)
	if err != nil || !strings.Contains(parsed.Path, apiRoot+strings.TrimSuffix(fileTransferDownloadCapabilityPath, "{token}")) {
		return notionMarkdownFilePlacement{}, false
	}
	// A download address is never an external file.
	placement := notionMarkdownFilePlacement{}
	if fragment, err := url.ParseQuery(parsed.Fragment); err == nil &&
		workspaceImportReferenceIDPattern.MatchString(fragment.Get("file")) {
		placement.FileID = fragment.Get("file")
		placement.Name = strings.TrimSpace(fragment.Get("name"))
		if len([]rune(placement.Name)) > maxGraphExternalFileNameRunes {
			placement.Name = ""
		}
	}
	return placement, true
}

// notionMarkdownFileLine reads a line holding only a file, PDF, video or
// audio file, or an image, into its block; version1 reads it as the public
// API's "v1" does, and its caption the addresses of sitePages' pages standing
// bare in it as their mentions.
func notionMarkdownFileLine(line string, version1 bool, sitePages *notionMarkdownSitePages) (string, json.RawMessage, bool) {
	trimmed := strings.TrimSpace(line)
	var kind, address, caption, imageLink string
	if match := notionMarkdownLinkedImagePattern.FindStringSubmatch(trimmed); match != nil {
		kind, address, imageLink, caption = graphBlockTypeImage, html.UnescapeString(match[1]), html.UnescapeString(match[2]), match[3]
	} else if match := notionMarkdownFileTagPattern.FindStringSubmatch(trimmed); match != nil {
		kind, address, caption = notionMarkdownFileTagTypes[match[1]], html.UnescapeString(match[2]), match[4]
	} else if match := notionMarkdownImagePattern.FindStringSubmatch(trimmed); match != nil {
		taken := false
		if address, taken = notionMarkdownImageTaken(match[3], match[2] != "" || version1 && match[4] != "", version1); !taken {
			return "", nil, false
		}
		kind, caption = graphBlockTypeImage, match[1]
	} else {
		return "", nil, false
	}
	spans := notionMarkdownLinkCaption(caption, version1, sitePages)
	// Notion's keeps no link of a picture's; Leafen's one it opens.
	if !validGraphExternalFileURL(imageLink) {
		imageLink = ""
	}
	var content any
	if placement, stored := notionMarkdownStoredFile(address); stored {
		content = notionMarkdownFileRequest{File: placement, Caption: spans}
	} else if !notionMarkdownWebAddress(address) {
		// The block waits for its file or address, keeping its caption.
		content = notionMarkdownExternalMedia{Caption: spans}
	} else {
		switch kind {
		case graphBlockTypeFile:
			content = graphExternalFileBlockContent{Source: graphExternalFileSource, URL: address, Caption: spans}
		case graphBlockTypePDF:
			content = graphExternalPDFBlockContent{URL: address, Caption: spans}
		default:
			content = notionMarkdownExternalMedia{URL: address, Caption: spans, ImageLink: imageLink}
		}
	}
	raw, err := json.Marshal(content)
	if err != nil {
		return "", nil, false
	}
	return kind, raw, true
}

// notionMarkdownExternalMedia is an image, video or audio file from the web,
// or, with no address, a block of any of the five kinds waiting for its file.
type notionMarkdownExternalMedia struct {
	URL     string          `json:"url,omitempty"`
	Caption []graphTextSpan `json:"caption,omitempty"`
	// ImageLink is where an image opens when pressed.
	ImageLink string `json:"imageLink,omitempty"`
}

// notionMarkdownLinkedImagePattern reads an image's tag on its line
// (notionMarkdownContainerForms), with the link it opens, which Markdown's
// own form has no place for.
var notionMarkdownLinkedImagePattern = regexp.MustCompile(`^<image source="([^"]*)"(?: link="([^"]*)")?>(.*)</image>$`)

// notionMarkdownFileBlockLine writes a file, PDF, video, audio or image
// block as its line, reporting false for another block or one it can't.
func notionMarkdownFileBlockLine(block workspaceExportMarkdownBlock, blockID string, mentions notionMarkdownMentions) (string, bool) {
	tag, tagged := notionMarkdownFileTagNames[block.Type]
	if !tagged && block.Type != graphBlockTypeImage {
		return "", false
	}
	encoded, err := json.Marshal(block.Content)
	if err != nil {
		return "", false
	}
	content := struct {
		FileID  string          `json:"fileId"`
		Name    string          `json:"name"`
		URL     string          `json:"url"`
		Source  string          `json:"source"`
		Caption []graphTextSpan `json:"caption"`
		Link    string          `json:"imageLink"`
	}{}
	if json.Unmarshal(encoded, &content) != nil {
		return "", false
	}
	address := ""
	switch {
	case content.FileID != "" && mentions.FileAddress != nil:
		address = mentions.FileAddress(blockID, content.FileID, content.Name)
	case content.FileID != "":
		address = workspaceImportFileReference + content.FileID
	case validGraphStoredFileAddress(content.URL):
		// Any address the block keeps is said as kept, as Notion's reads it.
		address = content.URL
	case content.URL != "" || content.Source != "":
		return "", false
	}
	caption, written := notionMarkdownText(content.Caption, mentions.at(block.ID))
	if !written {
		return "", false
	}
	// The public API's "v1" breaks a caption's lines with <br> (seen 2026-11-10).
	if mentions.APIForm {
		caption = strings.ReplaceAll(caption, "\n", "<br>")
	} else {
		caption = strings.ReplaceAll(caption, "\n", "<br />")
	}
	if address == "" {
		return notionMarkdownEmptyFileLine(block.Type, caption, mentions.APIForm), true
	}
	if block.Type == graphBlockTypeImage && content.Link != "" && content.FileID == "" {
		return `<image source="` + notionMarkdownAttributeEscaped.Replace(address) + `" link="` +
			notionMarkdownAttributeEscaped.Replace(content.Link) + `">` + caption + "</image>", true
	}
	if block.Type == graphBlockTypeImage {
		return notionMarkdownImageLine(address, caption, mentions), true
	}
	// The public API names its address in src and always closes the tag.
	if mentions.APIForm {
		return "<" + tag + ` src="` + strings.ReplaceAll(address, `"`, "%22") + `">` + caption + "</" + tag + ">", true
	}
	opening := "<" + tag + ` source="` + notionMarkdownFileSource(address) + `"`
	// Without a caption it closes in itself, and with one holds it on its own
	// line, as Notion's reads (seen 2026-11-04).
	if caption == "" {
		return opening + " />", true
	}
	return opening + ">\n" + caption + "\n</" + tag + ">", true
}

// notionMarkdownSourceWritten writes a file's address in its tag's source,
// its " percent-encoded, its & as &amp; and its ' as &#39;, as Notion's "v2"
// writes it (seen 2026-11-11: "…/f&amp;#46;pdf?a=1&amp;b=2",
// "…/a&#39;b&amp;c.mp4"), so that the reading keeps an address holding
// "&amp;" or "&copy" as it is.
var notionMarkdownSourceWritten = strings.NewReplacer(`"`, "%22", `&`, "&amp;", `'`, "&#39;")

// notionMarkdownVersion2ImageAddress is an image's address as the API's
// "v2" writes it, the characters a Markdown destination can't hold as they
// are encoded (seen 2026-11-11: <, >, ", |, {, }, ^, `, \; ', [, ], ~ as
// they are) — "v1" writes it as it is.
var notionMarkdownVersion2ImageAddress = strings.NewReplacer(
	"<", "%3C", ">", "%3E", `"`, "%22", "|", "%7C", "{", "%7B", "}", "%7D", "^", "%5E", "`", "%60", `\`, "%5C",
)

// notionMarkdownImageLine is a picture's line, as Notion's reads one: "v1"
// writes its address as kept (seen 2026-11-11: "a b.com", " https://…",
// "…/(a).png"); "v2" and the MCP's a web address (notionMarkdownWebAddress)
// with its spaces escaped, in angle brackets where it holds parentheses, and
// another, which ![…](…) wouldn't read back, with its tag, as "v2" does a
// caption with spaces at its edges, its caption on its own line (seen
// 2026-11-10 and 2026-11-11).
func notionMarkdownImageLine(address, caption string, mentions notionMarkdownMentions) string {
	if mentions.APIForm {
		if mentions.Version2 {
			address = notionMarkdownVersion2ImageAddress.Replace(address)
		}
		return "![" + caption + "](" + address + ")"
	}
	if !notionMarkdownWebAddress(address) || mentions.Version2 && caption != strings.Trim(caption, " \t") {
		opening := `<image source="` + notionMarkdownFileSource(address) + `"`
		if caption == "" {
			return opening + " />"
		}
		return opening + ">\n" + caption + "\n</image>"
	}
	destination := strings.ReplaceAll(address, " ", "%20")
	if mentions.Version2 {
		destination = notionMarkdownVersion2ImageAddress.Replace(destination)
	}
	if strings.ContainsAny(destination, "()") {
		destination = "<" + destination + ">"
	}
	return "![" + caption + "](" + destination + ")"
}

// notionMarkdownEmptyFileLine is the line of a file, PDF, video, audio or
// image block of no file or address yet, its caption written, as Notion's
// reads one (seen 2026-11-11): <video src="">caption</video> and
// ![caption]() in the public API's "v1", <video /> or its caption on its own
// line between its tags, <image> too, in "v2" and the MCP's.
func notionMarkdownEmptyFileLine(blockType, caption string, apiForm bool) string {
	tag := notionMarkdownFileTagNames[blockType]
	switch {
	case apiForm && blockType == graphBlockTypeImage:
		return "![" + caption + "]()"
	case apiForm:
		return "<" + tag + ` src="">` + caption + "</" + tag + ">"
	}
	if blockType == graphBlockTypeImage {
		tag = "image"
	}
	if caption == "" {
		return "<" + tag + " />"
	}
	return "<" + tag + ">\n" + caption + "\n</" + tag + ">"
}

// fileAddresses names a stored file for the reader: an MCP reader's by its
// stable address (notion_page_markdown_file_blocks.go); another's by a
// download address of theirs, under their origin, with its block's ID and
// the file's after "#"; a reader who is no one, or a server that can't seal
// one, names it leafen-file:<ID>.
func (reader notionMarkdownReader) fileAddresses(app core.App, workspaceID string) func(blockID, fileID, name string) string {
	secret, sealed := "", false
	return func(blockID, fileID, name string) string {
		if reader.PrincipalID == "" {
			return workspaceImportFileReference + fileID
		}
		if reader.StableFiles && blockID != "" {
			return notionMarkdownFileBlockAddress(workspaceID, blockID, fileID, name)
		}
		if !sealed {
			secret, _ = fileDownloadCapabilitySecret(app)
			sealed = true
		}
		token, _, err := sealFileDownloadCapabilityToken(secret, reader.PrincipalID, fileID, time.Now().UTC())
		if secret == "" || err != nil {
			return workspaceImportFileReference + fileID
		}
		fragment := url.Values{"file": {fileID}}
		if blockID != "" {
			fragment.Set("block", blockID)
		}
		if name != "" {
			fragment.Set("name", name)
		}
		return reader.Origin + apiRoot + strings.Replace(fileTransferDownloadCapabilityPath, "{token}", url.PathEscape(token), 1) +
			"#" + fragment.Encode()
	}
}

// notionMarkdownFiles places the stored files Markdown written to a page in
// a workspace names: one already on the page, or an upload Upload finds for
// the writer's connection, as the file it made, in that workspace (an MCP
// connection's taken there, mcp_file_attach.go).
type notionMarkdownFiles struct {
	WorkspaceID string
	OnPage      map[string]bool
	Upload      func(app core.App, workspaceID, uploadID string) (string, error)
}

func notionMarkdownFileUnavailable() error {
	return graphInputError(errorCodeGraphReferenceInvalid,
		"A file the Markdown names is unavailable: keep a file line read from this page, or place an upload with file-upload://<ID>.")
}

// notionMarkdownPageFiles are the stored files a page's Markdown names, its
// callouts' pictures too.
func notionMarkdownPageFiles(items []workspaceImportStagedItem) map[string]bool {
	files := map[string]bool{}
	for _, item := range items {
		request := notionMarkdownFileRequest{}
		if strings.Contains(string(item.Content), `"markdownFile"`) &&
			json.Unmarshal(item.Content, &request) == nil && request.File.FileID != "" {
			files[request.File.FileID] = true
		}
		if placement, named := notionMarkdownIconFile(item.Content); named && placement.FileID != "" {
			files[placement.FileID] = true
		}
	}
	return files
}

// notionMarkdownFileKey names the file a file, PDF, video, audio or image
// line holds, whatever its caption, so a line whose caption changed keeps
// its block.
func notionMarkdownFileKey(item workspaceImportStagedItem) (string, bool) {
	if _, tagged := notionMarkdownFileTagNames[item.Type]; !tagged && item.Type != graphBlockTypeImage {
		return "", false
	}
	content := map[string]json.RawMessage{}
	if json.Unmarshal(item.Content, &content) != nil {
		return "", false
	}
	delete(content, "caption")
	key, err := json.Marshal(content)
	return item.Type + "\x00" + string(key), err == nil
}
