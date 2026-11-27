package app

import "strings"

// A table cell's line that starts with a list's bullet — "-", "*" or "+" —
// and a space reads as Notion's page Markdown reads it (seen 2026-11-14,
// "v1" and "v2" alike, its <td> and <th> and a pipe table's cells): the
// bullet written "  •", what follows as written ("- a" is "  • a", "-  b"
// "  •  b", "a<br>- b" and a cell's next line "a⏎  • b", "**- a**" the bold
// "  • a", "\- a" "  • a" too); a numbered item's mark, "-b", "-\tb" and a
// bullet within a line stay ("1. b", "x - a").

// notionMarkdownCellBullet is how a cell's line writes a bullet starting it.
const notionMarkdownCellBullet = "  •"

// notionMarkdownCellBullets is a cell's text and spans with the bullets
// starting its lines written as Notion writes them; a mention's or an
// equation's text stays.
func notionMarkdownCellBullets(plain string, spans []graphTextSpan) (string, []graphTextSpan) {
	starts := map[int]bool{}
	for at := 0; at+1 < len(plain); at++ {
		if (at == 0 || plain[at-1] == '\n') && strings.IndexByte("-*+", plain[at]) >= 0 && plain[at+1] == ' ' {
			starts[at] = true
		}
	}
	if len(starts) == 0 {
		return plain, spans
	}
	written := func(text string, offset int) string {
		var out strings.Builder
		for at := 0; at < len(text); at++ {
			if starts[offset+at] {
				out.WriteString(notionMarkdownCellBullet)
				continue
			}
			out.WriteByte(text[at])
		}
		return out.String()
	}
	if spans == nil {
		return written(plain, 0), nil
	}
	out, offset, text := make([]graphTextSpan, len(spans)), 0, strings.Builder{}
	for index, span := range spans {
		out[index] = span
		if span.Mention == "" && span.Equation == "" {
			out[index].Text = written(span.Text, offset)
		}
		offset += len(span.Text)
		text.WriteString(out[index].Text)
	}
	return text.String(), out
}
