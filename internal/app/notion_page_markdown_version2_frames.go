package app

import (
	"maps"
	"slices"
)

// Where Notion's "v2" says what a block tag holds (seen 2026-11-15): it
// reads the Markdown's blocks and says what their texts' inline tags lack
// (notionMarkdownVersion2Said), then reads each block tag in turn — what
// it says of the tag itself (a bare <li>, what a <ul> holds), then of what
// the tag holds: a tag in it that its closing tag closed over
// (notionMarkdownUnclosedBlock), its blocks' inline tags, then its block tags, each the
// same way. "<li>a</li>⏎<callout>⏎x <b>y⏎</callout>" says the bare <li>
// before the <b>, "<ul>⏎<li>a</li>⏎x <b>y⏎</ul>" its children, the <b>,
// then a's bare, "<callout>⏎<p>x <b>y</p>⏎z <i>w⏎</callout>" the <i> before
// the <b>, "<ul>⏎<li>a</li>⏎<ol>⏎<li>b</li>⏎</ul>" its children, the <ol>
// closed over, then a's bare, and "<ul>⏎x⏎<li>b\</li>⏎</ul>⏎<ul>⏎<li>c\
// </li>⏎</ul>" the first list's words before the second's item closed over.
// A tag standing in text holds nothing so.

// notionMarkdownVersion2BlockTags are the block tags the reading met before
// a block's first line (from) and by its end (to).
type notionMarkdownVersion2BlockTags struct{ from, to int }

// frames puts what the blocks standing in block tags say, and what the tags
// the closing tags of those closed over say, among what the reading says
// (repair.missing) after what each such tag says of itself, the tags its
// closing tag closed over that its own reading says left open first
// (notionMarkdownVersion2FramedTrailing), then the openings in it refused
// (heldOpening); it is what the
// other blocks say, and the flow tags closed over in none. Blocks are each
// text's lines by their line in out, with the tags their first line met.
func (repair *notionMarkdownRepair) frames(texts map[int]string, places map[int]notionMarkdownVersion2BlockTags) (said, flow []string) {
	indexes := slices.Sorted(maps.Keys(texts))
	blocks := make([]notionMarkdownVersion2BlockTags, len(indexes))
	for at, index := range indexes {
		blocks[at] = places[index]
	}
	frames, parents := notionMarkdownVersion2BlockFrames(repair.inline, blocks), notionMarkdownVersion2TagFrames(repair.inline)
	// held are each tag's blocks' texts, and last the one of them that is
	// the Markdown's last block.
	held, last, top, topLast := map[int][]string{}, map[int]int{}, []string{}, -1
	for at, index := range indexes {
		frame := frames[at]
		if frame < 0 {
			if at == len(indexes)-1 {
				topLast = len(top)
			}
			top = append(top, texts[index])
			continue
		}
		if _, ok := last[frame]; !ok {
			last[frame] = -1
		}
		if at == len(indexes)-1 {
			last[frame] = len(held[frame])
		}
		held[frame] = append(held[frame], texts[index])
	}
	// A tag in one left open to the end, as an item left open holding what
	// follows it, is said with the tags left open (seen 2026-11-14:
	// "<li>a\</li>⏎<ul>⏎<li>b\</li>⏎</ul>⏎<ul>⏎<li>c\</li>⏎</ul>" says c's
	// item closed over before a's bare).
	closedOver, open, left := map[int][]string{}, notionMarkdownBlockTagsOpen(repair.inline), map[int]bool{}
	for _, one := range open {
		left[one.event] = !one.over
	}
	for _, one := range open {
		if !one.flow {
			continue
		}
		words := notionMarkdownUnclosedBlock(one.name)
		parent := parents[one.event]
		for frame := parent; frame >= 0 && parent >= 0; frame = parents[frame] {
			if left[frame] {
				parent = -1
			}
		}
		if parent >= 0 {
			closedOver[parent] = append(closedOver[parent], words)
		} else {
			flow = append(flow, words)
		}
	}
	// What was said of the tags marked in the texts is said where they stand
	// (repair.placedSaid), and not where it was.
	inserts, taken := map[int][]string{}, map[int]bool{}
	framed, _ := notionMarkdownVersion2FramedTrailing(repair.inline)
	for event, tag := range repair.inline {
		words := slices.Concat(framed[event], repair.heldOpenings[event], closedOver[event],
			repair.placedSaid(notionMarkdownVersion2SaidOf(held[event], last[event]), taken))
		if len(words) > 0 {
			inserts[tag.at] = append(inserts[tag.at], words...)
		}
	}
	said = repair.placedSaid(notionMarkdownVersion2SaidOf(top, topLast), taken)
	missing := make([]string, 0, len(repair.missing))
	for at := 0; at <= len(repair.missing); at++ {
		missing = append(missing, inserts[at]...)
		if at < len(repair.missing) && !taken[at] {
			missing = append(missing, repair.missing[at])
		}
	}
	repair.missing = missing
	return said, flow
}

