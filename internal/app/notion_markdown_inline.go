package app

import (
	"html"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Notion's enhanced Markdown adds to the Markdown a page's writer reads:
// <mention-…> tags, <span underline="true" color="…">, $equations$ and
// :name: custom emoji. The inline parser reads these in its Notion mode, and
// emphasis as CommonMark's, as Notion's "v2" and MCP Markdown read it
// (notion_markdown_version2_emphasis.go); "v1" pairs emphasis its own way
// (notionMarkdownVersion1Emphasis).

// workspaceImportFlanks tells whether the run of a mark at value[index:run]
// may open and close emphasis, as CommonMark's left- and right-flanking
// runs, for _ only where no letter or digit stands on the other side.
func workspaceImportFlanks(value string, index, run int) (opens, closes bool) {
	before, after := ' ', ' '
	if index > 0 {
		before, _ = utf8.DecodeLastRuneInString(value[:index])
	}
	if run < len(value) {
		after, _ = utf8.DecodeRuneInString(value[run:])
	}
	punctuation := func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSymbol(r) }
	left := !unicode.IsSpace(after) && (!punctuation(after) || unicode.IsSpace(before) || punctuation(before))
	right := !unicode.IsSpace(before) && (!punctuation(before) || unicode.IsSpace(after) || punctuation(after))
	if value[index] == '_' {
		return left && (!right || punctuation(before)), right && (!left || punctuation(after))
	}
	return left, right
}

var notionMarkdownMentionPattern = regexp.MustCompile(
	`^<mention-(page|database|data-source|user|date)((?:\s+[A-Za-z][A-Za-z-]*="[^"]*")*)\s*(/>|>)`)

