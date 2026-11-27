package app

import (
	"strings"
	"unicode/utf8"
)

// parse reads a line's inline Markdown into the parser's spans, with the
// style around it. Notion's "v2" and MCP Markdown read it left to right into
// pieces that stand whole and runs of emphasis marks, and pair the marks
// once the line is read (notion_markdown_version2_emphasis.go); "v1" and an
// imported file pair a mark as they come to it.
func (parser *workspaceImportInlineParser) parse(value string, style workspaceImportInlineStyle, depth int) {
	// Its spans are whole when it is done.
	defer parser.flush()
	var emphasis *notionMarkdownEmphasis
	if parser.notion && !parser.version1 {
		emphasis = &notionMarkdownEmphasis{}
	}
	literal := strings.Builder{}
	// The text read as written here is one node of a cell's text, whatever
	// is read between its parts (parser.cell).
	node := parser.read()
	flush := func() {
		if emphasis != nil {
			emphasis.words(literal.String())
		} else {
			parser.node = node
			parser.emitText(literal.String(), style)
		}
		literal.Reset()
	}
	// put writes a piece that stands whole with the style around it: at once,
	// or once the line's emphasis has paired.
	put := func(piece func(workspaceImportInlineStyle)) {
		flush()
		if emphasis != nil {
			emphasis.whole(piece)
			return
		}
		parser.read()
		piece(style)
	}
	for index := 0; index < len(value); {
		char := value[index]
		switch {
		case char == '\\' && parser.notion && index+1 < len(value) && (value[index+1] == ':' || value[index+1] == '.'):
			// An escaped colon or full stop parts the text around it, so an
			// address it stands in reads unlinked (notionMarkdownBareLinks).
			mark := value[index+1 : index+2]
			put(func(style workspaceImportInlineStyle) {
				if parser.barriers == nil {
					parser.barriers = map[int]bool{}
				}
				parser.barriers[parser.plain] = true
				parser.node = node
				parser.emit(mark, style)
			})
			index += 2
			continue
		case char == '\\' && parser.version1 && index+1 < len(value):
			// "v1" takes a backslash before any character as that character,
			// a space or a letter too (seen 2026-11-11: "\ a" is " a",
			// "\a" is "a").
			_, size := utf8.DecodeRuneInString(value[index+1:])
			literal.WriteString(value[index+1 : index+1+size])
			index += 1 + size
			continue
		case char == '\\' && index+1 < len(value) && strings.IndexByte(notionMarkdownLeafenEscapes, value[index+1]) >= 0:
			literal.WriteByte(value[index+1])
			index += 2
			continue
		case char == '`' && emphasis != nil:
			code, end, ok := notionMarkdownCodeSpanAt(value, index)
			if !ok {
				// A run of backticks nothing closes is its backticks.
				literal.WriteString(value[index:end])
				index = end
				continue
			}
			put(func(style workspaceImportInlineStyle) {
				style.code = true
				parser.emit(code, style)
			})
			index = end
			continue
		case char == '`':
			// "v1" reads two backticks together as code of nothing, which goes
			// from a paragraph's text, and a backslash before a backtick in
			// code as Leafen writes it (notion_markdown_version1_code_spans.go).
			if code, end, ok := workspaceImportCodeSpan(value, index, parser.version1); ok {
				put(func(style workspaceImportInlineStyle) {
					// A cell keeps it as an empty piece of no style (seen
					// 2026-11-11: "*a `` b*" is an italic "a ", "" and an
					// italic " b").
					if code == "" && parser.cell {
						parser.add(graphTextSpan{})
						return
					}
					style.code = true
					parser.emit(code, style)
				})
				index = end
				continue
			}
		case char == ':' && parser.notion && style.link == "":
			if end := strings.IndexByte(value[index+1:], ':'); end > 0 && notionMarkdownEmojiName(value[index+1:index+1+end]) {
				// The Markdown's writer resolves the name to a custom emoji,
				// or keeps it as text.
				name := value[index : index+end+2]
				put(func(style workspaceImportInlineStyle) {
					parser.add(graphTextSpan{
						Text: name, Mention: "custom_emoji", Bold: style.bold, Italic: style.italic, CommentID: parser.sitePages.discussionThreads(style.discussions),
						Strikethrough: style.strikethrough, Underline: style.underline, Color: style.color, Background: style.background,
					})
				})
				index += end + 2
				continue
			}
		case char == '$' && parser.notion:
			expression, end, ok := notionMarkdownVersion2Equation(value, index)
			if parser.version1 {
				expression, end, ok = notionMarkdownVersion1Equation(value, index)
			}
			if !ok {
				break
			}
			if !parser.version1 {
				expression = notionMarkdownEquationBreaks(expression)
			}
			index = end
			// "v1" writes it between backticks too, which its reading takes off
			// when no space, tab or line break ends what they hold (seen
			// 2026-11-10 and 2026-11-14: "$`x`$" is "x", "$``$" nothing,
			// "$` x`$" " x", "$`<br>b`$" "⏎b", "$` x `$" and "$`a<br>`$" as
			// written); "v2" keeps them.
			if inner := strings.TrimPrefix(strings.TrimSuffix(expression, "`"), "`"); parser.version1 &&
				len(expression) >= 2 && len(inner) == len(expression)-2 && inner == strings.TrimRight(inner, " \t\n") {
				expression = inner
			}
			// An equation of nothing is an empty one, of no marks (seen
			// 2026-11-11: "**a $$ b**" is a bold "a ", an equation "" and
			// a bold " b"; graph_text_empty_pieces.go).
			if expression == "" {
				put(func(workspaceImportInlineStyle) {
					parser.add(graphTextSpan{EmptyPiece: graphTextEmptyEquation})
				})
				continue
			}
			version1 := parser.version1
			put(func(style workspaceImportInlineStyle) {
				// "v1" marks no equation (seen 2026-11-10: "*$b$*" and
				// "<span color=\"red\">$a$</span>" an equation only).
				if version1 {
					style = workspaceImportInlineStyle{}
				}
				parser.add(graphTextSpan{
					Text: expression, Equation: expression, Bold: style.bold, Italic: style.italic, CommentID: parser.sitePages.discussionThreads(style.discussions),
					Strikethrough: style.strikethrough, Underline: style.underline, Color: style.color, Background: style.background,
				})
			})
			continue
		case char == '<' && parser.notion && depth < 8:
			if inner, textStart, ok := notionMarkdownVersion1SpanOpening(value, index, style); ok && parser.version1 {
				// A "v1" <span> closes at the first </span> after it, or marks
				// the rest of its text (seen 2026-11-10: "*a <span
				// color=\"red\">b* c</span>" a red "b" in the italic,
				// "</span>" words after it).
				textEnd, next := len(value), len(value)
				if closing := strings.Index(value[textStart:], "</span>"); closing >= 0 {
					textEnd, next = textStart+closing, textStart+closing+len("</span>")
				}
				flush()
				parser.parse(value[textStart:textEnd], inner, depth+1)
				index = next
				continue
			}
			if next, ok := parser.tag(value, index, depth, put); ok {
				index = next
				continue
			}
			if next, ok := parser.autolink(value, index, put); ok {
				index = next
				continue
			}
		case char == '<':
			if next, ok := parser.autolink(value, index, put); ok {
				index = next
				continue
			}
		case char == '[' && depth < 8:
			if next, ok := parser.link(value, index, depth, put); ok {
				index = next
				continue
			}
		case parser.version1 && (char == '*' || char == '_' || char == '~') && depth < 8:
			inner, textStart, textEnd, next, ok := notionMarkdownVersion1Emphasis(value, index, style)
			if ok {
				flush()
				parser.parse(value[textStart:textEnd], inner, depth+1)
			} else {
				// Marks that pair with nothing are text.
				literal.WriteString(value[index:next])
			}
			index = next
			continue
		case emphasis != nil && (char == '*' || char == '_' || char == '~'):
			flush()
			index = emphasis.run(value, index)
			continue
		case char == '_' && !workspaceImportUnderscoreOpens(value, index):
			// A run of underscores that can't open emphasis — inside a word or
			// before a space — is text, all of it, as CommonMark's.
			run := index + len(value[index:]) - len(strings.TrimLeft(value[index:], "_"))
			literal.WriteString(value[index:run])
			index = run
			continue
		case (char == '*' || char == '_' || char == '~') && depth < 8:
			if next, ok := parser.importEmphasis(value, index, style, depth, flush); ok {
				index = next
				continue
			}
		}
		literal.WriteByte(char)
		index++
	}
	flush()
	if emphasis != nil {
		emphasis.write(parser, style)
	}
	if depth == 0 && parser.notion {
		parser.flush()
		parser.linkBare()
	}
}

