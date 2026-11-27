package crdt

// beforeClocks gives the clocks writers had before a transaction to a walk
// over a type's items that asks for them item after item. The items come in
// stretches of a few writers', so the clocks asked last are kept in a short
// list looked through before the state vector's map, which took most of
// such a walk on a block several people typed in for hours (Leafen,
// 2026-10-18: computeDelta 0.64 ms an update on a fresh room, 5.8 ms on one
// open six hours).
type beforeClocks struct {
	state StateVector
	seen  [8]struct {
		client ClientID
		clock  uint64
	}
	count int
}

func newBeforeClocks(state StateVector) *beforeClocks {
	return &beforeClocks{state: state}
}

// clock is the client's clock before the transaction, as state.Clock gives it.
func (clocks *beforeClocks) clock(client ClientID) uint64 {
	for index := 0; index < clocks.count; index++ {
		if clocks.seen[index].client == client {
			return clocks.seen[index].clock
		}
	}
	clock := clocks.state.Clock(client)
	if clocks.count < len(clocks.seen) {
		clocks.seen[clocks.count].client, clocks.seen[clocks.count].clock = client, clock
		clocks.count++
	}
	return clock
}
