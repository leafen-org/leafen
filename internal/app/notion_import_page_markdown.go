package app

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// A move reads a page's "v2" Markdown, one request, for what Notion's block
// API doesn't tell: whether a plain heading holds blocks
// (notion_import_heading_children.go) and the data source mentions its
// lines hold (notion_import_data_source_mentions.go). One reading serves
// both, remembered for the next listings of that page in the same job or
// scan; a page whose Markdown Notion refuses is not remembered.

// notionImportPageReading is what one reading of a page's Markdown tells.
type notionImportPageReading struct {
	// Holds is whether the page may hold blocks under a plain heading:
	// Markdown cut short, or with blocks Notion left out of it, may.
	Holds bool
	// Mentions are its lines' data source mentions, nil when it names none.
	Mentions *notionImportMarkdownMentions
}

// notionImportPageMarkdown reads a page's "v2" Markdown, or remembers its
// reading, and says whether it asked Notion; refused, it holds and names
// nothing.
func notionImportPageMarkdown(
	ctx context.Context, client notionProviderClient, scope, pageID string,
) (reading notionImportPageReading, read bool, err error) {
	key := scope + "\x00" + pageID
	if known, found := notionImportPageReadings.get(key); found {
		return known, false, nil
	}
	answer := struct {
		Markdown        string   `json:"markdown"`
		Truncated       bool     `json:"truncated"`
		UnknownBlockIDs []string `json:"unknown_block_ids"`
	}{}
	err = client.requestVersion(ctx, notionProviderMarkdownAPIVersion, http.MethodGet,
		"/v1/pages/"+url.PathEscape(pageID)+"/markdown?markdown_version=v2", nil, &answer)
	if notionProviderRefused(err) {
		return notionImportPageReading{Holds: true}, true, nil
	}
	if err != nil {
		return notionImportPageReading{}, true, err
	}
	reading.Holds = answer.Truncated || len(answer.UnknownBlockIDs) > 0 ||
		notionMarkdownHeadingHoldsTag.MatchString(answer.Markdown)
	if strings.Contains(answer.Markdown, notionMarkdownDataSourceAddress) {
		reading.Mentions = readNotionImportMarkdownMentions(answer.Markdown)
	}
	notionImportPageReadings.put(key, reading)
	return reading, true, nil
}

// notionImportPageReadings remembers the readings of the pages read last: a
// page's listings follow one another, its blocks' and sub-pages' soon after.
// It holds so many pages, and so many lines of those naming data sources;
// past either the oldest go.
var notionImportPageReadings = &notionImportPageReadingMemo{readings: map[string]notionImportPageReading{}}

const (
	notionImportPageReadingMemoSize  = 512
	notionImportPageReadingMemoLines = 200000
)

type notionImportPageReadingMemo struct {
	sync.Mutex
	order    []string
	readings map[string]notionImportPageReading
	lines    int
}

func (memo *notionImportPageReadingMemo) get(key string) (notionImportPageReading, bool) {
	memo.Lock()
	defer memo.Unlock()
	reading, known := memo.readings[key]
	return reading, known
}

func (memo *notionImportPageReadingMemo) put(key string, reading notionImportPageReading) {
	memo.Lock()
	defer memo.Unlock()
	if was, known := memo.readings[key]; known {
		memo.lines -= was.lineCount()
	} else {
		memo.order = append(memo.order, key)
	}
	memo.readings[key] = reading
	memo.lines += reading.lineCount()
	for len(memo.order) > 1 && (len(memo.order) > notionImportPageReadingMemoSize || memo.lines > notionImportPageReadingMemoLines) {
		memo.lines -= memo.readings[memo.order[0]].lineCount()
		delete(memo.readings, memo.order[0])
		memo.order = memo.order[1:]
	}
}

func (reading notionImportPageReading) lineCount() int {
	if reading.Mentions == nil {
		return 0
	}
	return len(reading.Mentions.Lines)
}