// tag reads a Notion tag opening at value[index:] — a missing custom emoji,
// a mark, a mention, a date, a link preview or a <span> — and reports where
// reading goes on.
func (parser *workspaceImportInlineParser) tag(
	value string, index, depth int, put func(func(workspaceImportInlineStyle)),
) (int, bool) {
	if mention, end, ok := notionMarkdownUnknownMention(value, index); ok {
		mention = parser.sitePages.unknownMention(mention)
		put(func(style workspaceImportInlineStyle) {
			switch mention.Mention {
			case "":
				parser.emit(mention.Text, style)
			case "custom_emoji":
				mention.Bold, mention.Italic, mention.Strikethrough = style.bold, style.italic, style.strikethrough
				mention.Underline, mention.Color, mention.Background, mention.CommentID = style.underline, style.color, style.background, parser.sitePages.discussionThreads(style.discussions)
				parser.add(mention)
			default:
				parser.add(mention)
			}
		})
		return end, true
	}
	if _, _, textStart, textEnd, closed, ok := notionMarkdownStyleTag(value, index, workspaceImportInlineStyle{}); ok {
		text := value[textStart:textEnd]
		put(func(style workspaceImportInlineStyle) {
			code, inner, _, _, _, _ := notionMarkdownStyleTag(value, index, style)
			if code {
				// "v2" writes a </code> inside code as <\\/code> (notionMarkdownVersion2CodeClose).
				if !parser.version1 {
					text = strings.ReplaceAll(text, notionMarkdownVersion2CodeClose, "</code>")
				}
				// Each <code> is a piece of its own (workspaceImportInlineParser.cuts).
				parser.read()
				parser.emit(text, inner)
			} else {
				parser.parse(text, inner, depth+1)
			}
		})
		return closed, true
	}
	mention, end, ok := notionMarkdownCurrentTag(value, index, parser.sitePages)
	if !ok {
		mention, end, ok = notionMarkdownMention(value, index, parser.version1, parser.sitePages)
		// "v1" reads a tag whose address names nothing as words.
		if parser.version1 && (!ok || mention.Mention == "page" || mention.Mention == "database" || mention.Mention == "person") {
			if words, wordsEnd, read := parser.version1MentionWords(value, index); read {
				mention, end, ok = words, wordsEnd, true
			}
		}
		// "v1" reads a date only in a tag closed in itself; one holding
		// words, even none, is text (seen 2026-11-11).
		if ok && parser.version1 && mention.Mention == "date" && !strings.HasSuffix(value[index:end], "/>") {
			ok = false
		}
	}
	if ok {
		put(func(style workspaceImportInlineStyle) {
			// A mention is marked as the text around it, and a tag that
			// stands for text or a link is that text (seen 2026-11-10).
			if linked := style; mention.Mention == "" {
				// "v1" reads its words as a piece of their own, or an empty
				// one (tagPieces).
				if parser.version1 && (mention.Text != "" || mention.EmptyPiece != "") {
					parser.tagPieces = append(parser.tagPieces, parser.plain)
					defer func() { parser.tagPieces = append(parser.tagPieces, parser.plain) }()
				}
				if mention.EmptyPiece != "" {
					return
				}
				if mention.Link != "" {
					linked.link = mention.Link
				}
				// Its text links no address it holds (linkBare).
				if parser.barriers == nil {
					parser.barriers = map[int]bool{}
				}
				for at := range len(mention.Text) {
					parser.barriers[parser.plain+at] = true
				}
				parser.emit(mention.Text, linked)
				return
			}
			mention.Bold, mention.Italic, mention.Strikethrough = style.bold, style.italic, style.strikethrough
			mention.Underline, mention.Color, mention.Background, mention.CommentID = style.underline, style.color, style.background, parser.sitePages.discussionThreads(style.discussions)
			parser.add(mention)
		})
		return end, true
	}
	if _, textStart, textEnd, closed, ok := notionMarkdownSpan(value, index, workspaceImportInlineStyle{}); ok {
		text := value[textStart:textEnd]
		put(func(style workspaceImportInlineStyle) {
			inner, _, _, _, _ := notionMarkdownSpan(value, index, style)
			parser.parse(text, inner, depth+1)
		})
		return closed, true
	}
	// "v2" reads an HTML link as a mark over its text (notionMarkdownAnchor).
	if href, textStart, textEnd, closed, ok := notionMarkdownAnchor(value, index); ok && !parser.version1 {
		text := value[textStart:textEnd]
		put(func(style workspaceImportInlineStyle) {
			parser.anchor(href, text, style, depth)
		})
		return closed, true
	}
	return 0, false
}

