package app

import (
	"maps"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Notion's public pages show a mentioned page or database by its current
// name and icon when a visitor can open it there, and without its name when
// they cannot (seen 2026-10-01: "사용 권한 없음"). A public page's text,
// captions, table cells and database text cells therefore carry a mention's
// target, name and, for a page, its icon only when the target opens through
// the same authority as a page link does, and a database's mention names the
// page holding it, which opens too; any other mention keeps its place with
// the "unavailable" mark and, for a page, the page's id alone — Notion links
// such a mention to the page's own address, where it isn't found (user
// decision 2026-10-01) — never its name or icon. A link to a page that does
// not open keeps its words without the link.

const (
	publicMentionUnavailableKey = "unavailable"
	// What a public page's mention carries of the target the visitor opens:
	// a page's icon, or without one whether its body holds anything (a lined
	// document then, an empty one otherwise, as a member's mention shows it),
	// and the page holding a database, marked when that page is the
	// database's own.
	publicMentionIconKey       = "mentionIcon"
	publicMentionIconTypeKey   = "mentionIconType"
	publicMentionHasContentKey = "mentionHasContent"
	publicMentionPageKey       = "mentionPageId"
	publicMentionPageKindKey   = "mentionPageKind"
	// A page mention's page, while untitled, named by its kind: a full-page
	// database "database" (mentionPageKind), a view block's page marked too.
	publicMentionViewBlockKey = "mentionViewBlock"
)

var publicSpanListFields = []string{"rich", "caption", "notionRichTextSpans"}

var publicMentionFormatKeys = []string{"bold", "italic", "underline", "strikethrough", "code", "color"}

func projectPublicMentions(
	app core.App,
	authorization publicVisibilityAuthorization,
	pageID string,
	items []graphReadItem,
	now time.Time,
) ([]graphReadItem, error) {
	pageTargets, databaseTargets, people := []string{}, []string{}, []string{}
	seen := map[string]bool{}
	for _, item := range items {
		mapPublicItemSpans(item, func(span map[string]any) (map[string]any, bool) {
			// A person mention reads with the person's name now (person_mention_names.go).
			if person := publicSpanString(span, "userId"); span["mention"] == "person" && person != "" && !seen["person:"+person] {
				seen["person:"+person] = true
				people = append(people, person)
			}
			pageTarget, databaseTarget := publicSpanTarget(span)
			if pageTarget != "" && !seen["page:"+pageTarget] {
				seen["page:"+pageTarget] = true
				pageTargets = append(pageTargets, pageTarget)
			}
			if databaseTarget != "" && !seen["database:"+databaseTarget] {
				seen["database:"+databaseTarget] = true
				databaseTargets = append(databaseTargets, databaseTarget)
			}
			return nil, false
		})
	}
	if len(pageTargets) == 0 && len(databaseTargets) == 0 && len(people) == 0 {
		return items, nil
	}
	names, err := personMentionNames(app, authorization.WorkspaceID, people)
	if err != nil {
		return nil, err
	}

	pages, err := loadPublicLinkedPages(app, authorization, pageTargets)
	if err != nil {
		return nil, err
	}
	databases, err := loadPublicMentionedDatabases(app, authorization, databaseTargets)
	if err != nil {
		return nil, err
	}
	sources, sourceIDs, err := loadPublicMentionedSources(app, authorization, databaseTargets)
	if err != nil {
		return nil, err
	}
	maps.Copy(databases, sources)
	reach := newPublicLinkedReach(app, authorization, now, pageID)
	opens := map[string]bool{}
	opensPage := func(target string) (bool, error) {
		// Every target is decided, the read's reach remembering what it has.
		if open, checked := opens[target]; checked {
			return open, nil
		}
		open, err := reach.reachable(target)
		if err != nil {
			return false, err
		}
		opens[target] = open
		return open, nil
	}
	openPages := map[string]storedPublicLinkedPage{}
	for _, target := range pageTargets {
		page, live := pages[target]
		if !live {
			continue
		}
		open, err := opensPage(target)
		if err != nil {
			return nil, err
		}
		if open {
			openPages[target] = page
		}
	}
	openDatabases := map[string]storedPublicMentionedDatabase{}
	for _, target := range databaseTargets {
		database, live := databases[target]
		if !live {
			continue
		}
		open, err := opensPage(database.PageID)
		if err != nil {
			return nil, err
		}
		if open {
			openDatabases[target] = database
		}
	}
	// What they name, as the visitor reads it (public_content_names.go).
	closer := publicReaderCloser(app, reach)
	publicLinkedPagesAsRead(app, closer, openPages)
	databaseSources, err := publicMentionedDatabasesAsRead(app, closer, openDatabases)
	if err == nil {
		err = publicMentionedSourcesNamed(app, closer, openDatabases, sourceIDs)
	}
	if err != nil {
		return nil, err
	}

	projected := append([]graphReadItem(nil), items...)
	for index, item := range items {
		value, changed := mapPublicItemSpans(item, func(span map[string]any) (map[string]any, bool) {
			if span["mention"] == "person" {
				return publicPersonSpan(span, names)
			}
			return projectPublicSpan(span, pages, openPages, openDatabases, databaseSources)
		})
		if changed {
			projected[index].Value = value
		}
	}
	return projected, nil
}

// mapPublicItemSpans applies mapSpan to the spans a block or a row holds,
// and returns a copy of the item's value holding the changed spans.
func mapPublicItemSpans(
	item graphReadItem, mapSpan func(map[string]any) (map[string]any, bool),
) (map[string]any, bool) {
	field := ""
	var next map[string]any
	changed := false
	switch item.Kind {
	case graphReadKindBlock:
		content, ok := item.Value["content"].(map[string]any)
		if !ok {
			return nil, false
		}
		field = "content"
		next, changed = mapPublicContentSpans(content, mapSpan)
	case graphReadKindRow:
		values, ok := item.Value["values"].(map[string]any)
		if !ok {
			return nil, false
		}
		field = "values"
		next, changed = mapPublicRowSpans(values, mapSpan)
	}
	if !changed {
		return nil, false
	}
	value := make(map[string]any, len(item.Value))
	for key, current := range item.Value {
		value[key] = current
	}
	value[field] = next
	return value, true
}

// mapPublicRowSpans applies mapSpan to the spans of a row's text cells; a
// changed cell's plain value is rewritten from them.
func mapPublicRowSpans(
	values map[string]any, mapSpan func(map[string]any) (map[string]any, bool),
) (map[string]any, bool) {
	var next map[string]any
	for propertyID, raw := range values {
		cell, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		spans, changed := mapPublicSpans(publicSpanList(cell["spans"]), mapSpan)
		if !changed {
			continue
		}
		nextCell := make(map[string]any, len(cell))
		for key, value := range cell {
			nextCell[key] = value
		}
		nextCell["spans"] = spans
		if _, text := cell["value"].(string); text {
			nextCell["value"] = publicSpansText(spans)
		}
		if next == nil {
			next = make(map[string]any, len(values))
			for key, value := range values {
				next[key] = value
			}
		}
		next[propertyID] = nextCell
	}
	if next == nil {
		return values, false
	}
	return next, true
}

// publicSpanList reads spans a row cell keeps as spans in the form block
// content keeps them.
func publicSpanList(value any) any {
	spans, ok := value.([]graphTextSpan)
	if !ok {
		return value
	}
	list := make([]any, len(spans))
	for index, span := range spans {
		list[index] = publicTextSpanValue(span)
	}
	return list
}

// publicSpanTarget names the page a span mentions or links to, or the
// database it mentions.
func publicSpanTarget(span map[string]any) (pageID, databaseID string) {
	switch span["mention"] {
	case "page":
		return publicSpanString(span, "pageId"), ""
	case "database":
		return "", publicSpanString(span, "databaseId")
	case nil, "":
	default:
		return "", ""
	}
	link, _ := span["link"].(string)
	return linkedPageID(link), ""
}

// publicPersonSpan is a person mention with the person's name now.
func publicPersonSpan(span map[string]any, names map[string]string) (map[string]any, bool) {
	written, _ := span["text"].(string)
	text := personMentionText(names, publicSpanString(span, "userId"), written)
	if text == written {
		return nil, false
	}
	next := make(map[string]any, len(span))
	for key, value := range span {
		next[key] = value
	}
	next["text"] = text
	return next, true
}

func projectPublicSpan(
	span map[string]any,
	live map[string]storedPublicLinkedPage,
	openPages map[string]storedPublicLinkedPage,
	openDatabases map[string]storedPublicMentionedDatabase, databaseSources map[string][]databaseSourceTitle,
) (map[string]any, bool) {
	pageTarget, databaseTarget := publicSpanTarget(span)
	switch span["mention"] {
	case "page":
		page, open := openPages[pageTarget]
		if !open {
			return publicUnavailableMention(span, "page", pageTarget), true
		}
		icon, iconType := "", ""
		if page.IconType != "" && page.IconType != "none" && page.Icon != "" {
			icon, iconType = page.Icon, page.IconType
		}
		text := page.Title
		// A mention of a block reads its page's title, " - " and the words it
		// was written with, as Notion's page draws one (page_mention_block_anchor.go).
		if publicSpanString(span, "blockId") != "" && page.Title != "" {
			text = page.Title + " - " + publicSpanString(span, "text")
		}
		pageKind := ""
		if readPageKind(page.Kind) == pageKindDatabase {
			pageKind = pageKindDatabase
		}
		return publicSpanWith(span, map[string]any{
			"text": text, publicMentionIconKey: icon, publicMentionIconTypeKey: iconType,
			publicMentionHasContentKey: page.HasContent, publicMentionPageKey: "", publicMentionPageKindKey: pageKind,
			publicMentionViewBlockKey: page.ViewBlock, publicMentionSourceTitlesKey: page.SourceTitles,
		})
	case "database":
		database, open := openDatabases[databaseTarget]
		if !open {
			return publicUnavailableMention(span, "database", ""), true
		}
		pageKind := ""
		if database.PageKind == "database" {
			pageKind = database.PageKind
		}
		return publicSpanWith(span, map[string]any{
			"text": database.Title, publicMentionIconKey: database.Icon, publicMentionIconTypeKey: database.IconType, publicMentionHasContentKey: false,
			publicMentionPageKey: database.PageID, publicMentionPageKindKey: pageKind, publicMentionSourceTitlesKey: databaseSources[databaseTarget],
		})
	}
	// A link to something other than a live page of the workspace is left as
	// written.
	if _, isPage := live[pageTarget]; !isPage {
		return nil, false
	}
	if _, open := openPages[pageTarget]; open {
		return nil, false
	}
	next := make(map[string]any, len(span))
	for key, value := range span {
		next[key] = value
	}
	delete(next, "link")
	return next, true
}

// publicSpanWith gives the span the values the visitor reads, each a word,
// a yes or words, and drops a key whose value is empty or no; it keeps the
// span when nothing changes.
func publicSpanWith(span map[string]any, values map[string]any) (map[string]any, bool) {
	dropped := func(key string, value any) bool { return key != "text" && publicSpanValueEmpty(value) }
	same := true
	for key, value := range values {
		current, present := span[key]
		if dropped(key, value) {
			same = same && !present
		} else {
			same = same && present && publicSpanValueIs(current, value)
		}
	}
	if same {
		return nil, false
	}
	next := make(map[string]any, len(span)+len(values))
	for key, value := range span {
		next[key] = value
	}
	for key, value := range values {
		if dropped(key, value) {
			delete(next, key)
		} else {
			next[key] = value
		}
	}
	return next, true
}

// publicSpanValueIs reports whether a span's value is the word or yes given,
// whatever else a stored span holds there.
func publicSpanValueIs(current, value any) bool {
	switch value := value.(type) {
	case string:
		text, ok := current.(string)
		return ok && text == value
	case bool:
		yes, ok := current.(bool)
		return ok && yes == value
	}
	return publicSpanWordsAre(current, value)
}

// publicUnavailableMention keeps a mention's place and formatting, and of
// its target a page's id alone.
func publicUnavailableMention(span map[string]any, kind, pageID string) map[string]any {
	next := map[string]any{"text": "", "mention": kind, publicMentionUnavailableKey: true}
	if pageID != "" && len(pageID) <= maxGraphIdentifierLength {
		next["pageId"] = pageID
	}
	for _, key := range publicMentionFormatKeys {
		if value, ok := span[key]; ok {
			next[key] = value
		}
	}
	return next
}

// mapPublicContentSpans applies mapSpan to each span of the content's text,
// caption and table cells, and returns a copy holding the changed spans. A
// change to the rich text rewrites the plain text from it.
func mapPublicContentSpans(
	content map[string]any, mapSpan func(map[string]any) (map[string]any, bool),
) (map[string]any, bool) {
	var next map[string]any
	set := func(key string, value any) {
		if next == nil {
			next = make(map[string]any, len(content))
			for field, item := range content {
				next[field] = item
			}
		}
		next[key] = value
	}
	for _, field := range publicSpanListFields {
		if spans, changed := mapPublicSpans(content[field], mapSpan); changed {
			set(field, spans)
			if field == "rich" {
				set("text", publicSpansText(spans))
			}
		}
	}
	if cells, ok := content["cells"].([]any); ok {
		var nextCells []any
		for index, cell := range cells {
			spans, changed := mapPublicSpans(cell, mapSpan)
			if !changed {
				continue
			}
			if nextCells == nil {
				nextCells = append([]any(nil), cells...)
			}
			nextCells[index] = spans
		}
		if nextCells != nil {
			set("cells", nextCells)
		}
	}
	if next == nil {
		return content, false
	}
	return next, true
}

func mapPublicSpans(value any, mapSpan func(map[string]any) (map[string]any, bool)) ([]any, bool) {
	spans, ok := value.([]any)
	if !ok {
		return nil, false
	}
	var next []any
	for index, raw := range spans {
		span, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		mapped, changed := mapSpan(span)
		if !changed {
			continue
		}
		if next == nil {
			next = append([]any(nil), spans...)
		}
		next[index] = mapped
	}
	return next, next != nil
}

func publicSpansText(spans []any) string {
	var text strings.Builder
	for _, raw := range spans {
		if span, ok := raw.(map[string]any); ok {
			value, _ := span["text"].(string)
			text.WriteString(value)
		}
	}
	return text.String()
}

func publicSpanString(span map[string]any, key string) string {
	value, _ := span[key].(string)
	return strings.TrimSpace(value)
}