// notionMarkdownMention reads a <mention-page>, <mention-database>,
// <mention-data-source>, <mention-user> or <mention-date> tag opening at
// value[start:] into a mention span naming its target by the ID its address
// ends in, as written; the Markdown's writer resolves the ID. "v2", the MCP
// and Leafen's own Markdown read such a tag as the <mention> it stands for,
// whatever kind it names, its address deciding what it mentions
// (notionMarkdownAddressMention: seen 2026-11-11, <mention-page
// url="user://…"/> a person, <mention-user url="https://…">x</mention-user>
// a link), a <mention-database> one of type="database"
// (notionMarkdownMentionTypeClash). "v1" reads a page's or a database's tag
// by its address too — a page's, a database's or a data source's, which
// Notion's own reading tells apart — and one of a person's address, and a
// data source's or a person's tag of any address but theirs, as its words
// (version1MentionWords). A data source is mentioned as its database, named
// by its collection:// address until the writer finds the database
// (notion_markdown_data_source_mentions.go). It returns where the tag ends.
func notionMarkdownMention(value string, start int, version1 bool, site *notionMarkdownSitePages) (graphTextSpan, int, bool) {
	match := notionMarkdownMentionPattern.FindStringSubmatch(value[start:])
	if match == nil {
		return graphTextSpan{}, 0, false
	}
	end := start + len(match[0])
	text := ""
	if match[3] == ">" {
		closing := strings.Index(value[end:], "</mention-"+match[1]+">")
		if closing < 0 {
			return graphTextSpan{}, 0, false
		}
		text = unescapeNotionMarkdown(value[end : end+closing])
		end += closing + len("</mention-"+match[1]+">")
	}
	attributes := map[string]string{}
	for _, attribute := range notionMarkdownAttributePattern.FindAllStringSubmatch(match[2], -1) {
		attributes[attribute[1]] = strings.NewReplacer(`&quot;`, `"`, `&lt;`, `<`, `&gt;`, `>`, `&amp;`, `&`).Replace(attribute[2])
		if match[1] == "date" {
			// "v1" reads a date's every character reference (seen
			// 2026-11-11: "2026&#45;01&#45;02" is 2026-01-02); "v2"'s were
			// put so that this reads them as its reading does
			// (notion_page_markdown_version2_attributes.go).
			attributes[attribute[1]] = html.UnescapeString(attribute[2])
		}
	}
	if match[1] == "date" {
		span := graphTextSpan{Text: text, Mention: "date"}
		var named bool
		span.Date, span.DateEnd, span.DateTimeZone, named = notionMarkdownDateSpan(attributes)
		// "v1" reads a tag that closes itself, with attributes but no start,
		// as a date mention whose start was never given (seen 2026-11-11:
		// <mention-date url="x"/> reads back start="undefined", in "v2"
		// <date />, its date in the API without a start); its words are a
		// date mention's with none.
		if version1 && !named && match[3] == "/>" && len(attributes) > 0 {
			span.DateStartUnset, named = true, true
			span.Text = notionMarkdownDateText("", "")
		}
		if span.Text == "" {
			span.Text = notionMarkdownDateText(span.Date, span.DateEnd)
		}
		return span, end, named
	}
	address := strings.TrimSpace(attributes["url"])
	collection := strings.HasPrefix(address, notionMarkdownDataSourceAddress)
	if !version1 {
		span, ok := notionMarkdownAddressMention(address, text, match[1] == "database" && !collection, site)
		return span, end, ok
	}
	switch {
	case collection:
		span, ok := notionMarkdownAddressMention(address, text, false, site)
		return span, end, ok
	case match[1] == "user":
		if !strings.HasPrefix(attributes["url"], "user://") {
			return graphTextSpan{}, 0, false
		}
		id := notionMarkdownAddressID(attributes["url"])
		return graphTextSpan{Text: text, Mention: "person", UserID: id}, end, id != ""
	case match[1] == "data-source", strings.HasPrefix(attributes["url"], "user://"):
		return graphTextSpan{}, 0, false
	}
	if link, linked := notionMarkdownMentionLink(attributes["url"], text, site); linked {
		return link, end, true
	}
	// "v1" reads one of a mail address or a phone number as its text, or
	// its address when it holds none, and one of a host without a scheme
	// as a link there, its words the address with https before it when
	// the tag holds none (seen 2026-11-10 and 2026-11-11:
	// "example.com/path" reads "https://example.com/path").
	if scheme, _, _ := strings.Cut(attributes["url"], ":"); scheme == "mailto" || scheme == "tel" {
		if text == "" {
			text = address
		}
		return graphTextSpan{Text: text}, end, true
	}
	if target := notionMarkdownSchemelessLinkTarget(attributes["url"]); target != "" &&
		!strings.HasPrefix(target, "/") && !strings.HasPrefix(target, "#") {
		if text = strings.TrimSpace(text); text == "" {
			text = "https://" + strings.TrimPrefix(strings.TrimSpace(attributes["url"]), "//")
		}
		return graphTextSpan{Text: text, Link: target}, end, true
	}
	// One of a name with dots Notion's Markdown doesn't link as a host
	// is its words, as written when the tag holds none (seen 2026-11-11:
	// "relative.html", "a.zip").
	if !strings.Contains(address, ":") && notionMarkdownBareHostPattern.MatchString(notionMarkdownSchemelessHost(address)) {
		if text = strings.TrimSpace(text); text == "" {
			text = address
		}
		return graphTextSpan{Text: text}, end, true
	}
	// A page's or a database's address names either: the writer tells which,
	// keeping a Notion web address for it (notion_address_mentions_here.go).
	span := graphTextSpan{Text: text, Link: notionMarkdownNotionWebAddress(address)}
	if match[1] == "database" {
		span.Mention, span.DatabaseID = "database", notionMarkdownAddressID(attributes["url"])
	} else {
		// A block's address names its page at the block (page_mention_block_anchor.go).
		span.Mention, span.PageID = "page", notionMarkdownPageMentionID(attributes["url"])
	}
	if span.Text == "" {
		// A tag without text keeps its place until the writer gives it
		// its target's name.
		span.Text = span.PageID + span.DatabaseID
	}
	return span, end, span.PageID+span.DatabaseID != ""
}

// notionMarkdownAddressID is the ID an address in Notion's enhanced
// Markdown ends in, as written: a page's, a database's or a user's.
func notionMarkdownAddressID(address string) string {
	// A person's names them before the email Notion writes after it.
	if id, user := notionMarkdownUserAddressID(address); user {
		return id
	}
	id := address
	if parsed, err := url.Parse(address); err == nil && parsed.Scheme != "user" {
		id = parsed.Path
	}
	id = strings.TrimPrefix(id, "user://")
	if index := strings.LastIndex(id, "/"); index >= 0 {
		id = id[index+1:]
	}
	return id
}