// autolink reads an <https://…> address opening at value[index:], but angleAsText.
func (parser *workspaceImportInlineParser) autolink(value string, index int, put func(func(workspaceImportInlineStyle))) (int, bool) {
	end := strings.IndexByte(value[index:], '>')
	if end <= 1 || parser.sitePages.angleAsText() {
		return 0, false
	}
	// A data source's address is its mention, its brackets gone, as an
	// address standing bare is (notionMarkdownBareDataSource), but for a
	// comment's (angleDataSource); "v1" keeps them as text around it (seen 2026-11-11).
	if source := value[index+1 : index+end]; parser.notion && !parser.version1 {
		if link, ok := notionMarkdownBareDataSource(source, 0); ok && link.target != "" && link.end == len(source) {
			put(func(style workspaceImportInlineStyle) {
				parser.add(parser.sitePages.angleDataSource(graphTextSpan{
					Text: strings.TrimPrefix(source, notionMarkdownDataSourceAddress), Mention: "database", DatabaseID: source,
					Bold: style.bold, Italic: style.italic, Strikethrough: style.strikethrough, Underline: style.underline,
					Color: style.color, Background: style.background,
				}))
			})
			return index + end + 1, true
		}
	}
	target := workspaceImportLinkTarget(value[index+1 : index+end])
	if target == "" || strings.ContainsAny(target, " \t") {
		return 0, false
	}
	put(func(style workspaceImportInlineStyle) {
		// One of this server's pages is its mention, as an address standing
		// bare (notionMarkdownSitePages), but for a comment's (BareAsText).
		if mention, mentioned := parser.sitePages.mention(target, graphTextSpan{
			Bold: style.bold, Italic: style.italic, Strikethrough: style.strikethrough, Underline: style.underline,
			Color: style.color, Background: style.background,
		}); mentioned && !style.code {
			parser.add(mention)
			return
		}
		style.link = target
		// One of Notion's naming a page reads as written, linked as a write
		// keeps it (notionMarkdownWrittenAddress).
		if kept, named := notionMarkdownWrittenAddress(target); named && parser.notion {
			style.link = kept
		}
		parser.emit(target, style)
	})
	return index + end + 1, true
}

