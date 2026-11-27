package app

import "strings"

// Notion's "v2" and MCP Markdown pair emphasis as CommonMark's "process
// emphasis" does (seen 2026-11-10, each line written with replace_content
// and read back as blocks): a line is read left to right into words, pieces
// that stand whole — code, an equation, a tag with what it holds, a link, a
// mention, an escape — and runs of *, _ and ~; then each run that may close,
// from the left, pairs with the nearest run of its mark before it that may
// open, as many marks as both have up to two, a run that both opens and
// closes pairing with one whose length makes a multiple of three with its
// own only when both are, and the runs between them left as words.
// "***a** b*" is a bold italic "a" and an italic " b", "*a **b* c**" an
// italic "a b c", "**a\**b**" a "*" and an italic "a*b". ~ pairs two and
// two: "~~a~~" is struck through, "~a~" and "~~a~" are words, "~~~a~~~" a
// "~", a struck "a" and a "~".

// notionMarkdownEmphasis holds a line's pieces until its emphasis pairs.
type notionMarkdownEmphasis struct {
	pieces []notionMarkdownEmphasisPiece
}

type notionMarkdownEmphasisPiece struct {
	// text is words as written; put writes a piece that stands whole with
	// the style around it.
	text string
	put  func(workspaceImportInlineStyle)
	// mark is a run's *, _ or ~, count how many of its marks are left,
	// length how many it had; opens and closes say what it may do.
	mark          byte
	count, length int
	opens, closes bool
	// italic, bold and strikethrough count the pairs that begin (+1) and
	// end (-1) their emphasis at this piece.
	italic, bold, strikethrough int
}

func (emphasis *notionMarkdownEmphasis) words(text string) {
	if text != "" {
		emphasis.pieces = append(emphasis.pieces, notionMarkdownEmphasisPiece{text: text})
	}
}

func (emphasis *notionMarkdownEmphasis) whole(put func(workspaceImportInlineStyle)) {
	emphasis.pieces = append(emphasis.pieces, notionMarkdownEmphasisPiece{put: put})
}

// run adds the run of marks at value[index:] and reports where it ends.
func (emphasis *notionMarkdownEmphasis) run(value string, index int) int {
	mark := value[index]
	end := index + len(value[index:]) - len(strings.TrimLeft(value[index:], string(mark)))
	opens, closes := workspaceImportFlanks(value, index, end)
	emphasis.pieces = append(emphasis.pieces, notionMarkdownEmphasisPiece{
		mark: mark, count: end - index, length: end - index, opens: opens, closes: closes,
	})
	return end
}

// pair pairs the runs as CommonMark's "process emphasis" does, with its
// openers_bottom so a long line is read in time.
func (emphasis *notionMarkdownEmphasis) pair() {
	runs := []int{}
	for at, piece := range emphasis.pieces {
		if piece.mark != 0 {
			runs = append(runs, at)
		}
	}
	// The runs not yet left as words, linked both ways by their order.
	previous, next := make([]int, len(runs)), make([]int, len(runs))
	for at := range runs {
		previous[at], next[at] = at-1, at+1
	}
	remove := func(at int) {
		if previous[at] >= 0 {
			next[previous[at]] = next[at]
		}
		if next[at] < len(runs) {
			previous[next[at]] = previous[at]
		}
	}
	type floorKey struct {
		mark  byte
		opens bool
		rest  int
	}
	floors := map[floorKey]int{}
	for closer := 0; closer < len(runs); {
		closing := &emphasis.pieces[runs[closer]]
		if !closing.closes {
			closer = next[closer]
			continue
		}
		key := floorKey{closing.mark, closing.opens, closing.length % 3}
		if closing.mark == '~' {
			key.rest = min(closing.count, 2)
		}
		floor, known := floors[key]
		if !known {
			floor = -1
		}
		opener := previous[closer]
		for ; opener > floor; opener = previous[opener] {
			opening := &emphasis.pieces[runs[opener]]
			if opening.mark != closing.mark || !opening.opens {
				continue
			}
			if closing.mark == '~' {
				if opening.count >= 2 && closing.count >= 2 {
					break
				}
				continue
			}
			odd := (closing.opens || opening.closes) && closing.length%3 != 0 && (opening.length+closing.length)%3 == 0
			if !odd {
				break
			}
		}
		if opener <= floor {
			floors[key] = previous[closer]
			following := next[closer]
			if !closing.opens {
				remove(closer)
			}
			closer = following
			continue
		}
		opening := &emphasis.pieces[runs[opener]]
		used := 1
		if opening.count >= 2 && closing.count >= 2 {
			used = 2
		}
		opening.count -= used
		closing.count -= used
		first, after := &emphasis.pieces[runs[opener]+1], &emphasis.pieces[runs[closer]]
		switch {
		case closing.mark == '~':
			first.strikethrough++
			after.strikethrough--
		case used == 2:
			first.bold++
			after.bold--
		default:
			first.italic++
			after.italic--
		}
		for between := next[opener]; between != closer; between = next[between] {
			remove(between)
		}
		next[opener], previous[closer] = closer, opener
		if opening.count == 0 {
			remove(opener)
		}
		if closing.count == 0 {
			following := next[closer]
			remove(closer)
			closer = following
		}
	}
}

// write puts the line's pieces into the parser's spans, each with the
// style around the line and the emphasis it stands in; a run's marks left
// over are words.
func (emphasis *notionMarkdownEmphasis) write(parser *workspaceImportInlineParser, style workspaceImportInlineStyle) {
	emphasis.pair()
	italic, bold, strikethrough := 0, 0, 0
	for _, piece := range emphasis.pieces {
		italic += piece.italic
		bold += piece.bold
		strikethrough += piece.strikethrough
		inner := style
		inner.italic = inner.italic || italic > 0
		inner.bold = inner.bold || bold > 0
		inner.strikethrough = inner.strikethrough || strikethrough > 0
		switch {
		case piece.put != nil:
			piece.put(inner)
		case piece.mark != 0:
			parser.emitText(strings.Repeat(string(piece.mark), piece.count), inner)
		default:
			parser.emitText(piece.text, inner)
		}
	}
}
