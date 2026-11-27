package app

import (
	"context"
	"errors"
	"html"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A synced block reference that holds blocks in a page's Markdown writes
// them into its original, wherever that stands, as Notion's API and MCP do
// (seen 2026-11-11, "v1" and "v2"): update_content, replace_content and
// replace_content_range change the original's blocks in place, the blocks
// that stay keeping their IDs, and the page then reads the reference with
// them. A reference holding only blank lines, or none, leaves its original
// as it is. A new page's Markdown and inserted content may hold only empty
// references; one holding blocks is refused, as Notion's is, in Leafen's
// words (notionMarkdownSyncedRefusal). The
// original's page is written as a write of its own Markdown, admitted as a
// write of that page is, as a block update there is, once the page the
// reference stands on has planned its own write; an original on that same
// page takes the reference's blocks in place of its own.

var (
	notionMarkdownSyncedReferenceOpen = regexp.MustCompile(`^<(synced-block-reference|synced_block_reference)((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*>(.*)$`)
	notionMarkdownSyncedOriginalOpen  = regexp.MustCompile(`^<(synced-block|synced_block)((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*>[ \t]*$`)
)

// errNotionSyncedOriginalNotFound is an original whose page the writer may
// read but not write, answered as a block update there is.
var errNotionSyncedOriginalNotFound = errors.New("synced block original not writable")

// notionMarkdownSyncedContent is a reference that holds blocks: its address,
// what its tag stands in, and its lines between its tags.
type notionMarkdownSyncedContent struct {
	address, lead string
	lines         []string
}

// notionMarkdownSyncedWrite is an original on another page and the lines
// its reference gives it.
type notionMarkdownSyncedWrite struct {
	blockID, pageID string
	content         notionMarkdownSyncedContent
}

// notionMarkdownSyncedContents finds the references in Markdown that hold
// blocks, past code: "v1" names them <synced_block_reference>, its other
// tags being text there, and "v2" either way.
func notionMarkdownSyncedContents(markdown string, version2 bool) []notionMarkdownSyncedContent {
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	if !version2 {
		lines = notionMarkdownVersion1BlockQuotes(lines)
	}
	found := []notionMarkdownSyncedContent{}
	fence := ""
	for index := 0; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			fence = notionMarkdownFenceOf(trimmed)
			continue
		}
		match := notionMarkdownSyncedReferenceOpen.FindStringSubmatch(trimmed)
		if match == nil || !version2 && match[1] != "synced_block_reference" {
			continue
		}
		line := lines[index]
		lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		closing := "</" + match[1] + ">"
		content := notionMarkdownSyncedContent{
			address: html.UnescapeString(notionMarkdownAttributes(match[2])["url"]), lead: lead,
		}
		if before, _, closed := strings.Cut(match[3], closing); closed {
			content.lines = []string{lead + before}
		} else {
			end := notionMarkdownSyncedClosing(lines, index, match[1], notionMarkdownSyncedReferenceOpen)
			if end < 0 {
				continue
			}
			if strings.TrimSpace(match[3]) != "" {
				content.lines = append(content.lines, lead+match[3])
			}
			content.lines = append(content.lines, lines[index+1:end]...)
			if before := strings.TrimSuffix(strings.TrimSpace(lines[end]), closing); before != "" {
				content.lines = append(content.lines, lead+before)
			}
			index = end
		}
		if strings.TrimSpace(strings.Join(content.lines, "")) != "" {
			found = append(found, content)
		}
	}
	return found
}

// notionMarkdownSyncedClosing is the line closing the tag opened at start,
// past the same tags opened inside it; -1 when it doesn't close.
func notionMarkdownSyncedClosing(lines []string, start int, name string, opening *regexp.Regexp) int {
	depth, closing := 0, "</"+name+">"
	for index := start + 1; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])
		if match := opening.FindStringSubmatch(trimmed); match != nil && match[1] == name {
			// One closed on its own line holds nothing past it.
			if len(match) < 4 || !strings.Contains(match[3], closing) {
				depth++
			}
			continue
		}
		if strings.HasSuffix(trimmed, closing) {
			if depth == 0 {
				return index
			}
			depth--
		}
	}
	return -1
}

