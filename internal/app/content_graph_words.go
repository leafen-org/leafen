package app

import (
	"encoding/base64"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A page opened reads its blocks with its words as they stand at the same
// moment: its open room's text, or its stored text when no room is open,
// taken once the read's snapshot has begun. Read apart, as stored, they were
// the room's of up to ten seconds before (a room stores what it takes a
// moment later, its writes coalesced): a page opened while someone typed
// drew their line some 80 characters short until the room caught it up
// (2026-11-13, a NAS-class server).

// pageWordsMaxBytes bounds the words a page's read carries, which the page's
// first draw waits for: a page whose text is larger reads it apart, as
// before.
const pageWordsMaxBytes = 64 << 10

// pageWordsAsRead is the page's text as one Yjs update, as its reader reads
// it (a mention closed to them in marks, as its room's socket sends it), or
// empty when it is larger than the read carries; and the moment of the
// block changes whose text it holds, which the read's blocks must be of.
func pageWordsAsRead(app core.App, readerID, pageID string) (string, pageWordsMoment, error) {
	moment := pageWordsMoment{}
	document := openPageTextRoom(app, pageID)
	var update []byte
	if document == nil {
		stored, found, err := loadYgoDoc(app, pageID)
		if err != nil || !found {
			return "", moment, err
		}
		document = stored
		update = crdt.EncodeStateAsUpdateV1(document, nil)
	} else {
		update, moment = pageWordsOfRoom(pageID, document)
	}
	if len(update) > pageWordsMaxBytes {
		return "", moment, nil
	}
	update = collaborationReaderUpdates(app, readerID)(document, update)
	return base64.StdEncoding.EncodeToString(update), moment, nil
}

// The text a block change carries reaches the page's open room only once
// the change is committed (content_graph_mutation_words.go), while a read's
// blocks are of the moment its snapshot began, which may be before that
// commit or after it with the room not yet holding its text: a page opened
// in a run of merges and Undos drew a merged line beside its emptied one
// (2026-11-13). The room's words are taken with the moment of the changes
// whose text it holds, and a read whose blocks are of another reads again.

// pageWordsReads bounds the times a page's first window is read for its
// blocks and words to be of one moment; the last goes as it is.
const pageWordsReads = 3

// pageWordsMoment is the moment of the block changes whose text a page's
// open room held as its words were taken: the last whose text it took, and
// the one whose text it was about to take.
type pageWordsMoment struct{ held, coming int }

// agrees is whether blocks read at a graph revision are of the moment.
func (moment pageWordsMoment) agrees(revision int) bool {
	return moment.held <= revision && (moment.coming == 0 || moment.coming > revision)
}

type pageWordsRoomMoment struct {
	mu sync.Mutex
	pageWordsMoment
}

// pageWordsRoomMoments are the moments of the pages whose open room took a
// block change's text lately.
var pageWordsRoomMoments sync.Map // page ID -> *pageWordsRoomMoment

// pageWordsMomentKept is how long a page's moment is kept once its room took
// a change's text: past it, a read's snapshot began after.
const pageWordsMomentKept = 30 * time.Second

func pageWordsOfRoom(pageID string, document *crdt.Doc) ([]byte, pageWordsMoment) {
	found, ok := pageWordsRoomMoments.Load(pageID)
	if !ok {
		return crdt.EncodeStateAsUpdateV1(document, nil), pageWordsMoment{}
	}
	room := found.(*pageWordsRoomMoment)
	room.mu.Lock()
	defer room.mu.Unlock()
	return crdt.EncodeStateAsUpdateV1(document, nil), room.pageWordsMoment
}

// publishGraphWordsAfterCommit brings the text a block change carried into
// the page's open room once the change, at cursor, is committed, after the
// text the server changed with it (serverText) the change published before.
// From before the commit until the room holds them, a read that takes the
// room's words reads its blocks again if they are of the change.
func publishGraphWordsAfterCommit(app core.App, pageID string, cursor int, words []byte, serverText bool) {
	transaction := app.TxInfo()
	if len(words) == 0 && !serverText || transaction == nil {
		publishStoredYgoUpdateAfterCommit(app, pageID, words)
		return
	}
	var room *pageWordsRoomMoment
	for {
		found, _ := pageWordsRoomMoments.LoadOrStore(pageID, &pageWordsRoomMoment{})
		room = found.(*pageWordsRoomMoment)
		room.mu.Lock()
		// One let go of meanwhile is no longer read: the page's new one is.
		if current, _ := pageWordsRoomMoments.Load(pageID); current == found {
			break
		}
		room.mu.Unlock()
	}
	room.coming = cursor
	room.mu.Unlock()
	transaction.OnComplete(func(err error) error {
		room.mu.Lock()
		defer room.mu.Unlock()
		room.coming = 0
		if err != nil {
			return nil
		}
		if len(words) > 0 {
			applyStoredYgoUpdateToRoom(app, pageID, words)
		}
		room.held = max(room.held, cursor)
		time.AfterFunc(pageWordsMomentKept, func() {
			room.mu.Lock()
			defer room.mu.Unlock()
			if room.held == cursor && room.coming == 0 {
				pageWordsRoomMoments.CompareAndDelete(pageID, room)
			}
		})
		return nil
	})
}
