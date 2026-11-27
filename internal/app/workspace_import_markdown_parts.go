package app

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/yuin/goldmark/text"
)

// A long Markdown file is read a part at a time (2026-10-14: read whole, a
// 20 MiB file took the server's memory from 0.2 to 2 GB before its import
// began): each part ends where a block starts at the left margin after a
// blank line outside fenced code, so no block is cut, and the blocks are
// staged as each part is read, nested as they would be read whole. Every
// part is read with the file's link reference definitions, which may stand
// anywhere in it.

// workspaceImportMarkdownPartBytes is about how much Markdown a part holds.
const workspaceImportMarkdownPartBytes = 512 << 10

var workspaceImportMarkdownDefinitionPattern = regexp.MustCompile(`^ {0,3}\[[^\]]+\]:[ \t]*\S`)

func parseWorkspaceImportMarkdown(markdown string) ([]workspaceImportStagedItem, error) {
	items := []workspaceImportStagedItem{}
	err := workspaceImportMarkdownSource(markdown)(func(item workspaceImportStagedItem) error {
		items = append(items, item)
		return nil
	})
	return items, err
}

// workspaceImportMarkdownSource stages a Markdown file's blocks a part at a
// time.
func workspaceImportMarkdownSource(markdown string) workspaceImportItemSource {
	return workspaceImportTitledMarkdownSource(markdown, "", "")
}

// workspaceImportTitledMarkdownSource stages the Markdown of a page whose
// file names it so: its own sub-pages it names inline hang on their fields,
// a first heading naming them being its title
// (workspace_import_field_pages.go). What the import names, it names in the
// import's language. An inline database's data source, properties and view
// go right before its block, its rows right after.
func workspaceImportTitledMarkdownSource(markdown, title, locale string) workspaceImportItemSource {
	return func(emit func(workspaceImportStagedItem) error) error {
		normalized := strings.ReplaceAll(strings.ReplaceAll(markdown, "\r\n", "\n"), "\r", "\n")
		normalized, head, headed := workspaceImportPageHeadOf(normalized)
		if headed {
			if err := emit(workspaceImportStagedItem{Head: &head}); err != nil {
				return err
			}
		}
		parts, definitions := workspaceImportMarkdownParts(workspaceImportMarkdownParagraphLines(workspaceImportMarkdownCodeSpanLines(normalized)))
		builder := newWorkspaceImportItemBuilder()
		placed, inline := map[string]bool{}, map[string]bool{}
		fields := newWorkspaceImportFieldPages(normalized, title)
		for _, part := range parts {
			if definitions != "" {
				part += "\n\n" + definitions
			}
			source := []byte(part)
			reader := workspaceImportMarkdownReader{source: source, locale: locale, placed: placed, named: fields.inline, inlineDatabases: inline}
			if !reader.children(workspaceImportMarkdown.Parser().Parse(text.NewReader(source)), 0) {
				return workspaceImportInputError()
			}
			for index, block := range reader.list.blocks {
				if builder.count == 0 && index == 0 {
					if heading, titled := fields.titleHeading(block); titled {
						if err := emit(heading); err != nil {
							return err
						}
						continue
					}
				}
				block.Anchors = fields.anchors(block)
				if err := emitWorkspaceImportMarkdownBlock(emit, builder, block); err != nil {
					return err
				}
				if block.Type == graphBlockTypeChildPage && inline[block.ID] {
					if err := emit(workspaceImportStagedItem{InlineDatabase: block.ID}); err != nil {
						return err
					}
				}
			}
		}
		if builder.count == 0 {
			return emit(builder.item(workspaceImportParsedBlock{Type: graphBlockTypeParagraph, Content: json.RawMessage(`{}`)}))
		}
		return nil
	}
}

func emitWorkspaceImportMarkdownBlock(emit func(workspaceImportStagedItem) error, builder *workspaceImportItemBuilder, block workspaceImportParsedBlock) error {
	if block.Database == nil {
		return emit(builder.item(block))
	}
	for _, operation := range block.Database.setup {
		if err := emit(stagedWorkspaceImportOperation(operation)); err != nil {
			return err
		}
	}
	if err := emit(builder.item(block)); err != nil {
		return err
	}
	for _, operation := range block.Database.rows {
		if err := emit(stagedWorkspaceImportOperation(operation)); err != nil {
			return err
		}
	}
	return nil
}

var (
	workspaceImportMarkdownHeadingOrQuote = regexp.MustCompile(`^ {0,3}(#{1,6}(?:[ \t]|$)|>)`)
	workspaceImportMarkdownListMarker     = regexp.MustCompile(`^ {0,3}(?:[-*+]|\d{1,9}[.)])(?:[ \t]|$)`)
	workspaceImportMarkdownQuoteMarker    = regexp.MustCompile(`^ {0,3}>[ \t]?`)
	workspaceImportMarkdownOwnBlock       = regexp.MustCompile(`^ {0,3}(?:\||<|(?:-[ \t]*){3,}$|(?:\*[ \t]*){3,}$|(?:_[ \t]*){3,}$)`)
)

