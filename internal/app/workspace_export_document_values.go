package app

import (
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// workspaceExportCell is one database value ready to write: plain text for
// CSV and Markdown, and linked parts for HTML.
type workspaceExportCell struct {
	Parts []workspaceExportCellPart
	// Plain is what CSV and Markdown write in place of the parts, when it
	// differs: a relation rollup's page addresses (rollupShownCell).
	Plain string
}

type workspaceExportCellPart struct {
	Text   string
	Href   string
	Class  string
	RowID  string
	FileID string
	// LinkClass is the class its link carries: "url-value" for an address,
	// an email and a phone, as Notion's export marks them (linkedValuePart).
	LinkClass string
	// Runs keep a text value's formatting, links and mentions.
	Runs []workspaceExportTextRun
	// DateTime is a date's start, which HTML writes in <time datetime>.
	DateTime string
	// HTMLText is what HTML writes in place of Text, when it differs: a
	// date on its own clock (workspace_export_document_dates.go).
	HTMLText string
	// Checkbox is "on" or "off" for a checkbox's value, which HTML writes as
	// a checkbox (workspaceExportCheckboxPart).
	Checkbox string
	// Related marks a relation's page, which CSV and Markdown write with
	// where it is (relatedText).
	Related bool
	// File marks a file value's file — one uploaded (FileID), placed in its
	// row page's folder (FileFolder), or one at an address (Href) — which
	// CSV and Markdown write as where it is and HTML as a link, a picture's
	// with its thumbnail (filePartHTML).
	File       bool
	FileFolder string
	// Status is a status value's color, "default" for none of its own,
	// which HTML writes as Notion's status (workspaceExportStatusHTML).
	Status string
}

// cellText writes a value for a CSV file: its parts' texts between ", ", a
// related page with where it is.
func (renderer *workspaceExportRenderer) cellText(cell workspaceExportCell) string {
	if cell.Plain != "" {
		return cell.Plain
	}
	texts := make([]string, 0, len(cell.Parts))
	for _, part := range cell.Parts {
		texts = append(texts, renderer.relatedText(part))
	}
	return strings.Join(texts, ", ")
}

// relatedText is a part's text and, for a relation's page, where it is in
// brackets — its file in the export from the file being written (from the
// export's top in a CSV, databaseCSV), or its page on this server when the
// export leaves it out — as Notion's export writes one (seen 2026-11-12:
// "나 (w671%20…/%EB%82%98%20〈id〉.html)" in a CSV, "나
// (%EB%82%98%20〈id〉.md)" on a row page, "밖일
// (https://app.notion.com/p/〈id〉?pvs=21)" for one left out; 2026-11-13: a
// Markdown & CSV export's CSV names the row page's ".md" file).
func (renderer *workspaceExportRenderer) relatedText(part workspaceExportCellPart) string {
	if part.Related {
		if href := renderer.link(part.RowID); href != "" {
			return part.Text + " (" + href + ")"
		}
	}
	// A file is where it is, as Notion's CSV and Markdown write a file
	// value (seen 2026-11-13): an address as it is, an uploaded file's
	// place in the export from the file being written, its name when the
	// export leaves it out.
	if part.File {
		if href := renderer.filePartHref(part); href != "" {
			return href
		}
	}
	return part.Text
}

// filePartHref is where a file value's file is: its address, or its place
// in the export in its row page's folder.
func (renderer *workspaceExportRenderer) filePartHref(part workspaceExportCellPart) string {
	if part.FileID == "" {
		return part.Href
	}
	folder := part.FileFolder
	if folder == "" {
		folder = renderer.folder
	}
	return renderer.fileHrefIn(part.FileID, folder)
}

// optionName is an option's name and color; one the property no longer has
// has none, and its cells show nothing (databaseValueAsRead).
func (document *workspaceExportDocument) optionName(property *workspaceExportDocProperty, id string) (string, string) {
	options, listed := property.Config["options"].([]any)
	for _, raw := range options {
		option, _ := raw.(map[string]any)
		if mapString(option, "id") == id {
			return mapString(option, "name"), mapString(option, "color")
		}
	}
	if !listed {
		return id, ""
	}
	return "", ""
}

// cell formats one row value the way the database shows it, with the
// values computed on read: formulas, rollups, and the row page's created and
// edited times and people.
func (document *workspaceExportDocument) cell(property *workspaceExportDocProperty, row *workspaceExportDocRow) workspaceExportCell {
	if document.hides(property, row) {
		return workspaceExportCell{}
	}
	wrapper, _ := row.Values[property.ID].(map[string]any)
	value := wrapper["value"]
	cell := workspaceExportCell{}
	add := func(part workspaceExportCellPart) {
		if strings.TrimSpace(part.Text) != "" {
			cell.Parts = append(cell.Parts, part)
		}
	}
	switch property.Type {
	case graphValueTypeTitle:
		// A title of spaces is written as it is, as Notion's CSV quotes it
		// (seen 2026-11-13: "   ").
		if row.Title != "" {
			cell.Parts = append(cell.Parts, workspaceExportCellPart{Text: row.Title, RowID: row.ID})
		}
	case graphValueTypeRichText:
		// A value with spans reads as its spans, so that a date mention's
		// date is the same in every format, and its mentions with their
		// names now (namedRuns), in the CSV as in its row page.
		text, _ := value.(string)
		runs := document.namedRuns(workspaceExportRunsFromSpans(wrapper["spans"], document.Language), document.untitled())
		if len(runs) > 0 {
			text = workspaceExportPlainText(runs)
		}
		add(workspaceExportCellPart{Text: closedMentionsInLine(text, document.Language), Runs: runs})
	case graphValueTypeSelect, graphValueTypeStatus:
		id, ok := value.(string)
		// A status that sets none, or an option it no longer has, reads its
		// default (databaseStatusValueOrDefault).
		if name, _ := document.optionName(property, id); name == "" && property.Type == graphValueTypeStatus {
			if config, err := json.Marshal(property.Config); err == nil {
				id = databaseStatusDefaultOptionID(string(config))
				ok = id != ""
			}
		}
		if ok {
			name, color := document.optionName(property, id)
			if property.Type == graphValueTypeStatus {
				add(workspaceExportCellPart{Text: name, Status: workspaceExportStatusColor(property, id, color)})
			} else {
				add(workspaceExportCellPart{Text: name, Class: "selected-value select-value-color-" + color})
			}
		}
	case graphValueTypeMultiSelect:
		// In its names' order, as Notion's export writes a multi-select in
		// every format (seen 2026-11-14: "10, 9, Alpha, Gamma, beta, zeta,
		// 둘, 셋" for one set as 셋, 둘, zeta, Alpha, beta, Gamma, 10, 9).
		chosen := []workspaceExportCellPart{}
		for _, id := range workspaceExportStrings(value) {
			name, color := document.optionName(property, id)
			chosen = append(chosen, workspaceExportCellPart{Text: name, Class: "selected-value select-value-color-" + color})
		}
		sort.SliceStable(chosen, func(left, right int) bool { return chosen[left].Text < chosen[right].Text })
		for _, part := range chosen {
			add(part)
		}
	case graphValueTypePerson:
		ids := workspaceExportStrings(value)
		// A person that defaults to the creator reads as the row's creator
		// while it names no one (databasePersonDefaultsToCreator).
		if len(ids) == 0 && row.CreatedBy != "" && property.Config["defaultCreator"] == true {
			ids = []string{row.CreatedBy}
		}
		for _, id := range ids {
			add(workspaceExportCellPart{Text: document.personName(id), Class: "user"})
		}
	case graphValueTypeCheckbox:
		checked, _ := value.(bool)
		add(workspaceExportCheckboxPart(checked))
	case graphValueTypeDate:
		add(document.dateCellPart(property, value))
	case graphValueTypeRelation:
		// A related row outside what the person exporting can read stays
		// out, as it does on screen, rather than showing its internal ID.
		for _, id := range workspaceExportStrings(value) {
			if part, ok := document.relatedPart(id); ok {
				cell.Parts = append(cell.Parts, part)
			}
		}
	case graphValueTypeFiles:
		items, _ := value.([]any)
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			name := mapString(item, "name")
			if fileID := mapString(item, "fileId"); fileID != "" {
				add(workspaceExportCellPart{Text: name, FileID: fileID, File: true, FileFolder: row.Folder})
			} else {
				add(workspaceExportCellPart{Text: name, Href: workspaceExportSafeLink(mapString(item, "url")), File: true})
			}
		}
	case graphValueTypeURL:
		text, _ := value.(string)
		add(workspaceExportCellPart{Text: text, Href: workspaceExportSafeLink(text), LinkClass: "url-value"})
	case graphValueTypeEmail, graphValueTypePhone:
		text, _ := value.(string)
		add(linkedValuePart(text))
	case graphValueTypeNumber:
		if number, ok := value.(float64); ok {
			add(workspaceExportCellPart{Text: strconv.FormatFloat(number, 'f', -1, 64)})
		}
	case graphValueTypeUniqueID:
		if number, ok := value.(float64); ok {
			text := strconv.FormatFloat(number, 'f', -1, 64)
			if prefix := strings.TrimSpace(mapString(property.Config, "idPrefix")); prefix != "" {
				text = prefix + "-" + text
			}
			add(workspaceExportCellPart{Text: text})
		}
	case graphValueTypeFormula:
		if computed, ok := document.computedValues(row)[property.ID]; ok {
			part := document.formulaCellPart(computed.Value)
			// A number that is not finite is written as Notion's export writes
			// it (seen 2026-11-14: "Infinity", "-Infinity", "NaN").
			if _, ok := databaseComputedNonFinite(computed); ok && part.Text == "" {
				part.Text = computed.Text
			}
			// A value naming a mention its exporter can't open says so, as the
			// app's cell does (formula.ts, lineWithClosedMentions).
			part.Text = closedMentionsInLine(part.Text, document.Language)
			add(part)
		}
	case graphValueTypeRollup:
		if computed, ok := document.computedValues(row)[property.ID]; ok {
			rolled := document.rollupCell(property, computed)
			for _, part := range rolled.Parts {
				add(part)
			}
			cell.Plain = rolled.Plain
		}
	case graphValueTypeCreatedTime:
		add(document.timeCellPart(property, row.Created))
	case graphValueTypeLastEditedTime:
		add(document.timeCellPart(property, row.Updated))
	case graphValueTypeCreatedBy, graphValueTypeLastEditedBy:
		id := row.CreatedBy
		if property.Type == graphValueTypeLastEditedBy {
			id = row.LastEditedBy
		}
		if id != "" {
			add(workspaceExportCellPart{Text: document.personName(id), Class: "user"})
		}
	default:
		switch typed := value.(type) {
		case string:
			add(workspaceExportCellPart{Text: typed})
		case float64:
			add(workspaceExportCellPart{Text: strconv.FormatFloat(typed, 'f', -1, 64)})
		case bool:
			add(workspaceExportCellPart{Text: strconv.FormatBool(typed)})
		case map[string]any:
			add(workspaceExportCellPart{Text: workspaceExportDateText(typed)})
		}
	}
	return cell
}