// notionMarkdownSyncedRefusal refuses a reference holding blocks in a new
// page's Markdown or in inserted content, as Notion's does, in Leafen's
// words; "" for Markdown holding none.
func notionMarkdownSyncedRefusal(markdown string, version2, inserting bool) string {
	if len(notionMarkdownSyncedContents(markdown, version2)) == 0 {
		return ""
	}
	tag, empty := "<synced_block_reference>", "`<synced_block_reference url=\"...\">` with `</synced_block_reference>` on the next line"
	if version2 {
		tag, empty = "<synced-block-reference>", "`<synced-block-reference url=\"...\"></synced-block-reference>`"
	}
	said := " with content replaces the original synced block's content everywhere it appears. "
	if inserting {
		return "Inserting content can't change an existing synced block. A `" + tag + "`" + said +
			"To add a reference, leave it empty: " + empty + ". To change the original, use `update_content` or " +
			"`replace_content`, and set `allow_deleting_content: true` if you intend for the edit to delete nested " +
			"child pages or databases."
	}
	return "A new page can't change an existing synced block. A `" + tag + "`" + said +
		"The new page can include an empty reference: " + empty + ". To change the original, edit the page that " +
		"holds it with `update_content` or `replace_content`."
}

// text is a reference's lines as they read, whatever they stand in.
func (content notionMarkdownSyncedContent) text() string {
	lines := make([]string, 0, len(content.lines))
	for _, line := range content.lines {
		lines = append(lines, strings.TrimRight(strings.TrimPrefix(line, content.lead), " \t"))
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}

// notionMarkdownSyncedOriginals names the original of each reference in a
// write's Markdown whose blocks aren't those the page read before shows it
// with: one on the page written takes them in the Markdown, the others are
// left to write. A reference as it was read changes nothing, so a write
// that keeps one needs no more than reading its original.
func notionMarkdownSyncedOriginals(
	app core.App, request pageMarkdownReplacement, current pageAuthorization, before, markdown string,
) (string, []notionMarkdownSyncedWrite, error) {
	// "v1" reads a tag's values in single quotes or none as in double ones
	// (notion_page_markdown_version1_quotes.go), an original's here too.
	if !request.Version2 {
		markdown = notionMarkdownVersion1Quotes(markdown)
	}
	read := map[string]bool{}
	for _, content := range notionMarkdownSyncedContents(before, request.Version2) {
		read[content.address+"\x00"+content.text()] = true
	}
	writes := []notionMarkdownSyncedWrite{}
	for _, content := range notionMarkdownSyncedContents(markdown, request.Version2) {
		if read[content.address+"\x00"+content.text()] {
			continue
		}
		blockID, pageID, err := notionMarkdownSyncedOriginal(
			app, request.PrincipalID, current, request.Allows, request.Reader.Origin, content.address, true,
		)
		if err != nil {
			return "", nil, err
		}
		// One showing a page's body leaves that page as it is: Notion's
		// write of one fails (a 500, seen 2026-11-27); Leafen says why.
		if blockID == pageID {
			return "", nil, graphInputError(errorCodeGraphReferenceInvalid, "A synced block reference showing a whole "+
				"page's body can't change that page: leave the reference as it reads, and edit the page itself.")
		}
		if pageID == current.Page.Id {
			markdown = notionMarkdownSyncedSplice(app, markdown, blockID, content)
			continue
		}
		writes = append(writes, notionMarkdownSyncedWrite{blockID: blockID, pageID: pageID, content: content})
	}
	return markdown, writes, nil
}

// writeNotionMarkdownSyncedOriginals writes each original the lines its
// reference gives it, by its page's own Markdown, deleting there what the
// write may delete, and says whether one changed; the page written is then
// read again, its references with their originals' blocks.
func (p *pocketBaseYgoPersistence) writeNotionMarkdownSyncedOriginals(
	ctx context.Context, app core.App, request pageMarkdownReplacement, writes []notionMarkdownSyncedWrite,
	allowDeleting bool, result *pageMarkdownReplaced,
) (bool, error) {
	changed := false
	for _, write := range writes {
		publicPageID, authorize, err := request.SyncedOriginal(app, write.pageID)
		if err != nil {
			return changed, err
		}
		written, err := p.replacePageMarkdown(ctx, app, pageMarkdownReplacement{
			PrincipalID: request.PrincipalID, PublicPageID: publicPageID,
			MutationID: "notion-synced-original:" + newDiagnosticReference(),
			Reader:     request.Reader, Version2: request.Version2, PrepareInput: request.PrepareInput,
			Uploads: request.Uploads, Allows: request.Allows, SkipUnchanged: true, Authorize: authorize,
			Next: func(current string) (string, bool, error) {
				return notionMarkdownSyncedSplice(app, current, write.blockID, write.content), allowDeleting, nil
			},
		})
		if err != nil {
			return changed, err
		}
		changed = changed || !written.Receipt.Duplicate
	}
	if !changed {
		return false, nil
	}
	return true, app.RunInTransaction(func(txApp core.App) error {
		current, err := request.Authorize(txApp)
		if err != nil {
			return err
		}
		result.Before, err = projectNotionPageMarkdown(
			txApp, current.Workspace.OwnerID, current.Page.Id, request.PublicPageID, request.Reader,
		)
		return err
	})
}

// notionSyncedOriginalWrite admits a public API write to an original's page
// as a write of that page.
func notionSyncedOriginalWrite(
	identity notionOAuthBearerIdentity,
) func(core.App, string) (string, func(core.App) (pageAuthorization, error), error) {
	return func(app core.App, pageID string) (string, func(core.App) (pageAuthorization, error), error) {
		publicPageID, err := notionPagePublicID(app, pageID)
		return publicPageID, notionSyncedOriginalAdmission(authorizeNotionPageMutation(identity, pageID)), err
	}
}

// notionMarkdownSyncedSplice puts a reference's lines in place of what an
// original synced block holds in a page's Markdown, as far in as the
// original stands; Markdown without it is as it was.
func notionMarkdownSyncedSplice(app core.App, markdown, blockID string, content notionMarkdownSyncedContent) string {
	lines := strings.Split(markdown, "\n")
	fence := ""
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			fence = notionMarkdownFenceOf(trimmed)
			continue
		}
		match := notionMarkdownSyncedOriginalOpen.FindStringSubmatch(trimmed)
		if match == nil || notionMarkdownSyncedAddressBlock(app, html.UnescapeString(notionMarkdownAttributes(match[2])["url"])) != blockID {
			continue
		}
		end := notionMarkdownSyncedClosing(lines, index, match[1], notionMarkdownSyncedOriginalOpen)
		if end < 0 {
			return markdown
		}
		lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		spliced := slices.Clone(lines[:index+1])
		for _, held := range content.lines {
			switch {
			case strings.TrimSpace(held) == "":
				spliced = append(spliced, "")
			case strings.HasPrefix(held, content.lead):
				spliced = append(spliced, lead+held[len(content.lead):])
			default:
				spliced = append(spliced, lead+strings.TrimLeft(held, " \t"))
			}
		}
		return strings.Join(append(spliced, lines[end:]...), "\n")
	}
	return markdown
}

// notionMarkdownSyncedAddressBlock is the block a synced block's address
// names after #, or "".
func notionMarkdownSyncedAddressBlock(app core.App, address string) string {
	parsed, err := url.Parse(address)
	if err != nil {
		return ""
	}
	id := notionMarkdownAddressHex(parsed.Fragment)
	if id == "" || len(strings.ReplaceAll(parsed.Fragment, "-", "")) != len(id) {
		return ""
	}
	blockID, _ := resolveNotionBlockID(app, id[0:8]+"-"+id[8:12]+"-"+id[12:16]+"-"+id[16:20]+"-"+id[20:])
	return blockID
}

// notionSyncedOriginalAdmission is a page write's admission that answers a
// page the writer can't write as a block update there is answered.
func notionSyncedOriginalAdmission(authorize func(core.App) (pageAuthorization, error)) func(core.App) (pageAuthorization, error) {
	return func(app core.App) (pageAuthorization, error) {
		current, err := authorize(app)
		if requestError := (&graphRequestError{}); errors.As(err, &requestError) && requestError.code == errorCodePageNotFound {
			return current, errNotionSyncedOriginalNotFound
		}
		return current, err
	}
}
