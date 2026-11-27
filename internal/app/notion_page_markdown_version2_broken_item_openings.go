package app

import "strings"

// Where a "v2" write is refused for a list's item read inline
// (notionMarkdownVersion2BrokenItems), Notion's "v2" still says each block's
// opening tag first on its line that its reading refuses
// (notionMarkdownVersion2RefusedOpening), as it says them anywhere: after the
// tags left open and before all else, in order (seen 2026-11-16, written
// twice each: "<toggle x:y=\"1\">⏎<li>a</li></toggle>" says the malformed
// attribute, then that the <li> cannot stand inline; "<toggle x:y=\"1\">⏎
// <li>a</li> w" "Trailing unclosed …: <toggle>." first; "<ul>⏎<li>a⏎
// <li x:y=\"1\">b</li>⏎</li>⏎</ul>" the malformed attribute before the
// list's children). The lines read as Markdown of their own say their own
// (notionMarkdownVersion2ItemsPast).

// notionMarkdownVersion2BrokenItemOpenings is said, the words of such a
// refusal with the tags left open (notionMarkdownVersion2LeftOpen), with what
// is said of the refused openings first on lines among lines put after the
// tags left open.
func notionMarkdownVersion2BrokenItemOpenings(lines []string, said string, span notionMarkdownVersion2ItemSpan) string {
	openings, fence := []string{}, ""
	for index, line := range lines {
		body := strings.TrimSpace(line[len(notionMarkdownVersion2MarkerRun.FindString(line)):])
		if fence != "" || notionMarkdownVersion2Fence(body) {
			if fence == "" {
				fence = notionMarkdownFenceOf(body)
			} else if notionMarkdownFenceCloses(body, fence, true) {
				fence = ""
			}
			continue
		}
		if span.reads(index) || !notionMarkdownVersion2RefusedOpening(body) {
			continue
		}
		openings = append(openings, notionMarkdownVersion2OpeningRefusal(body)...)
	}
	if len(openings) == 0 {
		return said
	}
	if first, rest, _ := strings.Cut(said, "\n"); strings.HasPrefix(first, notionMarkdownVersion2TrailingSaid) {
		return strings.TrimSuffix(first+"\n"+strings.Join(openings, "\n")+"\n"+rest, "\n")
	}
	return strings.Join(openings, "\n") + "\n" + said
}
