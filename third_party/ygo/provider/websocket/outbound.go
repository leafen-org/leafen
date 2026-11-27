package websocket

import (
	"github.com/reearth/ygo/encoding"
	ygsync "github.com/reearth/ygo/sync"
)

// rewriteOutbound passes the document update a frame carries to this peer —
// a Sync or SyncReply frame's step-2 or update — through the connection's
// OutboundUpdate, and answers the frame with the update it gave back. Every
// other frame, and one whose framing doesn't decode, goes as it is. The
// frame is shared with the room's other peers, so a changed one is a new
// frame. (Leafen fork.)
func (p *peer) rewriteOutbound(data []byte) []byte {
	dec := encoding.NewDecoder(data)
	outer, err := dec.ReadVarUint()
	if err != nil || outer != msgSync && outer != msgSyncReply {
		return data
	}
	subType, update, err := ygsync.ReadSyncMessage(dec.RemainingBytes())
	if err != nil || subType != ygsync.MsgSyncStep2 && subType != ygsync.MsgUpdate {
		return data
	}
	next := p.outboundUpdate(p.room.doc, update)
	if len(next) == len(update) && (len(next) == 0 || &next[0] == &update[0]) {
		return data
	}
	return encoding.EncodeBytes(func(enc *encoding.Encoder) {
		enc.WriteVarUint(outer)
		enc.WriteVarUint(uint64(subType))
		enc.WriteVarBytes(next)
	})
}
