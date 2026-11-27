package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A page or database source keeps the Notion icon and cover Leafen can store
// directly: emoji, external images and built-in icons. The source's text
// holds them, since pages and databases keep nothing else there.

const notionPresentationSourcePrefix = "leafen-notion-presentation-v1:"

type notionImportPresentation struct {
	Icon     json.RawMessage `json:"icon,omitempty"`
	Cover    json.RawMessage `json:"cover,omitempty"`
	Archived bool            `json:"archived,omitempty"`
	// Description is a database's description as plain text.
	Description string `json:"description,omitempty"`
	// TitleSpans and DescriptionSpans are a database's name and description
	// as Notion formatted them, when it did; TitleSpans an ordinary page's
	// title too (page_title_spans.go).
	TitleSpans       []graphTextSpan `json:"titleSpans,omitempty"`
	DescriptionSpans []graphTextSpan `json:"descriptionSpans,omitempty"`
	// Anchor is what a sub-page its mention found hangs on: "title", "cell" for a database page's
	// (notion_import_field_pages.go), "block", or "unmentioned" (notion_import_line_pages.go).
	Anchor string `json:"anchor,omitempty"`
	// History is a page's Notion times and people (notion_import_page_stamps.go).
	History *notionImportPageHistory `json:"history,omitempty"`
}

// notionImportPresentationText encodes the icon, cover when asked, archived
// state and description a Notion page or database carries, "" for none.
func notionImportPresentationText(object notionProviderObject, withCover bool) string {
	return encodeNotionImportPresentation(notionImportObjectPresentation(object, withCover))
}

// notionImportObjectPresentation is what a Notion page or database carries
// that its source keeps (notionImportPresentationText).
func notionImportObjectPresentation(object notionProviderObject, withCover bool) notionImportPresentation {
	presentation := notionImportPresentation{
		Icon:     notionImportPresentationValue(object.Icon, "emoji", "external", nativeIconType),
		Archived: object.Object == "page" && object.IsArchived,
	}
	if withCover {
		presentation.Cover = notionImportPresentationValue(object.Cover, "external")
	}
	if object.Object == "database" {
		var description strings.Builder
		for _, part := range object.Description {
			description.WriteString(part.PlainText)
		}
		runes := []rune(description.String())
		presentation.Description = string(runes[:min(len(runes), maxGraphTextCharacters)])
		presentation.TitleSpans = notionImportNameSpans(object.Title, true)
		if len(runes) <= maxGraphTextCharacters {
			presentation.DescriptionSpans = notionImportNameSpans(object.Description, false)
		}
	}
	// An ordinary page's title keeps its formatting, as a database's name
	// does: its marks, links and equations; and its page, database and date
	// mentions, as Leafen's titles hold them (page_title_spans.go), a page
	// or database one naming its Notion object until the import is published
	// (resolveNotionImportTitleSpans). A person's mention is its words.
	if object.Object == "page" && object.Parent.Type != "database_id" && object.Parent.Type != "data_source_id" {
		for _, property := range object.Properties {
			if parts, err := notionProviderRichTextParts(property.Title); err == nil && len(parts) > 0 {
				presentation.TitleSpans = notionImportNameSpans(parts, true, "page", "database", "date")
				break
			}
		}
	}
	return presentation
}

// encodeNotionImportPresentation is a source's text for its presentation,
// "" for none.
func encodeNotionImportPresentation(presentation notionImportPresentation) string {
	if presentation.Icon == nil && presentation.Cover == nil && !presentation.Archived && presentation.Description == "" &&
		len(presentation.TitleSpans) == 0 && presentation.Anchor == "" && presentation.History == nil {
		return ""
	}
	encoded, err := json.Marshal(presentation)
	if err != nil {
		return ""
	}
	return notionPresentationSourcePrefix + string(encoded)
}

func notionImportPresentationValue(raw json.RawMessage, types ...string) json.RawMessage {
	value := struct {
		Type string `json:"type"`
	}{}
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return nil
	}
	for _, kind := range types {
		if value.Type == kind {
			return raw
		}
	}
	return nil
}