// notionMarkdownSpan reads a <span underline="true" color="…"> tag opening at
// value[start:]: the style inside it, and where its text starts and ends
// and the tag closes.
func notionMarkdownSpan(value string, start int, style workspaceImportInlineStyle) (workspaceImportInlineStyle, int, int, int, bool) {
	style, textStart, ok := notionMarkdownSpanOpening(value, start, style)
	if !ok {
		return style, 0, 0, 0, false
	}
	// The tag closes at its own </span>, past any spans inside it.
	depth := 1
	for index := textStart; index < len(value); index++ {
		switch {
		case value[index] == '\\':
			index++
		case strings.HasPrefix(value[index:], "<span"):
			depth++
		case strings.HasPrefix(value[index:], "</span>"):
			depth--
			if depth == 0 {
				return style, textStart, index, index + len("</span>"), true
			}
		}
	}
	return style, 0, 0, 0, false
}

// notionMarkdownSpanOpening reads the <span …> tag opening at value[start:]:
// the style inside it and where its text starts.
func notionMarkdownSpanOpening(value string, start int, style workspaceImportInlineStyle) (workspaceImportInlineStyle, int, bool) {
	return notionMarkdownSpanOpeningAs(value, start, style, false)
}

// notionMarkdownVersion1SpanOpening reads a "v1" <span …> opening, whose
// color Notion's "v1" drops when it isn't one, keeping the words (seen
// 2026-11-11: <span color="nope">a</span> is "a").
func notionMarkdownVersion1SpanOpening(value string, start int, style workspaceImportInlineStyle) (workspaceImportInlineStyle, int, bool) {
	return notionMarkdownSpanOpeningAs(value, start, style, true)
}

// notionMarkdownEmojiName reports whether a word between two colons reads as
// a custom emoji's :name:, as in Notion: lowercase or caseless letters,
// digits, _ and -, and not digits alone.
func notionMarkdownEmojiName(name string) bool {
	digits := true
	for _, character := range name {
		switch {
		case character >= '0' && character <= '9':
		case character == '_' || character == '-' || unicode.In(character, unicode.Ll, unicode.Lo):
			digits = false
		default:
			return false
		}
	}
	return !digits
}

// notionMarkdownEscapedText escapes text for Notion's enhanced Markdown,
// including the colons around a word that would read as a custom emoji and
// the underscores that would read as emphasis.
func notionMarkdownEscapedText(text string) string {
	return notionMarkdownEscapedUnderscores(notionMarkdownEscapedTextWith(text, notionMarkdownEscaped))
}

// notionMarkdownEscapedUnderscores escapes each run of underscores that may
// read as emphasis (workspaceImportUnderscoreOpens); one inside a word, as
// in snake_case, or between spaces stays as it is, as Notion writes it.
// Notion's "v1" writes them all bare and reads some back as emphasis.
func notionMarkdownEscapedUnderscores(text string) string {
	if !strings.Contains(text, "_") {
		return text
	}
	var out strings.Builder
	for index := 0; index < len(text); {
		if text[index] != '_' {
			out.WriteByte(text[index])
			index++
			continue
		}
		run := index + len(text[index:]) - len(strings.TrimLeft(text[index:], "_"))
		inWord := workspaceImportWordByte(text, index, true) && workspaceImportWordByte(text, run, false)
		if inWord || index > 0 && run < len(text) && workspaceImportSpaceAt(text, index-1) && workspaceImportSpaceAt(text, run) {
			out.WriteString(text[index:run])
		} else {
			out.WriteString(strings.ReplaceAll(text[index:run], "_", `\_`))
		}
		index = run
	}
	return out.String()
}

// notionMarkdownEscapedTextWith escapes text with a form's escapes, and the
// colons around a word that would read as a custom emoji.
func notionMarkdownEscapedTextWith(text string, escaped *strings.Replacer) string {
	var result strings.Builder
	written, previous, escapedColon := 0, -1, -1
	for index := 0; index < len(text); index++ {
		if text[index] != ':' {
			continue
		}
		if previous >= 0 && notionMarkdownEmojiName(text[previous+1:index]) {
			if previous != escapedColon {
				result.WriteString(escaped.Replace(text[written:previous]))
				result.WriteString(`\:`)
			}
			// The name between the colons is as written (seen 2026-11-11:
			// "\:smile_cat\:", where "x_y" is "x\_y").
			result.WriteString(text[previous+1 : index])
			result.WriteString(`\:`)
			written, escapedColon = index+1, index
		}
		previous = index
	}
	result.WriteString(escaped.Replace(text[written:]))
	return result.String()
}

