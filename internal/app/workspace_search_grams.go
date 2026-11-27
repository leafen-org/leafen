package app

import (
	"strings"
	"sync"
	"time"
)

// A long page's grams are made a line at a time, and the lines it had when
// they were last made are kept with theirs: a page changed again makes only
// its changed lines' grams. Made from the page's whole text at each change,
// the index made a long page's grams again for every keystroke batch
// (2026-10-18 soak: most of the index's allocation, 376 GB an hour). A run
// of a paired script and a word end with their line, so a page's grams are
// its lines' in turn (searchTextGrams).

const (
	// searchGramsKeptMinBytes is the text from which a page's lines are kept.
	searchGramsKeptMinBytes = 64 << 10
	searchGramsKeptPages    = 8
	searchGramsKeptIdle     = 10 * time.Minute
)

type searchKeptGrams struct {
	lines map[string]string
	timer *time.Timer
}

type searchGramsBook struct {
	mu    sync.Mutex
	pages map[string]*searchKeptGrams
	order []string
}

var searchGramsKept = &searchGramsBook{pages: map[string]*searchKeptGrams{}}

// searchPageGrams is a page's grams, as searchTextGrams makes them.
func searchPageGrams(pageID, text string) string {
	if len(text) < searchGramsKeptMinBytes {
		searchGramsKept.take(pageID)
		return searchTextGrams(text)
	}
	before := searchGramsKept.take(pageID)
	lines := make(map[string]string, len(before))
	grams := strings.Builder{}
	var run []string
	for line := range strings.SplitSeq(text, "\n") {
		made, known := lines[line]
		if !known {
			if made, known = before[line]; !known {
				one := strings.Builder{}
				run = searchLineGrams(&one, line, run)
				made = one.String()
			}
			lines[line] = made
		}
		grams.WriteString(made)
	}
	searchGramsKept.put(pageID, lines)
	return strings.TrimSpace(grams.String())
}

// take hands out the page's kept lines, leaving it without them.
func (book *searchGramsBook) take(pageID string) map[string]string {
	book.mu.Lock()
	defer book.mu.Unlock()
	kept := book.pages[pageID]
	if kept == nil {
		return nil
	}
	book.remove(pageID)
	return kept.lines
}

// put keeps the page's lines for its next change, a few pages' at most, for
// a while.
func (book *searchGramsBook) put(pageID string, lines map[string]string) {
	book.mu.Lock()
	defer book.mu.Unlock()
	book.remove(pageID)
	kept := &searchKeptGrams{lines: lines}
	kept.timer = time.AfterFunc(searchGramsKeptIdle, func() {
		book.mu.Lock()
		defer book.mu.Unlock()
		if book.pages[pageID] == kept {
			book.remove(pageID)
		}
	})
	book.pages[pageID] = kept
	book.order = append(book.order, pageID)
	for len(book.order) > searchGramsKeptPages {
		book.remove(book.order[0])
	}
}

func (book *searchGramsBook) remove(pageID string) {
	if kept := book.pages[pageID]; kept != nil {
		kept.timer.Stop()
		delete(book.pages, pageID)
	}
	for index, name := range book.order {
		if name == pageID {
			book.order = append(book.order[:index], book.order[index+1:]...)
			break
		}
	}
}
