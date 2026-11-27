package app

import (
	"math"

	"github.com/pocketbase/dbx"
)

// A picker or search that reads a fixed number of candidates and only then
// leaves out those the reader may not see, or may not choose, shows nothing
// of what they may past that number, and says nothing of it: the @ menu read
// a workspace's hundred latest matching rows, a member who could read none
// of them was offered none of the rows they could (2026-11-23). Each reads
// on, a window at a time from where the last ended, until it holds what it
// shows or nothing is left; a window twice the size of the one before, up
// to windowFillLargest, as each read goes through every match once more.

const windowFillLargest = 1600

// fillWindows keeps, of what read gives a window at a time, those keep
// accepts, in order, until limit are kept or a window comes back short.
// read gets the size of the window and the last candidate of the one
// before (nil for the first); keep may fill in what it keeps.
func fillWindows[T any](first, limit int, read func(size int, last *T) ([]T, error), keep func(*T) (bool, error)) ([]T, error) {
	kept := []T{}
	err := eachWindow(first, read, func(batch []T) (bool, error) {
		for index := range batch {
			if len(kept) == limit {
				break
			}
			accepted, err := keep(&batch[index])
			if err != nil {
				return false, err
			}
			if accepted {
				kept = append(kept, batch[index])
			}
		}
		return len(kept) == limit, nil
	})
	return kept, err
}

// eachWindow gives visit what read gives a window at a time until visit
// says it has what it needs or a window comes back short.
func eachWindow[T any](first int, read func(size int, last *T) ([]T, error), visit func([]T) (bool, error)) error {
	var last *T
	for size := first; ; size = min(size*2, max(first, windowFillLargest)) {
		batch, err := read(size, last)
		if err != nil {
			return err
		}
		done, err := visit(batch)
		if err != nil || done || len(batch) < size {
			return err
		}
		last = &batch[len(batch)-1]
	}
}

// windowFillNewestFirst is the start of a read newest first by an integer
// time and then an ID, before any window: everything comes after it.
const windowFillNewestFirst = math.MaxInt64

// windowFillAfterNewestSQL is the condition a read newest first by updated
// and then id puts on a window after the one ending at {:afterUpdated} and
// {:afterId}.
func windowFillAfterNewestSQL(updated, id string) string {
	return `(` + updated + ` < {:afterUpdated} OR (` + updated + ` = {:afterUpdated} AND ` + id + ` > {:afterId}))`
}

// windowFillAfterNewest binds windowFillAfterNewestSQL after a window whose
// last candidate key names by its time and ID, or before every candidate
// for the first.
func windowFillAfterNewest[T any](last *T, key func(*T) (int64, string)) dbx.Params {
	if last == nil {
		return dbx.Params{"afterUpdated": int64(windowFillNewestFirst), "afterId": ""}
	}
	updated, id := key(last)
	return dbx.Params{"afterUpdated": updated, "afterId": id}
}