// notionMarkdownInlineSpans reads a line of Notion's enhanced Markdown into
// its plain text and spans; spans are nil when the text carries no
// formatting.
func notionMarkdownInlineSpans(value string) (string, []graphTextSpan) {
	return notionMarkdownVersionInlineSpans(value, false)
}

// emitText puts text read as written into the line's spans; in Notion's
// Markdown the addresses it holds bare are linked once the line is read
// (linkBare).
func (parser *workspaceImportInlineParser) emitText(text string, style workspaceImportInlineStyle) {
	parser.emit(text, style)
}

// linkBare links the addresses the line's text holds bare, outside links,
// code and mentions, in each run of text of one style: Notion's "v2" finds
// them across the marks that leave the text one style, and ends them where
// its style changes (seen 2026-11-10: "_see example.com/_a_ x_" links
// "example.com/a", "see https://example.com/a**b**" only up to "a"). A page's
// address mentions it (notionMarkdownSitePages), a person's them; none BareAsText.
func (parser *workspaceImportInlineParser) linkBare() {
	spans := make([]graphTextSpan, 0, len(parser.spans))
	offset := 0
	for _, span := range parser.spans {
		start := offset
		offset += len(span.Text)
		// A cell's empty piece stays (workspaceImportInlineParser.cell).
		if span.Link != "" || span.Code || span.Mention != "" || span.Equation != "" || span.Text == "" || parser.sitePages.bareAsText() {
			spans = append(spans, span)
			continue
		}
		at := 0
		piece := func(end int, link string) {
			if end > at {
				part := span
				part.Text, part.Link = span.Text[at:end], link
				spans = append(spans, part)
				at = end
			}
		}
		// An escaped colon or full stop stands apart from the runs around it.
		run := 0
		for index := 0; index <= len(span.Text); index++ {
			if index < len(span.Text) && !parser.barriers[start+index] {
				continue
			}
			for _, link := range notionMarkdownBareLinks(span.Text[run:index]) {
				piece(run+link.start, "")
				// A person's address is their mention, which the writer names
				// (notionMarkdownBareUser).
				if id, user := notionMarkdownUserAddressID(link.target); user {
					person := span
					person.Text, person.Mention, person.UserID = id, "person", id
					spans, at = append(spans, person), run+link.end
					continue
				}
				// A data source's address is its mention, which the writer
				// makes its database's (notion_markdown_data_source_mentions.go).
				if source, collection := strings.CutPrefix(link.target, notionMarkdownDataSourceAddress); collection {
					mention := span
					mention.Text, mention.Mention, mention.DatabaseID = source, "database", link.target
					spans, at = append(spans, mention), run+link.end
					continue
				}
				// A view's address links the app's address of its ID, which
				// it reads as (notionMarkdownViewAddress).
				if words, view, viewed := parser.viewAddress(link.target); viewed {
					linked := span
					linked.Text, linked.Link = words, view
					spans, at = append(spans, linked), run+link.end
					continue
				}
				// One of this server's pages is its mention, and one of Notion's
				// a writer's mention of what its ID names (notionAddressMention).
				written := span
				written.Text = span.Text[run+link.start : run+link.end]
				if mention, mentioned := parser.sitePages.mention(link.target, written); mentioned {
					spans, at = append(spans, mention), run+link.end
					continue
				}
				// Else it is linked as a write keeps it (notionMarkdownWrittenAddress),
				// and for a writer that makes links mentions as written (LinksMentioned).
				target := link.target
				if kept, named := notionMarkdownWrittenAddress(target); named && !parser.sitePages.linksMentioned() {
					target = kept
				}
				piece(run+link.end, target)
			}
			run = index + 1
		}
		piece(len(span.Text), "")
	}
	parser.spans = spans
}