// linkedValuePart is an email's or a phone's value: a link to the value as
// it is, marked an address's value, as Notion's export writes both (seen
// 2026-11-14: <a href="w1065@example.com" class="url-value">, <a
// href="010-1234-5678" class="url-value">) — words alone for one whose
// scheme no export links (workspaceExportSafeLink).
func linkedValuePart(text string) workspaceExportCellPart {
	href := strings.TrimSpace(text)
	if parsed, err := url.Parse(href); err != nil || parsed.Scheme != "" {
		href = workspaceExportSafeLink(href)
	}
	return workspaceExportCellPart{Text: text, Href: href, LinkClass: "url-value"}
}

// relatedPart is a related row's page as a value names it: its title, its
// spaces too, or the export's word for one without, as Notion's export names
// it (seen 2026-11-13: "제목 없음 (…),     (…)").
func (document *workspaceExportDocument) relatedPart(id string) (workspaceExportCellPart, bool) {
	related := document.RowByID[id]
	if related == nil {
		return workspaceExportCellPart{}, false
	}
	return workspaceExportCellPart{Text: workspaceExportTitle(related.Title, document.untitled()), RowID: id, Related: true}, true
}

func workspaceExportStrings(value any) []string {
	items, _ := value.([]any)
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok && text != "" {
			result = append(result, text)
		}
	}
	return result
}

