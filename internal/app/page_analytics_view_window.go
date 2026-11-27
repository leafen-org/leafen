package app

import (
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A person's opens of the same page, database row page or database opened
// alone within a minute of their last counted view add no view, as Notion's
// (seen 2026-11-11 on synthetic pages, rows and an inline database: a second
// open 5 to 57 seconds after the counted one adds none, one 60 seconds or
// more after does — opens at 0, 40 and 80 seconds count two — and another
// person's open in the same minute counts for them). Each server keeps, for
// a minute, when each viewer's view of each target was last counted; a
// restart forgets them, which counts at most one open more. A visit that
// adds no view still moves who viewed and when.

const (
	pageAnalyticsRepeatViewWindow   = time.Minute
	pageAnalyticsViewWindowStoreKey = "leafen.page_analytics.view_window"
)

// The kinds of thing a view is of.
const (
	pageAnalyticsViewOfPage     = "page"
	pageAnalyticsViewOfRow      = "row"
	pageAnalyticsViewOfDatabase = "database"
)

type pageAnalyticsViewTarget struct {
	WorkspaceID string
	Kind        string
	TargetID    string
	ViewerKey   string
}

type pageAnalyticsViewWindow struct {
	mu      sync.Mutex
	counted map[pageAnalyticsViewTarget]time.Time
	swept   time.Time
}

func pageAnalyticsViewWindowOf(app core.App) *pageAnalyticsViewWindow {
	return app.Store().GetOrSet(pageAnalyticsViewWindowStoreKey, func() any {
		return &pageAnalyticsViewWindow{counted: map[pageAnalyticsViewTarget]time.Time{}}
	}).(*pageAnalyticsViewWindow)
}

// open says which of a batch's targets count a view now: those whose view by
// this viewer wasn't counted in the last minute. A target named more than
// once in the batch counts once. undo puts back what was there before, for
// a batch whose recording failed.
func (window *pageAnalyticsViewWindow) open(
	targets []pageAnalyticsViewTarget,
	now time.Time,
) (counts map[pageAnalyticsViewTarget]bool, undo func()) {
	window.mu.Lock()
	defer window.mu.Unlock()
	window.sweep(now)
	counts = make(map[pageAnalyticsViewTarget]bool, len(targets))
	earlier := map[pageAnalyticsViewTarget]time.Time{}
	for _, target := range targets {
		if _, decided := counts[target]; decided {
			continue
		}
		last, seen := window.counted[target]
		counts[target] = !seen || now.Sub(last) >= pageAnalyticsRepeatViewWindow
		if counts[target] {
			if seen {
				earlier[target] = last
			}
			window.counted[target] = now
		}
	}
	undo = func() {
		window.mu.Lock()
		defer window.mu.Unlock()
		for target, counted := range counts {
			if !counted || !window.counted[target].Equal(now) {
				continue
			}
			if last, seen := earlier[target]; seen {
				window.counted[target] = last
			} else {
				delete(window.counted, target)
			}
		}
	}
	return counts, undo
}

// sweep lets go, at most once a minute, of the views counted over a minute
// ago, which no longer hold an open back.
func (window *pageAnalyticsViewWindow) sweep(now time.Time) {
	if now.Sub(window.swept) < pageAnalyticsRepeatViewWindow {
		return
	}
	window.swept = now
	for target, last := range window.counted {
		if now.Sub(last) >= pageAnalyticsRepeatViewWindow {
			delete(window.counted, target)
		}
	}
}

// countViews is which of a batch's pages, rows and databases opened alone
// count a view now, each once; an anonymous visitor's batch names pages only.
func (window *pageAnalyticsViewWindow) countViews(
	workspaceID, viewerKey string,
	pageIDs []string,
	rows, databases rowPageAnalyticsObservations,
	now time.Time,
) (countedPageIDs []string, countedRows, countedDatabases rowPageAnalyticsObservations, undo func()) {
	target := func(kind, id string) pageAnalyticsViewTarget {
		return pageAnalyticsViewTarget{WorkspaceID: workspaceID, Kind: kind, TargetID: id, ViewerKey: viewerKey}
	}
	targets := make([]pageAnalyticsViewTarget, 0, len(pageIDs)+len(rows.IDs)+len(databases.IDs))
	for _, pageID := range pageIDs {
		targets = append(targets, target(pageAnalyticsViewOfPage, pageID))
	}
	for _, rowID := range rows.IDs {
		targets = append(targets, target(pageAnalyticsViewOfRow, rowID))
	}
	for _, containerID := range databases.IDs {
		targets = append(targets, target(pageAnalyticsViewOfDatabase, containerID))
	}
	counts, undo := window.open(targets, now)
	countedPageIDs = make([]string, 0, len(pageIDs))
	for _, pageID := range pageIDs {
		if counts[target(pageAnalyticsViewOfPage, pageID)] {
			countedPageIDs = append(countedPageIDs, pageID)
		}
	}
	countedOf := func(kind string, held rowPageAnalyticsObservations) rowPageAnalyticsObservations {
		counted := rowPageAnalyticsObservations{Counts: map[string]int{}, Pages: map[string]string{}}
		for _, id := range held.IDs {
			if counts[target(kind, id)] {
				counted.IDs = append(counted.IDs, id)
				counted.Counts[id], counted.Pages[id] = 1, held.Pages[id]
			}
		}
		return counted
	}
	return countedPageIDs, countedOf(pageAnalyticsViewOfRow, rows), countedOf(pageAnalyticsViewOfDatabase, databases), undo
}

// countedMemberObservations is one observation for each view counted.
func countedMemberObservations(pageIDs []string, rows, databases rowPageAnalyticsObservations) []pageAnalyticsObservation {
	observations := make([]pageAnalyticsObservation, 0, len(pageIDs)+len(rows.IDs)+len(databases.IDs))
	for _, pageID := range pageIDs {
		observations = append(observations, pageAnalyticsObservation{PageID: pageID})
	}
	for _, rowID := range rows.IDs {
		observations = append(observations, pageAnalyticsObservation{PageID: rows.Pages[rowID], RowID: rowID})
	}
	for _, containerID := range databases.IDs {
		observations = append(observations, pageAnalyticsObservation{PageID: databases.Pages[containerID], ContainerID: containerID})
	}
	return observations
}