// notionMarkdownVersionInlineSpans reads a line as notionMarkdownInlineSpans
// does, its emphasis as the public API's "v1" reads it when version1 is set.
func notionMarkdownVersionInlineSpans(value string, version1 bool) (string, []graphTextSpan) {
	return notionMarkdownSiteInlineSpans(value, version1, nil)
}

// notionMarkdownSiteInlineSpans reads a line as
// notionMarkdownVersionInlineSpans does, the addresses of sitePages' pages
// standing bare in it as their mentions (notionMarkdownSitePages).
func notionMarkdownSiteInlineSpans(value string, version1 bool, sitePages *notionMarkdownSitePages) (string, []graphTextSpan) {
	return notionMarkdownParsedSpans(value, workspaceImportInlineParser{notion: true, version1: version1, sitePages: sitePages})
}

// notionMarkdownCellInlineSpans reads a table cell's text as
// notionMarkdownSiteInlineSpans does, a "v1" cell's links as Notion's "v1"
// reads them there (graphWrittenCellTextLink).
func notionMarkdownCellInlineSpans(value string, version1 bool, sitePages *notionMarkdownSitePages) (string, []graphTextSpan) {
	return notionMarkdownParsedSpans(value, workspaceImportInlineParser{
		notion: true, version1: version1, cell: version1, sitePages: sitePages,
	})
}

func notionMarkdownParsedSpans(value string, parser workspaceImportInlineParser) (string, []graphTextSpan) {
	plain, spans, _ := notionMarkdownParsedPieces(value, parser)
	return plain, spans
}

// notionMarkdownSitePieces reads a line as notionMarkdownSiteInlineSpans
// does, with where in its text the pieces Notion's reading keeps apart
// begin inside the spans' runs (workspaceImportInlineParser.cuts).
func notionMarkdownSitePieces(value string, version1 bool, sitePages *notionMarkdownSitePages) (string, []graphTextSpan, []int) {
	return notionMarkdownParsedPieces(value, workspaceImportInlineParser{notion: true, version1: version1, sitePages: sitePages})
}

func notionMarkdownParsedPieces(value string, parser workspaceImportInlineParser) (string, []graphTextSpan, []int) {
	parser.parse(value, workspaceImportInlineStyle{}, 0)
	plain := strings.Builder{}
	styled := false
	for _, span := range parser.spans {
		plain.WriteString(span.Text)
		styled = styled || span != graphTextSpan{Text: span.Text}
	}
	// A cell's pieces stand as read, plain ones too.
	styled = styled || parser.cell && len(parser.spans) > 1
	if !styled && len(parser.tagPieces) == 0 {
		return plain.String(), nil, nil
	}
	cuts := parser.cuts
	if !styled {
		cuts = nil
	}
	return plain.String(), parser.spans, notionMarkdownTagCuts(cuts, parser.tagPieces, plain.Len())
}

// notionMarkdownTagCuts are a line's cuts with its "v1" mention tags' pieces
// among them (workspaceImportInlineParser.tagPieces): each place once, and
// once more for each empty piece there.
func notionMarkdownTagCuts(cuts, pieces []int, length int) []int {
	if len(pieces) == 0 {
		return cuts
	}
	places := map[int]int{}
	for _, cut := range cuts {
		places[cut] = 1
	}
	for index := 0; index+1 < len(pieces); index += 2 {
		start, end := pieces[index], pieces[index+1]
		if start == end {
			places[start] = max(places[start], 1) + 1
			continue
		}
		places[start], places[end] = max(places[start], 1), max(places[end], 1)
	}
	all := make([]int, 0, len(places)+len(pieces))
	for place, count := range places {
		if place < 0 || place > length {
			continue
		}
		for range count {
			all = append(all, place)
		}
	}
	slices.Sort(all)
	return all
}

// Notion's page Markdown now marks text with <b>, <i>, <s>, <u> and <code>
// (seen 2026-10-29), mentions anything as <mention url="…"> — a person's
// address user://…, a database's with type="database" — and a date as
// <date start="…" start-time="…" time-zone="…" />, beside the forms read
// above.

var (
	notionMarkdownStyleTagPattern   = regexp.MustCompile(`^<(b|strong|i|em|s|del|u|code)>`)
	notionMarkdownCurrentTagPattern = regexp.MustCompile(`^<(mention|date|link-preview)((?:\s+[A-Za-z][A-Za-z-]*="[^"]*")*)\s*(/>|>)`)
)