func workspaceExportDateText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		start, end := mapString(typed, "start"), mapString(typed, "end")
		if end != "" && end != start {
			return start + " → " + end
		}
		if start == "" {
			return mapString(typed, "name")
		}
		return start
	}
	return ""
}

// cellMarkdown writes a value for a row page's Markdown, with a text value's
// formatting, links and mentions, and a related page with where it is.
func (renderer *workspaceExportRenderer) cellMarkdown(cell workspaceExportCell) string {
	if cell.Plain != "" {
		return cell.Plain
	}
	texts := make([]string, 0, len(cell.Parts))
	for _, part := range cell.Parts {
		if len(part.Runs) > 0 {
			texts = append(texts, renderer.runsMarkdown(part.Runs))
			continue
		}
		texts = append(texts, renderer.relatedText(part))
	}
	return strings.Join(texts, ", ")
}

// cellHTML writes a value with links to exported row pages and files. A
// person is their first letter's icon and their name, the people of a value
// side by side, as Notion's HTML export writes them (seen 2026-11-12: <span
// class="user"><span class="icon text-icon user-icon"><span
// class="user-icon-inner">〈첫 글자〉</span></span>〈이름〉</span>, a bot's and
// a created-by value's alike), and the linked pages of a relation or of a
// relation's rollup between ", " (seen 2026-11-12: <a>나</a>, <a>다, 라</a>),
// and a file value's files and a multi-select's options with nothing between
// (filePartHTML; seen 2026-11-14: <span class="selected-value …">둘</span><span
// class="selected-value …">셋</span>).
func (renderer *workspaceExportRenderer) cellHTML(cell workspaceExportCell) string {
	var builder strings.Builder
	for index, part := range cell.Parts {
		if index > 0 {
			before := cell.Parts[index-1]
			switch {
			case part.Related && before.Related:
				builder.WriteString(", ")
			case part.File && before.File:
			case workspaceExportSelectedValue(part) && workspaceExportSelectedValue(before):
			case part.Class != "user" || before.Class != "user":
				builder.WriteString(" ")
			}
		}
		builder.WriteString(renderer.cellPartHTML(part))
	}
	return builder.String()
}

