package app

import "encoding/json"

// The awareness field the page's client keeps its presence in
// (web/src/lib/pagePresenceModel.ts PAGE_PRESENCE_AWARENESS_FIELD).
const pagePresenceAwarenessField = "leafenPresence"

// readerPresenceState is what the others in a page's room learn of someone
// who may only read or comment it: that someone with the page in sight is
// here, never who, nor where they look. As Notion's (seen 2026-11-12): while
// only a reader has the page open, its editor's bar shows their own face, and
// no reader's face is shown to anyone. The page, the sequence the client
// orders its states by, and whether the page is out of sight are kept; all
// else (the name, the person, the cursor) is dropped. A state that isn't the
// page's presence is dropped as removed.
func readerPresenceState(state []byte) []byte {
	var outer map[string]json.RawMessage
	if json.Unmarshal(state, &outer) != nil {
		return nil
	}
	raw, ok := outer[pagePresenceAwarenessField]
	if !ok {
		return nil
	}
	var presence struct {
		PageID   string  `json:"pageId"`
		Sequence float64 `json:"sequence"`
		Away     bool    `json:"away"`
	}
	if json.Unmarshal(raw, &presence) != nil || presence.PageID == "" || len(presence.PageID) > 512 {
		return nil
	}
	kept := map[string]any{"pageId": presence.PageID, "sequence": presence.Sequence, "reader": true}
	if presence.Away {
		kept["away"] = true
	}
	answer, err := json.Marshal(map[string]any{pagePresenceAwarenessField: kept})
	if err != nil {
		return nil
	}
	return answer
}