// notionMarkdownStyleTag reads a <b>, <i>, <s>, <u> or <code> tag opening at
// value[start:]: whether it is code, the style inside it, where its text
// starts and ends and where the tag closes.
func notionMarkdownStyleTag(value string, start int, style workspaceImportInlineStyle) (bool, workspaceImportInlineStyle, int, int, int, bool) {
	match := notionMarkdownStyleTagPattern.FindStringSubmatch(value[start:])
	if match == nil {
		return false, style, 0, 0, 0, false
	}
	name := match[1]
	opening, closing := "<"+name+">", "</"+name+">"
	textStart := start + len(match[0])
	if name == "code" {
		// Code is its text as written.
		end := strings.Index(value[textStart:], closing)
		if end < 0 {
			return false, style, 0, 0, 0, false
		}
		style.code = true
		return true, style, textStart, textStart + end, textStart + end + len(closing), true
	}
	switch name {
	case "b", "strong":
		style.bold = true
	case "i", "em":
		style.italic = true
	case "s", "del":
		style.strikethrough = true
	case "u":
		style.underline = true
	}
	depth := 1
	for index := textStart; index < len(value); index++ {
		switch {
		case value[index] == '\\':
			index++
		case strings.HasPrefix(value[index:], opening):
			depth++
		case strings.HasPrefix(value[index:], closing):
			depth--
			if depth == 0 {
				return false, style, textStart, index, index + len(closing), true
			}
		}
	}
	return false, style, 0, 0, 0, false
}

// notionMarkdownCurrentTag reads a <mention url="…">, <date … /> or
// <link-preview url="…" /> tag opening at value[start:] into its span, as
// notionMarkdownMention reads Leafen's earlier tags; a mention of a web
// address that is no page's is a link (notionMarkdownMentionLink), and a
// link preview the text of its address.
func notionMarkdownCurrentTag(value string, start int, site *notionMarkdownSitePages) (graphTextSpan, int, bool) {
	match := notionMarkdownCurrentTagPattern.FindStringSubmatch(value[start:])
	if match == nil {
		return graphTextSpan{}, 0, false
	}
	end := start + len(match[0])
	text := ""
	if match[3] == ">" {
		closing := strings.Index(value[end:], "</"+match[1]+">")
		if closing < 0 {
			return graphTextSpan{}, 0, false
		}
		text = unescapeNotionMarkdown(value[end : end+closing])
		end += closing + len("</"+match[1]+">")
	}
	attributes := map[string]string{}
	for _, attribute := range notionMarkdownAttributePattern.FindAllStringSubmatch(match[2], -1) {
		attributes[attribute[1]] = strings.NewReplacer(`&quot;`, `"`, `&lt;`, `<`, `&gt;`, `>`, `&amp;`, `&`).Replace(attribute[2])
	}
	address := strings.TrimSpace(attributes["url"])
	switch match[1] {
	case "date":
		span := graphTextSpan{Mention: "date", Text: text}
		named := map[string]string{
			"end": attributes["end"], "startTime": attributes["start-time"],
			"endTime": attributes["end-time"], "timeZone": attributes["time-zone"],
		}
		if start, ok := attributes["start"]; ok {
			named["start"] = start
		}
		var ok bool
		span.Date, span.DateEnd, span.DateTimeZone, ok = notionMarkdownDateSpan(named)
		if span.Text == "" {
			span.Text = notionMarkdownDateText(span.Date, span.DateEnd)
		}
		return span, end, ok
	case "link-preview":
		// Its address is its text, as written and unlinked; one with a source
		// is that source, linked to it as a [text](address) link is (seen
		// 2026-11-10 and 2026-11-11, "v1" and "v2": a host without a scheme
		// an https address, an anchor or a path in the app as written, a page
		// of this server's a link), and a source Notion links as a relative
		// address that opens nothing is its text (an approved difference).
		if source := strings.TrimSpace(attributes["source"]); source != "" {
			return graphTextSpan{Text: attributes["source"], Link: notionMarkdownReadLinkTarget(source, false)}, end, true
		}
		return graphTextSpan{Text: attributes["url"]}, end, true
	}
	span, ok := notionMarkdownAddressMention(address, text, attributes["type"] == "database", site)
	return span, end, ok
}