// workspaceImportMarkdownParagraphLines keeps a line that starts with "#" or
// ">" right under a paragraph's or a list item's line as a line of it, and a
// quote's line that starts with "#" right under its paragraph's line, as
// Notion's import reads Markdown (seen 2026-11-05: only a blank line before
// one starts a heading or a quote; a list item still starts after text;
// 2026-11-13: "> a" then "> # b" is a quote "a" and "# b"). A list item
// opening with a heading, a quote, code or a rule has no such line of words
// (2026-11-13: the second line of "- > a" as "> b" became words of the
// quote, and a "#" line of code in "- ```" was escaped).
func workspaceImportMarkdownParagraphLines(markdown string) string {
	lines := strings.Split(markdown, "\n")
	fence, fenceAt, inParagraph := "", 0, false
	quoteFence, inQuote := "", false
	for index, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		lead := len(line) - len(trimmed)
		indented := lead > 3
		quoted := !indented && strings.HasPrefix(trimmed, ">")
		if !quoted {
			quoteFence, inQuote = "", false
		}
		if fence != "" {
			if lead <= fenceAt+3 && strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, fence[:1]) == "" {
				fence = ""
				continue
			}
			if lead >= fenceAt || strings.TrimSpace(line) == "" {
				continue
			}
			// A line left of the code ends it, and the list item holding it.
			fence = ""
		}
		switch {
		case !indented && workspaceImportMarkdownFence(trimmed) != "":
			fence, fenceAt, inParagraph = workspaceImportMarkdownFence(trimmed), lead, false
		case strings.TrimSpace(line) == "":
			inParagraph = false
		case inParagraph && workspaceImportMarkdownHeadingOrQuote.MatchString(line):
			lines[index] = line[:lead] + "\\" + trimmed
		case quoted:
			inParagraph = false
			words := line
			for marker := workspaceImportMarkdownQuoteMarker.FindString(words); marker != ""; marker = workspaceImportMarkdownQuoteMarker.FindString(words) {
				words = words[len(marker):]
			}
			switch text := strings.TrimLeft(words, " "); {
			case quoteFence != "":
				if strings.HasPrefix(text, quoteFence) && strings.Trim(text, quoteFence[:1]) == "" {
					quoteFence = ""
				}
			case inQuote && workspaceImportMarkdownHeadingOrQuote.MatchString(words):
				lines[index] = line[:len(line)-len(text)] + "\\" + text
			default:
				quoteFence, inQuote = workspaceImportMarkdownLineWords(words)
			}
		case workspaceImportMarkdownListMarker.MatchString(line):
			fence, inParagraph = workspaceImportMarkdownLineWords(line)
			fenceAt = len(line) - len(workspaceImportMarkdownItemWords(line))
		case workspaceImportMarkdownHeadingOrQuote.MatchString(line), workspaceImportMarkdownOwnBlock.MatchString(line):
			inParagraph = false
		case !indented:
			inParagraph = true
		}
	}
	return strings.Join(lines, "\n")
}

// workspaceImportMarkdownLineWords reads what a line holds after a list
// item's markers: the fence of code it opens, or whether it is words that
// lines under it go on.
func workspaceImportMarkdownLineWords(line string) (string, bool) {
	words := workspaceImportMarkdownItemWords(line)
	if fence := workspaceImportMarkdownFence(strings.TrimLeft(words, " ")); fence != "" {
		return fence, false
	}
	return "", strings.TrimSpace(words) != "" &&
		!workspaceImportMarkdownHeadingOrQuote.MatchString(words) && !workspaceImportMarkdownOwnBlock.MatchString(words)
}

// workspaceImportMarkdownItemWords is what a line holds after its list
// markers ("- 1. a" holds "a").
func workspaceImportMarkdownItemWords(line string) string {
	words := line
	for marker := workspaceImportMarkdownListMarker.FindString(words); marker != ""; marker = workspaceImportMarkdownListMarker.FindString(words) {
		words = strings.TrimLeft(words[len(marker):], " \t")
	}
	return words
}

// workspaceImportMarkdownFence is the fence a line opens code with, none for
// a line that opens none.
func workspaceImportMarkdownFence(trimmed string) string {
	if !strings.HasPrefix(trimmed, "```") && !strings.HasPrefix(trimmed, "~~~") {
		return ""
	}
	return strings.Repeat(trimmed[:1], len(trimmed)-len(strings.TrimLeft(trimmed, trimmed[:1])))
}

// workspaceImportMarkdownParts cuts Markdown into parts, and gathers its link
// reference definitions.
func workspaceImportMarkdownParts(markdown string) ([]string, string) {
	if len(markdown) <= workspaceImportMarkdownPartBytes {
		return []string{markdown}, ""
	}
	parts, definitions := []string{}, []string{}
	start, size, fence, blank := 0, 0, "", false
	for offset := 0; offset < len(markdown); {
		end := strings.IndexByte(markdown[offset:], '\n')
		if end < 0 {
			end = len(markdown)
		} else {
			end += offset + 1
		}
		line := strings.TrimRight(markdown[offset:end], "\n")
		trimmed := strings.TrimLeft(line, " ")
		indented := len(line)-len(trimmed) > 3
		// A caption line stays with the block it is the caption of.
		if fence == "" && blank && size >= workspaceImportMarkdownPartBytes && line != "" && line[0] != ' ' && line[0] != '\t' &&
			!strings.HasPrefix(line, "[]("+workspaceImportCaptionMark) {
			parts = append(parts, markdown[start:offset])
			start, size = offset, 0
		}
		switch {
		case fence != "":
			if !indented && strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, fence[:1]) == "" {
				fence = ""
			}
		case !indented && (strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")):
			marker := trimmed[:3]
			fence = marker[:1] + strings.Repeat(marker[:1], len(trimmed)-len(strings.TrimLeft(trimmed, marker[:1]))-1)
		case workspaceImportMarkdownDefinitionPattern.MatchString(line):
			definitions = append(definitions, line)
		}
		blank = strings.TrimSpace(line) == ""
		size += end - offset
		offset = end
	}
	if start < len(markdown) {
		parts = append(parts, markdown[start:])
	}
	return parts, strings.Join(definitions, "\n")
}
