package app

import "github.com/pocketbase/pocketbase/core"

// What is under something in the trash is in the trash with it: a sub-page,
// a database page and a data source under a page in it, a database page with
// its data source (notion_trash_reads.go). A search reads that for the
// results it comes to, a window at a time from where its page of results
// starts, rather than for every title it read: a search with no query reads
// the workspace's every title, and climbing each one's pages to the top took
// a third of its time (2026-11-18, 20,000 pages).

const notionSearchTrashWindow = 256

type notionSearchTrash struct {
	app        core.App
	candidates []notionSearchCandidate
	inTrash    bool
	// read is whether a candidate's trash state has been read, left whether
	// it is left out.
	left map[int]bool
	read map[int]bool
}

func newNotionSearchTrash(app core.App, candidates []notionSearchCandidate, inTrash bool) *notionSearchTrash {
	return &notionSearchTrash{app: app, candidates: candidates, inTrash: inTrash, left: map[int]bool{}, read: map[int]bool{}}
}

// leftOut reports whether the candidate at index is left out of the search:
// under a page in the trash it isn't the root of, or a data source with its
// page in the trash, or anything in the trash when the search isn't for it.
func (trash *notionSearchTrash) leftOut(index int) (bool, error) {
	if !trash.read[index] {
		end := min(index+notionSearchTrashWindow, len(trash.candidates))
		holders := make([]string, 0, end-index)
		for at := index; at < end; at++ {
			if !trash.read[at] {
				holders = append(holders, trash.candidates[at].trashHolder)
			}
		}
		underTrash, err := loadPageTrashEffects(trash.app, holders)
		if err != nil {
			return false, err
		}
		for at := index; at < end; at++ {
			if trash.read[at] {
				continue
			}
			candidate := trash.candidates[at]
			effect, trashed := underTrash[candidate.trashHolder]
			trash.left[at] = trashed &&
				(effect.RootID != candidate.trashHolder || candidate.Kind == notionSearchDataSource || !trash.inTrash)
			trash.read[at] = true
		}
	}
	return trash.left[index], nil
}
