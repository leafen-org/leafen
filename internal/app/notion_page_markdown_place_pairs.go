package app

// A page's new Markdown keeps its current blocks one level at a time, as
// Notion's page Markdown does (seen 2026-11-21 through its API: 126 shapes
// written over as "v2", as "v1" and by update_content, beside the 139 of the
// place rule below): the blocks side by side under one parent —
// the page, or a block that keeps one — each read as its own line alone,
// and the gaps between them as one more line each. The longest common
// sequence of those, the ends first and then the current one's taken away
// before the new one's added (lineMatches), keeps a block whose own line
// stands unchanged; a gap kept holds the blocks around it in their places
// just as well ("k⏎⏎a" written over by "x⏎⏎k" keeps the gap, x taking k's
// block and k a's; "a⏎⏎k" by "k⏎⏎x" keeps k; "a⏎⏎b⏎⏎k" by "k⏎⏎x⏎⏎y" keeps
// both gaps, k taking a's block). So an unchanged line out of its place
// keeps its block only where no more gaps would go for it (one left over may
// still keep it, moved, matchNotionMarkdownBlocks). What a block that keeps
// one holds is paired the same way with what that one held, whether its own
// line changed or not ("<toggle>⏎t⏎⏎k⏎</toggle>⏎⏎a" by
// "<toggle>⏎t⏎⏎k⏎⏎m⏎</toggle>" keeps t and k; "- p⏎  - k⏎- a" by
// "- x⏎- p⏎  - k" gives x p's block and p a's).
//
// Between the blocks kept, the changed blocks take the current blocks in
// their order — where fewer stand than were, the last of those that were
// ("a⏎⏎b⏎⏎c" written over by "x" keeps c's block), and where more, the first
// of them take all there were and the rest are new ("a" by "x⏎⏎y" makes x of
// a's block). A block keeps the one in its place only as a kind that one
// turns into (notionMarkdownTurnsInto), or as the same line; else both
// stand alone, the one going and the other new, and no block after it moves
// up into its place ("---⏎⏎a" by "x⏎⏎---" makes both new). What a new block
// holds is new. Nothing pairs by how alike the lines read: "a⏎⏎beta two" by
// "beta two!" keeps b's block, "beta two" by "new⏎⏎beta two!" gives it to
// "new", files and links alike ("![one](u)⏎⏎b" by "x⏎⏎![two](u)" makes both
// new).

// notionMarkdownTextKinds turn into one another, keeping their block.
var notionMarkdownTextKinds = map[string]bool{
	graphBlockTypeParagraph: true, graphBlockTypeHeading1: true, graphBlockTypeHeading2: true,
	graphBlockTypeHeading3: true, graphBlockTypeHeading4: true, graphBlockTypeToggleHeading1: true,
	graphBlockTypeToggleHeading2: true, graphBlockTypeToggleHeading3: true, graphBlockTypeToggleHeading4: true,
	graphBlockTypeBulletedListItem: true, graphBlockTypeNumberedListItem: true, graphBlockTypeToDo: true,
	graphBlockTypeQuote: true, graphBlockTypeToggle: true,
}

// notionMarkdownSameKinds keep their block only as their own kind: a code
// block or an equation whose line changed, a callout only as a callout, a
// table, and a table's row only while its cells stay as they are
// (notionMarkdownRereadRows). A file or a link keeps its block as
// matchNotionMarkdownBlocks says.
var notionMarkdownSameKinds = map[string]bool{
	graphBlockTypeCode: true, graphBlockTypeEquation: true, graphBlockTypeCallout: true, graphBlockTypeDivider: true,
	graphBlockTypeTable: true, graphBlockTypeTableRow: true,
}

// notionMarkdownTurnsInto tells whether a block of one kind keeps its block
// written over as another.
func notionMarkdownTurnsInto(before, after string) bool {
	return notionMarkdownTextKinds[before] && notionMarkdownTextKinds[after] || before == after && notionMarkdownSameKinds[before]
}

// notionMarkdownGap is the line a gap between two blocks side by side reads
// as, whatever lies in it.
const notionMarkdownGap = "\x00gap"

// notionMarkdownPlaces pairs the blocks of new Markdown with the current
// ones, level by level.
type notionMarkdownPlaces struct {
	oldTree, newTree notionMarkdownTree
	matched, edited  []int
	// turns tells whether the current block at oldIndex keeps its block
	// written over by the new one at newIndex.
	turns func(oldIndex, newIndex int) bool
}

// notionMarkdownTree is what each item holds right under it, the items at
// the top, and each item's own line as a match reads it.
type notionMarkdownTree struct {
	children [][]int
	roots    []int
	keys     []string
}

func newNotionMarkdownPlaces(
	old, items []workspaceImportStagedItem, oldKeys, newKeys []string, matched, edited []int, turns func(int, int) bool,
) *notionMarkdownPlaces {
	return &notionMarkdownPlaces{
		oldTree: newNotionMarkdownTree(old, oldKeys), newTree: newNotionMarkdownTree(items, newKeys),
		matched: matched, edited: edited, turns: turns,
	}
}

func newNotionMarkdownTree(items []workspaceImportStagedItem, keys []string) notionMarkdownTree {
	at := make(map[string]int, len(items))
	tree := notionMarkdownTree{children: make([][]int, len(items)), keys: keys}
	for index, item := range items {
		at[item.ID] = index
		if parent, ok := at[item.ParentID]; ok && item.ParentID != "" {
			tree.children[parent] = append(tree.children[parent], index)
		} else {
			tree.roots = append(tree.roots, index)
		}
	}
	return tree
}

// lines reads blocks side by side as lines: each block's own, a gap between.
func (tree notionMarkdownTree) lines(blocks []int) []string {
	lines := make([]string, 0, 2*len(blocks))
	for at, index := range blocks {
		if at > 0 {
			lines = append(lines, notionMarkdownGap)
		}
		lines = append(lines, tree.keys[index])
	}
	return lines
}

// match pairs the blocks side by side under one parent: those whose own
// line stands unchanged, and between them, by place, the rest; and what
// each pair holds the same way.
func (places *notionMarkdownPlaces) match(oldBlocks, newBlocks []int) {
	kept, _ := lineMatches(places.oldTree.lines(oldBlocks), places.newTree.lines(newBlocks))
	lastOld, lastNew := 0, 0
	for at := range oldBlocks {
		// A block's line is every other one, as is a gap's.
		to := kept[2*at]
		if to < 0 || to%2 != 0 {
			continue
		}
		places.zip(oldBlocks[lastOld:at], newBlocks[lastNew:to/2])
		places.matched[newBlocks[to/2]] = oldBlocks[at]
		places.match(places.oldTree.children[oldBlocks[at]], places.newTree.children[newBlocks[to/2]])
		lastOld, lastNew = at+1, to/2+1
	}
	places.zip(oldBlocks[lastOld:], newBlocks[lastNew:])
}

// zip pairs the changed blocks side by side with the current ones there, by
// place, and what each pair holds as match does.
func (places *notionMarkdownPlaces) zip(oldBlocks, newBlocks []int) {
	kept := min(len(oldBlocks), len(newBlocks))
	for at, newIndex := range newBlocks[:kept] {
		oldIndex := oldBlocks[len(oldBlocks)-kept+at]
		if !places.turns(oldIndex, newIndex) && places.oldTree.keys[oldIndex] != places.newTree.keys[newIndex] {
			continue
		}
		places.edited[newIndex] = oldIndex
		places.match(places.oldTree.children[oldIndex], places.newTree.children[newIndex])
	}
}