// decodeNotionImportPresentation reads a source's stored presentation.
func decodeNotionImportPresentation(text string) (notionPagePresentationRequest, bool) {
	if !strings.HasPrefix(text, notionPresentationSourcePrefix) {
		return notionPagePresentationRequest{}, false
	}
	stored := notionImportPresentation{}
	if json.Unmarshal([]byte(strings.TrimPrefix(text, notionPresentationSourcePrefix)), &stored) != nil {
		return notionPagePresentationRequest{}, false
	}
	presentation, err := notionPagePresentation(stored.Icon, stored.Cover)
	if err != nil || presentation.IconManaged || presentation.IconCustomEmoji || presentation.CoverManaged {
		return notionPagePresentationRequest{}, false
	}
	return presentation, true
}

// applyNotionImportPagePresentation sets a new page's stored icon and cover,
// and archives it, by the person importing it, when Notion has it archived.
func applyNotionImportPagePresentation(page *core.Record, text, importerID string) {
	stored := notionImportPresentation{}
	if strings.HasPrefix(text, notionPresentationSourcePrefix) &&
		json.Unmarshal([]byte(strings.TrimPrefix(text, notionPresentationSourcePrefix)), &stored) == nil && stored.Archived {
		applyPageArchiveState(page, true, importerID, time.Now().UTC().Format(time.RFC3339Nano))
	}
	presentation, ok := decodeNotionImportPresentation(text)
	if !ok {
		return
	}
	if presentation.Icon != nil && presentation.IconType != nil {
		page.Set("icon", *presentation.Icon)
		page.Set("icon_type", *presentation.IconType)
	}
	if presentation.Cover != nil {
		applyPageCover(page, presentation.Cover, nil)
	}
}