// notionMarkdownVersion2TagFrames is, for each block tag the reading met,
// the block tag it stands in, -1 for none: a tag standing in text holds
// none, and a closing tag closes the last of its kind open and those it
// holds.
func notionMarkdownVersion2TagFrames(tags []notionMarkdownInlineBlockTag) []int {
	parents := make([]int, len(tags))
	stack := []int{}
	for event := range tags {
		parents[event] = notionMarkdownVersion2InnermostFrame(tags, stack)
		stack = notionMarkdownVersion2FrameStep(tags, stack, event)
	}
	return parents
}

// notionMarkdownVersion2BlockFrames is, for each block, the block tag it
// stands in, -1 for none: the last its first line opens, or else the one
// open at that line's end.
func notionMarkdownVersion2BlockFrames(tags []notionMarkdownInlineBlockTag, blocks []notionMarkdownVersion2BlockTags) []int {
	frames := make([]int, len(blocks))
	stack, next := []int{}, 0
	for event := 0; event <= len(tags); event++ {
		for ; next < len(blocks) && blocks[next].to <= event; next++ {
			frames[next] = notionMarkdownVersion2InnermostFrame(tags, stack)
			for opened := blocks[next].from; opened < blocks[next].to; opened++ {
				if tag := tags[opened]; !tag.closing && !tag.closedInSelf && !tag.text {
					frames[next] = opened
				}
			}
		}
		if event < len(tags) {
			stack = notionMarkdownVersion2FrameStep(tags, stack, event)
		}
	}
	return frames
}

// notionMarkdownVersion2InnermostFrame is the last block tag of the open
// ones (stack) that stands in no text, or -1.
func notionMarkdownVersion2InnermostFrame(tags []notionMarkdownInlineBlockTag, stack []int) int {
	for at := len(stack) - 1; at >= 0; at-- {
		if !tags[stack[at]].text {
			return stack[at]
		}
	}
	return -1
}

// notionMarkdownVersion2FrameStep is the open block tags after the one met.
// A list's closing tag standing in text, its list made no list, closes no
// item it holds: the item holds what follows to its own closing tag or the
// end (repair.itemOverByTextList; seen 2026-11-15: "<ul>⏎<li>a\</li>⏎</ul>
// x⏎<li>c</li>⏎y <i>z" says a's <li> closed as a flow tag and bare, then the
// <i> left open in its text, then c's bare <li>).
func notionMarkdownVersion2FrameStep(tags []notionMarkdownInlineBlockTag, stack []int, event int) []int {
	switch tag := tags[event]; {
	case tag.closedInSelf:
	case !tag.closing:
		stack = append(stack, event)
	default:
		for at := len(stack) - 1; at >= 0; at-- {
			if tags[stack[at]].name != tag.name {
				continue
			}
			if !tag.text || tag.name != "ul" && tag.name != "ol" {
				return stack[:at]
			}
			items := slices.DeleteFunc(slices.Clone(stack[at+1:]), func(open int) bool {
				return tags[open].name != "li" || tags[open].text
			})
			return append(slices.Clone(stack[:at]), items...)
		}
	}
	return stack
}