// workspaceExportSelectedValue is an option of a select or a multi-select.
func workspaceExportSelectedValue(part workspaceExportCellPart) bool {
	return strings.HasPrefix(part.Class, "selected-value ")
}

// cellPartHTML writes one part of a value: its text with its formatting, a
// checkbox, or its words in their span, <time> and link.
func (renderer *workspaceExportRenderer) cellPartHTML(part workspaceExportCellPart) string {
	if len(part.Runs) > 0 {
		return renderer.runsHTML(part.Runs)
	}
	if part.File {
		return renderer.filePartHTML(part)
	}
	if part.Status != "" {
		return workspaceExportStatusHTML(part)
	}
	if part.Checkbox != "" {
		checked := ""
		if part.Checkbox == "on" {
			checked = ` checked=""`
		}
		return `<input type="checkbox" class="checkbox checkbox-` + part.Checkbox + `" disabled=""` + checked + "/>"
	}
	text := workspaceExportEscape(part.Text)
	if part.HTMLText != "" {
		text = workspaceExportEscape(part.HTMLText)
	}
	href, page := part.Href, ""
	if part.RowID != "" {
		href, page = renderer.link(part.RowID), part.RowID
	}
	if part.FileID != "" {
		href = renderer.fileHref(part.FileID, "")
	}
	if part.Class == "user" {
		text = `<span class="icon text-icon user-icon"><span class="user-icon-inner">` +
			workspaceExportEscape(workspaceExportFirstLetter(part.Text)) + "</span></span>" + text
	}
	if part.Class != "" {
		text = `<span class="` + workspaceExportEscape(strings.TrimSuffix(part.Class, "-")) + `">` + text + "</span>"
	}
	if part.DateTime != "" {
		text = `<time datetime="` + workspaceExportEscape(part.DateTime) + `">` + text + "</time>"
	}
	if href != "" {
		linkClass := ""
		if part.LinkClass != "" {
			linkClass = ` class="` + part.LinkClass + `"`
		}
		text = `<a href="` + workspaceExportEscape(href) + `"` + linkClass + workspaceExportPageIDAttr(page) + `>` + text + "</a>"
	}
	return text
}

