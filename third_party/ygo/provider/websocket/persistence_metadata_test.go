package websocket

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	gws "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/reearth/ygo/crdt"
)

type metadataBatchAdapter struct {
	mu      sync.Mutex
	batches [][]PersistenceUpdate
	calls   int
	failAt  int
}

func (a *metadataBatchAdapter) LoadDoc(string) ([]byte, error) { return nil, nil }
func (a *metadataBatchAdapter) StoreUpdate(string, []byte) error {
	return nil
}
func (a *metadataBatchAdapter) StoreUpdateBatchContext(
	_ context.Context,
	_ string,
	updates []PersistenceUpdate,
) error {
	a.mu.Lock()
	a.calls++
	if a.failAt > 0 && a.calls == a.failAt {
		a.mu.Unlock()
		return errors.New("planned attributed batch failure")
	}
	a.mu.Unlock()
	copyBatch := make([]PersistenceUpdate, len(updates))
	for index, update := range updates {
		copyBatch[index] = PersistenceUpdate{
			Update: append([]byte(nil), update.Update...), Metadata: update.Metadata,
		}
	}
	a.mu.Lock()
	a.batches = append(a.batches, copyBatch)
	a.mu.Unlock()
	return nil
}

func (a *metadataBatchAdapter) snapshot() [][]PersistenceUpdate {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([][]PersistenceUpdate(nil), a.batches...)
}

func dialWSWithActor(t *testing.T, server *httptest.Server, room, actor string) *gws.Conn {
	t.Helper()
	header := http.Header{"X-Test-Actor": []string{actor}}
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/" + room
	connection, _, err := gws.DefaultDialer.Dial(url, header)
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	return connection
}

func attributedTextUpdates(t *testing.T) ([]byte, []byte) {
	t.Helper()
	document := crdt.New()
	text := document.GetText("t")
	updates := make([][]byte, 0, 2)
	unsubscribe := document.OnUpdate(func(update []byte, _ any) {
		updates = append(updates, append([]byte(nil), update...))
	})
	document.Transact(func(transaction *crdt.Transaction) {
		text.Insert(transaction, 0, "a", nil)
	})
	document.Transact(func(transaction *crdt.Transaction) {
		text.Insert(transaction, 1, "b", nil)
	})
	unsubscribe()
	require.Len(t, updates, 2)
	return updates[0], updates[1]
}

func TestPersistMetadata_MixedPeersRemainOrderedInOneBatch(t *testing.T) {
	adapter := &metadataBatchAdapter{}
	clock := newFakeClock()
	server := newCoalesceServer(adapter, clock, 50*time.Millisecond, 500*time.Millisecond)
	server.PersistCoalesceMaxUpdates = 8
	server.PersistCoalesceMaxBytes = 1 << 20
	server.Authorize = func(request *http.Request) (ConnectionConfig, bool) {
		return ConnectionConfig{PersistenceMetadata: request.Header.Get("X-Test-Actor")}, true
	}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()

	firstPeer := dialWSWithActor(t, httpServer, "metadata-room", "actor-a")
	secondPeer := dialWSWithActor(t, httpServer, "metadata-room", "actor-b")
	drainWS(t, firstPeer, crdt.New())
	drainWS(t, secondPeer, crdt.New())
	first, second := attributedTextUpdates(t)

	sendV1Update(t, firstPeer, first)
	clock.nextTimerOfDur(t, 50*time.Millisecond)
	sendV1Update(t, secondPeer, second)
	window := clock.nextTimerOfDur(t, 50*time.Millisecond)
	window.fire()

	require.Eventually(t, func() bool {
		return len(adapter.snapshot()) == 1
	}, time.Second, 5*time.Millisecond)
	batches := adapter.snapshot()
	require.Len(t, batches[0], 2)
	assert.Equal(t, "actor-a", batches[0][0].Metadata)
	assert.Equal(t, "actor-b", batches[0][1].Metadata)
}

func TestPersistMetadata_AttributedStoreRespectsCollectionBounds(t *testing.T) {
	updates := []PersistenceUpdate{
		{Update: []byte{1, 2}, Metadata: "actor-a"},
		{Update: []byte{3, 4}, Metadata: "actor-b"},
	}
	for _, testCase := range []struct {
		name       string
		maxUpdates int
		maxBytes   int
	}{
		{name: "item bound", maxUpdates: 1, maxBytes: 1 << 20},
		{name: "byte bound", maxUpdates: 8, maxBytes: 3},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			adapter := &metadataBatchAdapter{}
			server := &Server{
				persistence:               adapter,
				PersistCoalesceMaxUpdates: testCase.maxUpdates,
				PersistCoalesceMaxBytes:   testCase.maxBytes,
			}
			stored, err := server.storePersistenceUpdates(context.Background(), "room", updates)
			require.NoError(t, err)
			assert.Equal(t, len(updates), stored)

			batches := adapter.snapshot()
			require.Len(t, batches, 2)
			assert.Equal(t, "actor-a", batches[0][0].Metadata)
			assert.Equal(t, "actor-b", batches[1][0].Metadata)
		})
	}
}

func TestPersistMetadata_AttributedStoreReportsCommittedPrefix(t *testing.T) {
	adapter := &metadataBatchAdapter{failAt: 2}
	server := &Server{
		persistence:               adapter,
		PersistCoalesceMaxUpdates: 1,
		PersistCoalesceMaxBytes:   1 << 20,
	}
	updates := []PersistenceUpdate{
		{Update: []byte{1}, Metadata: "actor-a"},
		{Update: []byte{2}, Metadata: "actor-b"},
	}

	stored, err := server.storePersistenceUpdates(context.Background(), "room", updates)
	require.Error(t, err)
	assert.Equal(t, 1, stored)
	require.Len(t, adapter.snapshot(), 1)
	assert.Equal(t, "actor-a", adapter.snapshot()[0][0].Metadata)

	stored, err = server.storePersistenceUpdates(context.Background(), "room", updates[stored:])
	require.NoError(t, err)
	assert.Equal(t, 1, stored)
	require.Len(t, adapter.snapshot(), 2)
	assert.Equal(t, "actor-b", adapter.snapshot()[1][0].Metadata)
}
