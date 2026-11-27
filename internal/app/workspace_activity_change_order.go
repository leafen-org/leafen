package app

// The order of an edit's changes in workspace updates
// (workspaceActivityChangedBlocks): the order the edit first changed each
// block, the blocks one change made at once in the page's order, and a block
// made around lines the edit changed before it right before them.

// workspaceActivityChainOrder orders things each linked to the one before
// it: a thing follows the one it names where that one is among them, and
// runs that can't be placed among each other keep the order they came in.
func workspaceActivityChainOrder(ids, follows []string) []int {
	index := make(map[string]int, len(ids))
	for at, id := range ids {
		index[id] = at
	}
	next := map[int][]int{}
	head := make([]bool, len(ids))
	for at := range ids {
		if previous, found := index[follows[at]]; found && previous != at {
			next[previous] = append(next[previous], at)
		} else {
			head[at] = true
		}
	}
	ordered := make([]int, 0, len(ids))
	placed := make([]bool, len(ids))
	var place func(at int)
	place = func(at int) {
		if placed[at] {
			return
		}
		placed[at] = true
		ordered = append(ordered, at)
		for _, following := range next[at] {
			place(following)
		}
	}
	for at := range ids {
		if head[at] {
			place(at)
		}
	}
	// Things that only follow each other round.
	for at := range ids {
		place(at)
	}
	return ordered
}

// workspaceActivityWrap is a block a change made around blocks it moved into
// it: a line turned into a callout or a synced block, which is made as a new
// block holding the line it was. Notion's line turned into one is that line,
// so its updates list the callout before the line in it — "콜아웃" then the
// words typed in it (seen 2026-11-17 by workers w1645 and w1658 in pages a
// and b, first account, test workspace).
type workspaceActivityWrap struct {
	block string
	held  map[string]bool
}

// workspaceActivityWrapsOf is the blocks a change made around blocks the
// edit changed before it — placed is where the edit placed those — given
// which of its blocks it made.
func workspaceActivityWrapsOf(deltas []pageHistoryGraphDelta, placed map[string]int, making func(pageHistoryGraphDelta) bool) []workspaceActivityWrap {
	wraps := []workspaceActivityWrap{}
	for _, delta := range deltas {
		if _, seen := placed[delta.BlockID]; seen || !making(delta) {
			continue
		}
		held := map[string]bool{}
		for _, inner := range deltas {
			if _, seen := placed[inner.BlockID]; seen && inner.Moved && !inner.Deleted && inner.ParentID == delta.BlockID {
				held[inner.BlockID] = true
			}
		}
		if len(held) > 0 {
			wraps = append(wraps, workspaceActivityWrap{block: delta.BlockID, held: held})
		}
	}
	return wraps
}

// workspaceActivityWrappedFirst puts each block made around others right
// before the first of them, with when that one was first changed, in the
// order they were made, so one made around another made around a line goes
// before both.
func workspaceActivityWrappedFirst(blocks []string, at map[string]int64, wraps []workspaceActivityWrap) []string {
	for _, wrap := range wraps {
		from, to := -1, -1
		for index, blockID := range blocks {
			if blockID == wrap.block {
				from = index
			} else if to < 0 && wrap.held[blockID] {
				to = index
			}
		}
		if from < 0 || to < 0 || to > from {
			continue
		}
		at[wrap.block] = min(at[wrap.block], at[blocks[to]])
		copy(blocks[to+1:from+1], blocks[to:from])
		blocks[to] = wrap.block
	}
	return blocks
}
