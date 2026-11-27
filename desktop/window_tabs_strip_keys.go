//go:build (darwin && !ios) || windows

package main

import "encoding/json"

// Where the keys go as a menu of the strip closes, as Notion 7.44.1's tab
// bar gives them back (measured in its Windows app 2026-11-24, after each
// close with nothing in the page beforehand, a letter typed after): to what
// the menu opened from, on the strip — a tab's menu to its tab, a group's
// menu to its head, a group's menu opened as "신규 그룹" made it to the tab
// it was made from, the back and forward buttons' history menu (which the
// shown page draws) to its button. So it goes for an Escape, a pick and, for
// a group's or the history menu, a press outside it, which does nothing
// more; a press on the page outside a tab's menu goes on to the page, which
// takes the keys. A pick takes them where Notion's does: a tab closed, to
// the tab its menu was for if it is still there, else the shown one (its
// restoreTabFocusIfLost); a tab duplicated, to the copy; a group let go of,
// to its first tab; a group closed, to the shown tab; a closed tab opened
// again, to the tab the menu was for, whether it shows or comes back behind
// (twice each); but a close that took the page shown as the menu opened, to
// the page shown now, its own focus back (Notion's tab keeps its page's; a
// letter typed went into its page, twice). The thing takes them ringed for an Escape, an Enter or a menu key
// (the page's keyboard ring), and for a pick by a press unringed, the ring
// showing at the next key pressed (Notion's tab bar rings by the browser's
// focus-visible; measured 2026-11-24 in its Windows app with a letter after:
// the link, pinning, unpinning, a copy, into and out of a group, closes and
// reopening, by a press — no ring, the letter going nowhere and ringing it;
// by Enter, a ring at once; a key in the menu before a press, no ring). The
// strip keeps the ring till it is pressed or the keys leave it
// (tab_strip_ring.js).

// stripRing is what takes the keys on the strip: a tab, a group's head or
// the "back" or "forward" button; quiet, unringed till a key is pressed.
type stripRing struct {
	Tab    string `json:"tab,omitempty"`
	Group  string `json:"group,omitempty"`
	Button string `json:"button,omitempty"`
	Quiet  bool   `json:"quiet,omitempty"`
}

// none says the ring names nothing to take the keys.
func (ring stripRing) none() bool {
	return ring.Tab == "" && ring.Group == "" && ring.Button == ""
}

// stripKeys says where the keys go as a strip menu closes.
type stripKeys int

const (
	// keysToOpener: what the menu opened from takes them, ringed.
	keysToOpener stripKeys = iota
	// keysToPage: the shown page takes them (a press there, or the strip's
	// window going behind).
	keysToPage
	// keysStay: they stay where they are, for what closes the menu to open
	// another or to show another tab.
	keysStay
)

// ringStrip gives the frame's strip the keys on what ring names, ringed
// (quiet: unringed till a key is pressed there).
func ringStrip(frame *tabFrame, ring stripRing) {
	if frame.window == nil || ring.none() {
		return
	}
	focusStrip(frame)
	said, _ := json.Marshal(ring)
	frame.window.ExecJS("window.leafenStrip&&leafenStrip.ring(" + string(said) + ")")
}

// tabRing is a tab of the frame to ring: the one named if it is still in
// the frame, else the shown one.
func tabRing(frame *tabFrame, tab *appTab) stripRing {
	if tab == nil || frame.index(tab) < 0 {
		tab = frame.active
	}
	if tab == nil {
		return stripRing{}
	}
	return stripRing{Tab: tab.id}
}

// openerRing is what a closing menu gives the keys back to: its tab, or its
// group's head while the group is still there, else the tab it was made
// from or the shown tab.
func openerRing(frame *tabFrame, menu *stripMenu) stripRing {
	if menu.kind == "group" && menu.from == nil && containsGroup(frame.groups, menu.group) {
		return stripRing{Group: menu.group.id}
	}
	if menu.kind == "group" {
		return tabRing(frame, menu.from)
	}
	return tabRing(frame, menu.tab)
}

// pickKeys is where the keys go after a menu's pick, as Notion's (above):
// the page shown now for a close that took the page shown as the menu
// opened; nowhere new for a tab gone to a new window, whose own window takes
// them, nor for a new group, whose menu opens; else on the strip, what the
// menu opened from but for the picks that take them elsewhere, ringed for a
// pick by a key (keys) and quiet for one by a press.
func pickKeys(frame *tabFrame, menu *stripMenu, pick string, firstOfGroup *appTab, keys bool) (stripRing, stripKeys) {
	var ring stripRing
	switch pick {
	case "newWindow", "newGroup":
		return stripRing{}, keysStay
	case "close", "closeOthers", "closeBefore", "closeAfter", "closeGroup":
		if menu.page != nil && frame.index(menu.page) < 0 && frame.active != nil {
			return stripRing{}, keysToPage
		}
	}
	switch pick {
	case "duplicate", "closeGroup":
		ring = tabRing(frame, frame.active)
	case "ungroup":
		ring = tabRing(frame, firstOfGroup)
	default:
		ring = openerRing(frame, menu)
	}
	ring.Quiet = !keys
	return ring, keysToOpener
}

// ringAfterPick gives the keys where a menu's pick takes them (pickKeys).
// A page a pick shows as it starts — a copy, or a tab opened again that was
// shown as it closed — takes the keys as it starts, and gives them back to
// the strip once it has drawn its first screen (drawn).
func ringAfterPick(frame *tabFrame, menu *stripMenu, pick string, first *appTab, keys bool) {
	ring, where := pickKeys(frame, menu, pick, first, keys)
	switch where {
	case keysToPage:
		focusTabView(frame, frame.active)
	case keysToOpener:
		ringStrip(frame, ring)
		if (pick == "duplicate" || pick == "reopen") && frame.active != nil && frame.active.starting != nil {
			frame.active.ringDrawn = ring
		}
	}
}

// historyClosed takes that the shown page's history menu closed: its button
// takes the keys, ringed.
func (t *windowTabs) historyClosed(frame *tabFrame, forward bool) {
	button := "back"
	if forward {
		button = "forward"
	}
	ringStrip(frame, stripRing{Button: button})
}
