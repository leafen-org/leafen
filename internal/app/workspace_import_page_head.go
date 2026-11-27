package app

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// An exported page's icon and cover stand in its HTML file's header, Notion's
// export's and Leafen's alike (htmlImportPageHead.ts): brought back, they are
// the page's, not lines of its body. The converted document opens with one
// line naming them, [](leafen-page-head:<icon>,<cover>,<place>), each part
// written as a URL's: the icon as an export names one
// (workspaceImportIconOf), the cover a picture's web address or one the
// import uploaded (leafen-file:), and the place the cover is moved to, 0–100
// from its top. The line is taken off before the document is read and gives
// its page, once its text is in, each of them while it has none.

// workspaceImportStagedPageHead is the icon and cover a document's first
// line names for its page.
type workspaceImportStagedPageHead struct {
	Icon     string `json:"icon,omitempty"`
	IconType string `json:"iconType,omitempty"`
	Cover    string `json:"cover,omitempty"`
	Position int    `json:"position"`
}

var workspaceImportPageHeadLine = regexp.MustCompile(`^\[\]\(` + regexp.QuoteMeta(workspaceImportPageMark) + `([^()\s]*)\)[ \t]*(?:\n|$)`)

// workspaceImportPageHeadOf takes a document's page line off its start and
// gives what it names, false for no line or one naming nothing it reads.
func workspaceImportPageHeadOf(markdown string) (string, workspaceImportStagedPageHead, bool) {
	match := workspaceImportPageHeadLine.FindStringSubmatchIndex(markdown)
	if match == nil {
		return markdown, workspaceImportStagedPageHead{}, false
	}
	parts := strings.SplitN(markdown[match[2]:match[3]], ",", 3)
	for len(parts) < 3 {
		parts = append(parts, "")
	}
	head := workspaceImportStagedPageHead{Position: 50}
	icon, _ := url.PathUnescape(parts[0])
	switch value, kind := workspaceImportIconOf(icon); kind {
	case workspaceImportIconUploaded, notionIconTypeExternal:
		// A page's picture icon is an image, uploaded or at its address.
		head.Icon, head.IconType = value, "image"
	case "":
	default:
		head.Icon, head.IconType = value, kind
	}
	cover, _ := url.PathUnescape(parts[1])
	cover = strings.TrimSpace(cover)
	if file, uploaded := strings.CutPrefix(cover, workspaceImportFileReference); uploaded {
		if workspaceImportReferenceIDPattern.MatchString(file) {
			head.Cover = file
		}
	} else if notionPageExternalFile(cover) != nil {
		head.Cover = cover
	}
	if place, err := strconv.Atoi(parts[2]); err == nil && place >= 0 && place <= 100 {
		head.Position = place
	}
	return strings.TrimLeft(markdown[match[1]:], "\n"), head, head.Icon != "" || head.Cover != ""
}

// headWorkspaceImportPage gives a page the icon and cover its document's
// header names, each while it has none, a row page on its page record
// (headWorkspaceImportRowPage) and the import's own page, still hidden while
// it is made, on its record as its title (headWorkspaceImportStagingPage), and
// reads the page again for what follows.
func headWorkspaceImportPage(app core.App, actorID string, page *core.Record, head workspaceImportStagedPageHead) (*core.Record, error) {
	if page.GetBool("in_trash") {
		return page, nil
	}
	if page.GetString("data_source_id") != "" {
		if err := headWorkspaceImportRowPage(app, actorID, page, head); err != nil {
			return nil, err
		}
		fresh, err := app.FindRecordById(pagesCollection, page.Id)
		if errors.Is(err, sql.ErrNoRows) {
			return page, nil
		}
		return fresh, err
	}
	if page.GetBool("notion_import_staging") {
		return headWorkspaceImportStagingPage(app, page, head)
	}
	input := pageMetadataInput{
		Title: page.GetString("title"), ParentID: page.GetString("parent_id"),
		MutationID: fmt.Sprintf("workspace-import-head-%s-%d", page.Id, page.GetInt("cursor")), BaseCursor: page.GetInt("cursor"),
	}
	if kind := page.GetString("icon_type"); head.Icon != "" && page.GetString("icon") == "" && (kind == "" || kind == "none") {
		input.Icon, input.IconType = &head.Icon, &head.IconType
	}
	if head.Cover != "" && page.GetString("cover") == "" {
		input.Cover, input.CoverPosition = &head.Cover, &head.Position
	}
	if input.Icon == nil && input.Cover == nil {
		return page, nil
	}
	input = normalizePageMetadataInput(input)
	if !validPageMetadataMutationInput(page.Id, input) {
		return page, nil
	}
	if _, _, err := applyPageMetadataTransaction(app, actorID, page.Id, input, pageMetadataRequestHash(page.Id, input)); err != nil {
		return nil, err
	}
	fresh, err := app.FindRecordById(pagesCollection, page.Id)
	if errors.Is(err, sql.ErrNoRows) {
		return page, nil
	}
	return fresh, err
}
