package websocket

import "context"

// FlushRoom stores what a resident, loaded room has taken and not stored yet
// — its coalesced batch and the updates queued behind it — through the
// room's persistence, and returns once that write is done. The room and its
// persistence worker stay as they were; a room that is not resident, still
// loading, or without persistence has nothing to store. It reports false when
// the store failed (the batch is kept for the worker's next flush) or when ctx
// ended first. (Leafen fork: a change the application stores itself, which
// builds on a peer's edits the room took but has not stored, is stored after
// them.)
func (s *Server) FlushRoom(ctx context.Context, room string) bool {
	if ctx.Err() != nil {
		return false
	}
	s.rmu.RLock()
	rm, ok := s.rooms[room]
	s.rmu.RUnlock()
	if !ok {
		return true
	}
	select {
	case <-rm.ready:
	default:
		return true
	}
	if rm.loadErr != nil || rm.flushReq == nil {
		return true
	}
	// The ack is buffered, so the worker never waits to report.
	ack := make(chan bool, 1)
	select {
	case rm.flushReq <- ack:
	case <-rm.persistDone:
		// The worker is gone; its last drain stored what it held.
		return true
	case <-ctx.Done():
		return false
	}
	select {
	case stored := <-ack:
		return stored
	case <-ctx.Done():
		return false
	}
}
