package app

import "slices"

// A relation limited to one page ("1개 페이지", its config's limit 1) holds
// one page on each of its sides, as Notion's does: a side the change gives a
// page keeps the page just given in place of the one it held, and the page it
// lets go leaves it on its own side in the same change (seen 2026-11-07 with
// a task added to a second sprint, and 2026-11-16 within one database: a row
// picking r1 through a relation to its own database left r1 holding that row
// alone, r1 itself let go). A side the change only takes pages from, or gives
// a page it holds already, keeps what it holds.

// databaseRelationLimit is the number of pages a relation takes, 0 for any.
func databaseRelationLimit(config map[string]any) int {
	limit, _ := config["limit"].(float64)
	if limit < 1 {
		return 0
	}
	return int(limit)
}

// keepDatabaseRelationLimit returns the value next, which current became by
// the change, kept to a limit of one page: the page given last that it holds,
// and the pages it let go.
func keepDatabaseRelationLimit(current, next []string, limit int) ([]string, []string) {
	if limit != 1 || len(next) <= 1 {
		return next, nil
	}
	added, _ := databaseRelationPairDiff(current, next)
	if len(added) == 0 {
		return next, nil
	}
	keep := added[len(added)-1]
	return []string{keep}, slices.DeleteFunc(slices.Clone(next), func(id string) bool { return id == keep })
}