// recordNotionImportPresentation keeps the presentation of a page or database
// source staged before its Notion object was read. It changes only a source
// with no presentation yet, so a retried discovery turn is harmless.
func recordNotionImportPresentation(app core.App, jobID, sourceID, kind, text string) error {
	if text == "" {
		return nil
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_import_sources}} SET [[text]] = {:text}
		WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId} AND [[kind]] = {:kind} AND [[text]] = ''
	`).Bind(dbx.Params{"text": text, "jobId": jobID, "sourceId": sourceID, "kind": kind}).Execute()
	return err
}

// recordNotionImportPagePresentation reads a child page, which its parent
// listed as a block without an icon or cover, keeps its presentation and
// returns the file sources of its hosted images. A rate limit or cancellation
// retries the turn; a page Notion will not return keeps its content without
// an icon or cover.
func recordNotionImportPagePresentation(
	ctx context.Context, app core.App, client notionProviderClient, jobID, sourceID string,
) ([]stagedNotionSource, []string, error) {
	page, err := client.page(ctx, sourceID)
	rateLimited := &notionProviderRateLimitError{}
	if errors.As(err, &rateLimited) || ctx.Err() != nil {
		return nil, nil, errors.Join(err, ctx.Err())
	}
	if err != nil || page.Object != "page" || page.ID != sourceID {
		return nil, nil, nil
	}
	if err := recordNotionImportPagePresentationText(ctx, app, client, jobID, sourceID, page); err != nil {
		return nil, nil, err
	}
	return notionImportPresentationFileSources(app, page, sourceID, false, true)
}

// notionImportDataSource is an imported database's source with the database
// icon and, for an inline database, its cover: a copied hosted image, or what
// Leafen stores directly. A full-page database's cover is its page's.
func notionImportDataSource(app core.App, job storedJob, source storedNotionSource, id, title string) (*graphDataSource, error) {
	dataSource := &graphDataSource{ID: id, Title: title}
	stored := notionImportPresentation{}
	if strings.HasPrefix(source.Text, notionPresentationSourcePrefix) &&
		json.Unmarshal([]byte(strings.TrimPrefix(source.Text, notionPresentationSourcePrefix)), &stored) == nil {
		dataSource.TitleSpans = notionImportSpelledSpans(stored.TitleSpans, title)
		if stored.Description != "" {
			dataSource.Description = &stored.Description
			dataSource.DescriptionSpans = notionImportSpelledSpans(stored.DescriptionSpans, stored.Description)
		}
	}
	inline := source.ParentSourceID != notionDatabaseRootPageSourceID(source.SourceID)
	if presentation, ok := decodeNotionImportPresentation(source.Text); ok {
		if presentation.Icon != nil && presentation.IconType != nil {
			dataSource.Icon, dataSource.IconType = presentation.Icon, presentation.IconType
		}
		if inline && presentation.Cover != nil && *presentation.Cover != "" {
			dataSource.Cover = presentation.Cover
		}
	}
	fileID, err := notionImportCopiedPresentationFile(app, job.ID, job.WorkspaceID, source.SourceID+notionPresentationDatabaseIcon)
	if err != nil {
		return nil, err
	}
	if fileID != "" {
		image := graphDataSourceIconImage
		dataSource.Icon, dataSource.IconType = &fileID, &image
	}
	if !inline {
		return dataSource, nil
	}
	coverID, err := notionImportCopiedPresentationFile(app, job.ID, job.WorkspaceID, source.SourceID+notionPresentationDatabaseCover)
	if err != nil {
		return nil, err
	}
	if coverID != "" {
		dataSource.Cover = &coverID
	}
	return dataSource, nil
}

// Notion-hosted icons and covers, and custom emoji icons, are images Notion
// serves from expiring URLs. Each one is staged as a file source named after
// the Notion object and field, copied with the other import files, and used
// by the page or database that owns it instead of becoming a block.
const (
	notionPresentationPageIcon      = "#page-icon"
	notionPresentationPageCover     = "#page-cover"
	notionPresentationDatabaseIcon  = "#database-icon"
	notionPresentationDatabaseCover = "#database-cover"
	// A callout's uploaded icon, or its custom emoji's picture, is the
	// callout's uploaded picture (notion_import_block_icon.go).
	notionPresentationBlockIcon = "#block-icon"
)

// notionImportPresentationFileSources stages a Notion object's hosted icon
// and cover images.
func notionImportPresentationFileSources(
	app core.App, object notionProviderObject, parentSourceID string, database, withCover bool,
) ([]stagedNotionSource, []string, error) {
	targets := []struct {
		suffix string
		raw    json.RawMessage
	}{{notionPresentationPageIcon, object.Icon}, {notionPresentationPageCover, object.Cover}}
	if database {
		targets[0].suffix, targets[1].suffix = notionPresentationDatabaseIcon, notionPresentationDatabaseCover
	}
	if !withCover {
		targets = targets[:1]
	}
	sources, fileIDs := []stagedNotionSource{}, []string{}
	for _, target := range targets {
		rawURL, name := notionImportPresentationFileURL(target.raw)
		if rawURL == "" {
			continue
		}
		key, err := notionImportEncryptionKey(app)
		if err != nil {
			return nil, nil, err
		}
		cipher, err := security.Encrypt([]byte(rawURL), key)
		if err != nil {
			return nil, nil, err
		}
		sourceID := object.ID + target.suffix
		sources = append(sources, stagedNotionSource{
			SourceID: sourceID, Kind: graphBlockTypeFile, ParentSourceID: parentSourceID, ParentKind: "page",
			Title: name, Text: notionFileSourcePrefix + `{"caption":""}`, FileURLCipher: cipher,
		})
		fileIDs = append(fileIDs, sourceID)
	}
	return sources, fileIDs, nil
}

// notionImportPresentationFileURL returns the address of a hosted icon or
// cover image and the name it goes by, or "" for any other kind.
func notionImportPresentationFileURL(raw json.RawMessage) (string, string) {
	value := struct {
		Type string `json:"type"`
		File struct {
			URL string `json:"url"`
		} `json:"file"`
		CustomEmoji struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"custom_emoji"`
	}{}
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return "", ""
	}
	switch value.Type {
	case "file":
		return strings.TrimSpace(value.File.URL), notionHostedFileName(value.File.URL, "notion-presentation")
	case "custom_emoji":
		name := strings.TrimSpace(value.CustomEmoji.Name)
		if name == "" {
			name = "notion-custom-emoji"
		}
		return strings.TrimSpace(value.CustomEmoji.URL), name
	}
	return "", ""
}

