package app

import (
	"strings"
	"time"

	ygowebsocket "github.com/reearth/ygo/provider/websocket"
	"golang.org/x/time/rate"
)

const (
	collaborationMaxAwarenessBytesPerRoom   int64 = 100 << 20
	collaborationMaxAwarenessClientsPerRoom       = 10_000
	collaborationAwarenessExpiry                  = 30 * time.Second
	collaborationPersistBatchUpdates              = 64
	collaborationPersistBatchBytes                = 8 << 20

	// What one connection may send (2026-10-18; Ygo leaves each unlimited
	// or at 64 MiB). The largest message a person's editor sends is a sync
	// carrying an offline device's whole page or a very large paste, a few
	// MiB; the editor sends one update a transaction and presence at most a
	// frame, well under 100 a second; a reconnect's catch-up fits the
	// bursts. Over a limit the connection is closed and the editor
	// reconnects and syncs again, so no change is lost.
	collaborationMaxMessageBytes   int64 = 32 << 20
	collaborationMessagesPerSecond       = 100
	collaborationMessageBurst            = 500
	collaborationBytesPerSecond          = 8 << 20
	collaborationByteBurst               = 64 << 20
)

// newCollaborationServer is the sole native Ygo construction boundary.
// What one connection may send is bounded here; how many connections one
// person holds is bounded where sockets open (ygo_authorization.go), and
// the cgroup working-set controller guards the rest. Provider-documented
// awareness safety bounds remain owned and enforced by Ygo rather than a
// parallel Leafen counter or expiry loop.
func newCollaborationServer(persistence ygowebsocket.PersistenceAdapter) *ygowebsocket.Server {
	server := ygowebsocket.NewServerWithPersistence(persistence)
	server.MaxAwarenessBytesPerRoom = collaborationMaxAwarenessBytesPerRoom
	server.MaxAwarenessClientsPerRoom = collaborationMaxAwarenessClientsPerRoom
	server.AwarenessExpiry = collaborationAwarenessExpiry
	server.PersistCoalesceMaxUpdates = collaborationPersistBatchUpdates
	server.PersistCoalesceMaxBytes = collaborationPersistBatchBytes
	server.MaxMessageBytes = collaborationMaxMessageBytes
	server.MessageRateLimit = rate.Limit(collaborationMessagesPerSecond)
	server.MessageRateBurst = collaborationMessageBurst
	server.MessageByteRateLimit = rate.Limit(collaborationBytesPerSecond)
	server.MessageByteBurst = collaborationByteBurst
	// Block text is text from the start, so what people type reaches page
	// history as text changes.
	server.TextRoots = func(name string) bool { return strings.HasPrefix(name, ygoBlockTextRootPrefix) }
	// Where a page's socket may come from is asked as it comes in
	// (ygoRoomAuthorization.ServeHTTP): the server's own page, or the
	// packaged client's.
	server.AllowedOrigins = []string{"*"}
	if nativePersistence, ok := persistence.(*pocketBaseYgoPersistence); ok {
		nativePersistence.settlement.bindCollaborationServer(server)
	}
	return server
}