// link reads a [text](address) link, or a reference to a page imported with
// it, opening at value[index:].
func (parser *workspaceImportInlineParser) link(
	value string, index, depth int, put func(func(workspaceImportInlineStyle)),
) (int, bool) {
	closeText := strings.Index(value[index:], "](")
	if parser.notion && !parser.version1 {
		closeText = notionMarkdownLinkTextEnd(value, index) - index
	}
	if closeText <= 0 {
		return 0, false
	}
	text, rest := value[index+1:index+closeText], value[index+closeText+2:]
	address, used := "", -1
	switch {
	case parser.version1:
		// Its address is as written, tabs and angle brackets and all
		// (version1Link).
		if closeURL := notionMarkdownVersion1LinkClose(rest); closeURL >= 0 {
			address, used = rest[:closeURL], closeURL+1
		}
	default:
		if closeURL := workspaceImportLinkClose(rest, parser.notion, true); closeURL >= 0 {
			address, used = rest[:closeURL], closeURL+1
		}
	}
	// In Notion's "v2" and MCP Markdown an address between angle brackets
	// runs to its >, parentheses and all, as CommonMark's.
	if end := strings.IndexByte(rest, '>'); parser.notion && !parser.version1 && strings.HasPrefix(rest, "<") && end > 0 &&
		strings.HasPrefix(rest[end+1:], ")") && !strings.ContainsAny(rest[1:end], "<\n") {
		address, used = rest[1:end], end+2
	}
	if used <= 0 {
		return 0, false
	}
	next := index + closeText + 2 + used
	// A reference to what is imported with it (workspace_import_data_source_references.go).
	if mention, referenced := workspaceImportReference(address); referenced {
		put(func(style workspaceImportInlineStyle) {
			parser.add(workspaceImportReferenceSpan(mention, parser.referenceWords(text, depth), style))
		})
		return next, true
	}
	target, refused := parser.readLinkTarget(address)
	if refused != nil {
		put(func(workspaceImportInlineStyle) { parser.add(*refused) })
		return next, true
	}
	if target != "" {
		put(func(style workspaceImportInlineStyle) {
			style.link = target
			parser.parse(text, style, depth+1)
		})
		return next, true
	}
	// "v1" reads a link whose address makes none as its text (seen
	// 2026-11-11: "[a]()", "[a](javascript:x)" and "[a](https://exa mple.com)"
	// are "a"), and Notion's Markdown one of a host with a colon after it
	// (notionMarkdownHostPortAddress).
	if parser.version1 || parser.notion && notionMarkdownHostPortAddress(address) {
		put(func(style workspaceImportInlineStyle) {
			parser.parse(text, style, depth+1)
		})
		return next, true
	}
	return 0, false
}