// notionImportPresentationFileTarget returns the presentation field a file
// source stands for and the Notion object that owns it.
func notionImportPresentationFileTarget(sourceID string) (string, string) {
	for _, suffix := range []string{
		notionPresentationPageIcon, notionPresentationPageCover,
		notionPresentationDatabaseIcon, notionPresentationDatabaseCover, notionPresentationBlockIcon,
	} {
		if owner, found := strings.CutSuffix(sourceID, suffix); found && owner != "" {
			return suffix, owner
		}
	}
	return "", ""
}

// refreshNotionImportPresentationFileURL reads the owning page or database
// again for a fresh address of its hosted image.
func refreshNotionImportPresentationFileURL(
	ctx context.Context, client notionProviderClient, suffix, owner string,
) (string, string, error) {
	var object notionProviderObject
	var err error
	if suffix == notionPresentationBlockIcon {
		object, err = client.block(ctx, owner)
		if err != nil || object.ID != owner {
			return "", "", errors.New(errorCodeNotionImportFileRefreshFailed)
		}
		rawURL, name := notionImportPresentationFileURL(notionImportBlockIconValue(object))
		if rawURL == "" {
			return "", "", errors.New(errorCodeNotionImportFileRefreshFailed)
		}
		return rawURL, name, nil
	}
	if suffix == notionPresentationDatabaseIcon || suffix == notionPresentationDatabaseCover {
		object, _, _, err = client.databaseForImport(ctx, owner)
	} else {
		object, err = client.page(ctx, owner)
	}
	if err != nil || object.ID != owner {
		return "", "", errors.New(errorCodeNotionImportFileRefreshFailed)
	}
	raw := object.Icon
	if suffix == notionPresentationPageCover || suffix == notionPresentationDatabaseCover {
		raw = object.Cover
	}
	rawURL, name := notionImportPresentationFileURL(raw)
	if rawURL == "" {
		return "", "", errors.New(errorCodeNotionImportFileRefreshFailed)
	}
	return rawURL, name, nil
}

// notionImportPresentationFile returns the copied image for a page or
// database source's icon or cover, if Notion hosted one.
func notionImportPresentationFile(app core.App, jobID, workspaceID, sourceID, field string) (string, error) {
	owner, suffix := sourceID, notionPresentationPageIcon
	if field == "cover" {
		suffix = notionPresentationPageCover
	}
	if database, found := strings.CutPrefix(sourceID, "notion-database-root-page:"); found {
		owner, suffix = database, notionPresentationDatabaseIcon
		if field == "cover" {
			suffix = notionPresentationDatabaseCover
		}
	}
	return notionImportCopiedPresentationFile(app, jobID, workspaceID, owner+suffix)
}

