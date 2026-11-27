package websocket

import (
	"slices"

	"github.com/reearth/ygo/awareness"
	"github.com/reearth/ygo/encoding"
)

// withOwnState adds the room's own awareness client to what an injected
// awareness frame goes on to the room's peers as, when the frame names that
// client with a state and the room has one (Leafen fork). The application
// announces the room's own state this way: it sets it with
// Awareness.SetLocalState, which reaches no peer, and injects the room's
// encoding of it, which the room already holds at that clock and so doesn't
// apply. Since v1.55 upstream sends on only what the room applied, so the
// announcement would reach no one; the peers are sent the room's own state
// as the room holds it.
func withOwnState(aw *awareness.Awareness, frame []byte, res awareness.ApplyResult) awareness.ApplyResult {
	own := aw.ClientID()
	if slices.Contains(res.Applied, own) || aw.GetLocalState() == nil {
		return res
	}
	dec := encoding.NewDecoder(frame)
	n, err := dec.ReadVarUint()
	if err != nil {
		return res
	}
	for i := uint64(0); i < n; i++ {
		clientID, err := dec.ReadVarUint()
		if err != nil {
			return res
		}
		if _, err := dec.ReadVarUint(); err != nil { // clock
			return res
		}
		state, err := dec.ReadVarBytes()
		if err != nil {
			return res
		}
		if clientID == own && len(state) > 0 && string(state) != "null" {
			res.Applied = append(res.Applied, own)
			res.Altered = true
			return res
		}
	}
	return res
}