// importEmphasis reads the emphasis a run of marks opening at value[index:]
// starts in an imported file, as CommonMark's: at the next run of its mark
// that closes it (workspaceImportClosing).
func (parser *workspaceImportInlineParser) importEmphasis(
	value string, index int, style workspaceImportInlineStyle, depth int, flush func(),
) (int, bool) {
	char := value[index]
	delimiter := string(char)
	if index+1 < len(value) && value[index+1] == char {
		delimiter += string(char)
	}
	if char == '~' && len(delimiter) == 1 {
		return 0, false
	}
	// Three of a mark are bold and italic at once, as the public API's
	// Markdown writes them: ***text***.
	if triple := strings.Repeat(delimiter[:1], 3); char != '~' && strings.HasPrefix(value[index:], triple) &&
		!workspaceImportSpaceAt(value, index+3) {
		if end := workspaceImportClosing(value, index, triple); end > 0 {
			flush()
			inner := style
			inner.bold, inner.italic = true, true
			parser.parse(value[index+3:end], inner, depth+1)
			return end + 3, true
		}
	}
	opensWord := !workspaceImportSpaceAt(value, index+len(delimiter))
	if char == '_' && workspaceImportWordByte(value, index, true) {
		opensWord = false
	}
	if !opensWord {
		return 0, false
	}
	end := workspaceImportClosing(value, index, delimiter)
	if end <= 0 {
		return 0, false
	}
	flush()
	inner := style
	switch {
	case char == '~':
		inner.strikethrough = true
	case len(delimiter) == 2:
		inner.bold = true
	default:
		inner.italic = true
	}
	parser.parse(value[index+len(delimiter):end], inner, depth+1)
	return end + len(delimiter), true
}