// notionImportCopiedPresentationFile returns a presentation file source's
// copied image, or "" when Notion hosted none or it was omitted.
func notionImportCopiedPresentationFile(app core.App, jobID, workspaceID, fileSourceID string) (string, error) {
	task, err := loadNotionFileTask(app, jobID, fileSourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil || task.Status != "copied" || task.FileID == "" {
		return "", err
	}
	file, err := app.FindRecordById(filesCollection, task.FileID)
	if err != nil || !workspaceFileRecordBelongsTo(file, workspaceID) {
		return "", errors.New(errorCodeNotionImportFileCopyRequired)
	}
	return file.Id, nil
}

// applyNotionImportPageFiles sets a new page's copied icon and cover images.
func applyNotionImportPageFiles(app core.App, page *core.Record, jobID, workspaceID, sourceID string) (bool, error) {
	icon, err := notionImportPresentationFile(app, jobID, workspaceID, sourceID, "icon")
	if err != nil {
		return false, err
	}
	cover, err := notionImportPresentationFile(app, jobID, workspaceID, sourceID, "cover")
	if err != nil {
		return false, err
	}
	if icon != "" {
		page.Set("icon", icon)
		page.Set("icon_type", "image")
	}
	if cover != "" {
		applyPageCover(page, &cover, nil)
	}
	return icon != "" || cover != "", nil
}

// notionImportRowIconKey carries a row's directly stored icon, and
// notionImportRowArchivedKey whether Notion has the row archived, among its
// staged values; no property source ID has either form.
const (
	notionImportRowIconKey     = "leafen:row-icon"
	notionImportRowArchivedKey = "leafen:row-archived"
)

// notionImportRowArchived takes the archive mark out of a row's staged
// values and reports it.
func notionImportRowArchived(values map[string]notionDatabaseRowValuePlan) bool {
	_, archived := values[notionImportRowArchivedKey]
	delete(values, notionImportRowArchivedKey)
	return archived
}

// notionImportRowIcon returns the icon an imported row takes: a copied hosted
// image, or the icon Leafen stores directly (its cover: notion_import_row_cover.go).
func notionImportRowIcon(
	app core.App, job storedJob, rowSourceID string, values map[string]notionDatabaseRowValuePlan,
) (string, string, error) {
	icon, iconType := "", ""
	if stored, found := values[notionImportRowIconKey]; found {
		delete(values, notionImportRowIconKey)
		encoded, err := json.Marshal(stored.Value)
		if err != nil {
			return "", "", err
		}
		if presentation, err := notionPagePresentation(encoded, nil); err == nil && presentation.Icon != nil &&
			presentation.IconType != nil && !presentation.IconManaged && !presentation.IconCustomEmoji {
			icon, iconType = *presentation.Icon, *presentation.IconType
		}
	}
	fileID, err := notionImportCopiedPresentationFile(app, job.ID, job.WorkspaceID, rowSourceID+notionPresentationPageIcon)
	if err != nil {
		return "", "", err
	}
	if fileID != "" {
		icon, iconType = fileID, "image"
	}
	return icon, iconType, nil
}

// notionImportNameSpans is the formatting of a Notion database's name or
// description, or a page's title: its marks, colors, links and equations,
// and its mentions of the kinds named; any other mention stays its text.
func notionImportNameSpans(parts []notionProviderRichText, title bool, mentions ...string) []graphTextSpan {
	_, spans, _ := notionImportRichTextCell(parts, title)
	if len(spans) == 0 {
		return nil
	}
	kept := make([]graphTextSpan, 0, len(spans))
	for _, span := range spans {
		if span.Mention != "" && !slices.Contains(mentions, span.Mention) {
			span = graphTextSpan{Text: span.Text, Bold: span.Bold, Italic: span.Italic, Underline: span.Underline,
				Strikethrough: span.Strikethrough, Code: span.Code, Color: span.Color, Background: span.Background}
		}
		kept = append(kept, span)
	}
	return notionImportTidySpans(kept)
}

// notionImportSpelledSpans keeps a name's imported formatting when it still
// spells the name as it was kept, its spaces at the ends too (cut, or a
// database's name of spaces alone left unnamed, it goes).
func notionImportSpelledSpans(spans []graphTextSpan, text string) []graphTextSpan {
	if len(spans) == 0 || spelledName(spans) != text {
		return nil
	}
	return spans
}

// notionImportedPageTitleSpans is what an imported page's title keeps of the
// formatting its source recorded (notionImportPresentationText), while it
// spells the title as kept; none otherwise.
func notionImportedPageTitleSpans(sourceText, title string) string {
	stored := notionImportPresentation{}
	if !strings.HasPrefix(sourceText, notionPresentationSourcePrefix) ||
		json.Unmarshal([]byte(strings.TrimPrefix(sourceText, notionPresentationSourcePrefix)), &stored) != nil {
		return ""
	}
	encoded, err := encodedPageTitleSpans(title, notionImportSpelledSpans(stored.TitleSpans, title))
	if err != nil {
		return ""
	}
	return encoded
}