// workspaceExportFirstLetter is a person's first letter, as the app's
// picture of a person without a photo shows it (PersonAvatar.tsx).
func workspaceExportFirstLetter(name string) string {
	for _, letter := range strings.TrimSpace(name) {
		return strings.ToUpper(string(letter))
	}
	return "?"
}

// orderedProperties puts the title property first, then the rest in their
// database order.
func (document *workspaceExportDocument) orderedProperties(dataSourceID string) []*workspaceExportDocProperty {
	ordered := []*workspaceExportDocProperty{}
	for _, property := range document.Properties[dataSourceID] {
		if property.Type == graphValueTypeTitle {
			ordered = append([]*workspaceExportDocProperty{property}, ordered...)
		} else {
			ordered = append(ordered, property)
		}
	}
	return ordered
}

// filePartHTML writes a file value's file as Notion's HTML export does (seen
// 2026-11-13, a database's cell and a row page's property alike): in a span
// with room after it, a link to where it is, a picture's holding its
// thumbnail and any other's its name — <span
// style="margin-inline-end:6px"><a href="…"><img
// style="width:20px;max-height:24px" src="…" alt="a.png"/></a></span> — one
// beside the next with nothing between; one the export leaves out is its
// name.
func (renderer *workspaceExportRenderer) filePartHTML(part workspaceExportCellPart) string {
	name := workspaceExportEscape(part.Text)
	inner := name
	if href := renderer.filePartHref(part); href != "" {
		escaped := workspaceExportEscape(href)
		if workspaceExportPictureName(part.Text) || part.FileID == "" && workspaceExportPictureName(part.Href) {
			inner = `<img style="width:20px;max-height:24px" src="` + escaped + `" alt="` + name + `"/>`
		}
		inner = `<a href="` + escaped + `">` + inner + "</a>"
	}
	return `<span style="margin-inline-end:6px">` + inner + "</span>"
}

// workspaceExportPictureName is whether a file's name, or an address's path,
// ends as a picture's does.
func workspaceExportPictureName(name string) bool {
	name, _, _ = strings.Cut(name, "?")
	name, _, _ = strings.Cut(name, "#")
	dot := strings.LastIndexByte(name, '.')
	if dot < 0 {
		return false
	}
	switch strings.ToLower(name[dot+1:]) {
	case "png", "jpg", "jpeg", "gif", "webp", "svg", "bmp", "heic", "heif", "avif", "tif", "tiff", "ico":
		return true
	}
	return false
}

// workspaceExportStatusColor is a status option's color as Notion's export
// names it: "default" for one drawn in its group's color, as a new status's
// first option is (notionStatusOptionColorFromGroup), or with none.
func workspaceExportStatusColor(property *workspaceExportDocProperty, id, color string) string {
	options, _ := property.Config["options"].([]any)
	for _, raw := range options {
		option, _ := raw.(map[string]any)
		if fromGroup, _ := option["colorFromGroup"].(bool); fromGroup && mapString(option, "id") == id {
			return "default"
		}
	}
	if color == "" {
		return "default"
	}
	return color
}

// workspaceExportStatusHTML writes a status value as Notion's HTML export
// does, a database's cell and a row page's property alike (seen
// 2026-11-13): its name after a dot, in its color's class unless it is
// "default" — <span class="status-value select-value-color-blue"><div
// class="status-dot status-dot-color-blue"></div>In progress</span>, <span
// class="status-value"><div class="status-dot"></div>Not started</span>.
// Neither class has a style of its own there.
func workspaceExportStatusHTML(part workspaceExportCellPart) string {
	value, dot := "status-value", "status-dot"
	if part.Status != "default" {
		value += " select-value-color-" + part.Status
		dot += " status-dot-color-" + part.Status
	}
	return `<span class="` + workspaceExportEscape(value) + `"><div class="` + workspaceExportEscape(dot) + `"></div>` +
		workspaceExportEscape(part.Text) + "</span>"
}
