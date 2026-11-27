# Leafen Ygo fork

This directory is a source-preserving local fork of
`github.com/reearth/ygo@v1.57.1` (merged by hand from v1.54.0 on
2026-11-26). Leafen carries it because the upstream
WebSocket persistence API drops the authenticated connection origin before
coalesced persistence, while Leafen must commit Ygo text and its server-trusted
actor provenance atomically.

The intentional delta is limited to `provider/websocket`: connection metadata,
ordered attributed persistence batches, finite batch item/byte collection,
`Server.ApplyStored`, which brings a change Leafen's server already stored into
an open room and its peers without the room storing it again, and
`Server.TextRoots`, with its `crdt.WithTextRoots` document option, which
creates a room's block text roots as text so its persistence updates carry
their text changes, and `ConnectionConfig.WriteGate`, which the server asks
before applying each document write of a writable peer, so a write that
arrives after the peer's access changed is answered like a read-only peer's
(in `applySyncUpdate`, before `Server.OnPeerUpdate` and the room's own apply).
A write the gate drops that the room doesn't hold leaves the client's later
edits building on it, which the room couldn't integrate — the reason upstream
closes a peer whose update `OnPeerUpdate` refused — so that peer is closed
with 4205 once the gate lets its writes through again, and it reconnects and
sends them all; and `StatelessInfo.Identity`: upstream's
`ConnectionConfig.Identity` (v1.56.0), which upstream hands to `OnPeerUpdate`
with each update, comes with each stateless message of that connection too,
so the application knows who sent it, and
`ConnectionConfig.ReadOnlyPresence`, which lets a read-only peer's awareness
through while its document writes stay dropped, so a reader shows among the
people on the page, and `ConnectionConfig.AwarenessState`, which rewrites each
awareness state such a peer sends before the room applies it, so a reader is
counted on the page without being named; and each room's awareness takes a
client ID of its own in place of upstream's 0, so what a room that started
over tells its peers isn't taken for old changes of a client they knew at a
later clock. And
`ConnectionConfig.OutboundUpdate`, which is given each document update a
peer is sent (its step-2 and every update after, `outbound.go`) and answers
the update to send instead, so Leafen sends each reader a page's mentions as
that reader may read them, with `crdt.RewriteTextRunsV1` (`crdt/text_runs.go`),
which rewrites the strings an update carries into a text, told their root
and formatting, every length kept. And `Server.SendServerFrame`, which sends
a room's peers a frame of the application's own tag (`ServerFrameTagMin` and
above) in turn with the room's other frames: only the server sends those, as
a peer's frames of such tags are never carried on, where any peer may
broadcast a Stateless message to the others. And `crdt.UpdateRootsV1`
(`crdt/update_roots.go`), which names the root types an update writes into,
read from its own structs or the document's items they follow, so a page's
history names the blocks of an update stored ahead of the text it builds on,
which the document can't integrate yet. And `Server.FlushRoom`
(`provider/websocket/flush_room.go`), which has a room's persistence worker
store what the room took and hasn't stored yet through the worker's own
on-demand flush, the room and the worker kept, and returns once it is
stored: Leafen stores a page's typing ahead of a block change its editor
sends after it, one more write per change while the room holds typing.
And `Transaction.ChangedKeys` (`crdt/transaction.go`), which names the keys
of a map a transaction wrote or deleted — Yjs's public
`transaction.changed` — so a page's history, replaying a version's updates
from an `OnAfterTransaction` callback, reads only the blocks each update
changed; upstream's map observer tells the keys too but walks the map's items
for each one's old value. And `Transaction.ChangedRoots`
(`crdt/transaction_changed_roots.go`), which names the root types a
transaction changed, deletions included — the roots of the same
`transaction.changed` — so a page's history, planning a revert, reads again
only the blocks whose text or order each replayed version changed. And
`Sink.Inject`'s awareness frame naming the room's own client
(`provider/websocket/own_awareness.go`): the application announces the
room's own state by setting it (`Awareness.SetLocalState`) and injecting
the room's encoding of it, which the room already holds and doesn't apply;
since v1.55.0 upstream sends a room's peers only what the room applied, so
the peers are sent the room's own state as the room holds it.

Leafen uses upstream's own API where it serves (checked against v1.54.0's
public API on 2026-10-23, and again against v1.57.1's on 2026-11-26):

- The organization's DLP check of editor writes is upstream's
  `Server.OnPeerUpdate`, refusing with `PeerClose{4451, "dlp"}`; the fork's
  per-connection write check is gone.
- Where a socket may come from is checked by Leafen as it comes in (the
  server's own page, the packaged client's, an app frame's "null"), and the
  servers take upstream's `AllowedOrigins = ["*"]`, which upstream documents
  for sockets that carry their sign-in as an explicit token, as Leafen's do;
  the fork's origin list beside the same-origin check is gone.

What stays has no upstream counterpart:

- `WriteGate`: `OnPeerUpdate` is one hook for the server whose origin is the
  unexported peer, so it can't tell which connection wrote, and a refusal
  always closes the peer; the gate answers by the connection's own access and
  keeps the peer while a fence holds.
- `ApplyStored`: upstream's `Apply` and `BroadcastUpdate` either store the
  change again through the room's persistence or leave the room's document
  without it.
- Connection metadata and attributed persistence batches: upstream drops the
  connection before coalesced persistence, and the time each update came in.
- `Server.BroadcastStateless`: upstream only receives stateless messages.
- `FlushRoom`: upstream flushes a room's batch on demand only as it evicts
  the room (`CloseRoom`, an idle sweep, the last peer leaving).
- `OutboundUpdate`: upstream sends every peer of a room the same bytes.
- `StatelessInfo.Identity`: upstream hands the connection's identity to
  `OnPeerUpdate` only.
- The room's own announced state (`withOwnState`): upstream's `Inject`
  sends on only the awareness entries the room applied.
- `ReadOnlyPresence`, `AwarenessState`, `TextRoots`, and the
  per-peer byte rate and persistence batch count and size bounds: upstream has
  the message rate and the coalescing window and wait only.

Outside `provider/websocket`, `crdt` carries the document options the
server's persistence uses — `WithTextRoots` and `Doc.OnUpdateDetails`
(`crdt/doc.go`), which name the named text roots a transaction changed,
without computing their deltas (walking a text for one cost each update the
text's whole length, and the server reads the text from the update), called
as `OnUpdate` is — only for a transaction that adds or deletes something,
with the same update, right after the `OnUpdate` observers — and
`EncodeStateAsUpdateV1Shared` (`crdt/update.go`), the whole state encoded
once until the document next changes, which the server sends each peer that
joins or resyncs; a transaction that leaves the store as it was (a peer's
sync reply with nothing new) keeps it. The rest are changes of speed, not of
result. Applying updates out of order, the pending queues and inferring
parents in a merge are upstream's own since v1.51.12. A deletion, which
upstream walks through parent links since v1.51.13, notes each deleted item
in the store's kept delete set (`StructStore.noteDeleted`, below).

Encoding an update no longer walks the whole store (Leafen, 2026-10-19: on
a block five people typed in for hours, most of each keystroke): a client's
items newer than a state vector are found by binary search, as upstream does
too since v1.57.0, and the store keeps its delete set, sorted and compact, as
items are deleted or stored deleted, instead of rebuilding it from every item
for each state update, as upstream still does in one pass
(`StructStore.deleteSet`, which `EncodeStateAsUpdateV1`, its shared form,
`EncodeChangesSinceV1` and the V2 state encode read). A re-merged or
squashed item is found in its client's items by binary search
(`StructStore.indexOf`). One keystroke into a block of 100,000 pieces took
2 ms and now 2 µs, as on a block of 100; the encoded updates and states are
the same bytes.

A text's and an array's change delta (`computeDelta`, `YArray`'s walk) look
up the clock each item's writer had before the transaction in a short list
of the writers asked last (`crdt/before_clocks.go`) before the state
vector's map, with the same results: on a block several people typed in for
hours the map lookups were most of each update's delta (Leafen, 2026-10-18).

A committed transaction's update (`OnUpdate`, `OnUpdateDetails`) carries
the items it added and only what it deleted, as Yjs's update event writes the
transaction's own delete set: Leafen's change of 2026-10-20 (on a block with
10,000 deleted pieces one keystroke's update was 29,709 bytes instead of 14)
is upstream's own since v1.57.0. A state update (`EncodeStateAsUpdateV1`)
still carries the whole delete set, as in Yjs. The kept state encoding is
dropped as a transaction commits (`Doc.commitAndUnlock`) unless the
transaction left the store as it was. `MarkChanges` and `EncodeChangesSinceV1`
(`crdt/change_mark.go`) encode what a document changed since a moment — the
items after its state vector then and the deletions made since — which is
the merge of its transactions' updates, for a server that edits a loaded
document and stores the change.

`RewriteUpdateV1` (`crdt/rewrite.go`, Leafen, 2026-10-21) passes the
string values and map keys of an update's structs through a function, decoded
without integrating and encoded again as `MergeUpdatesV1` does, so the IDs,
clocks and lengths stay: a workspace moved to another server renames, inside
its pages' history, the people paired with accounts there.

All other copied files remain upstream verification input.

To reconstruct the pinned upstream tree in a temporary directory without
touching this fork, run from `product/`:

```sh
tmp_root="$(mktemp -d)"
trap 'rm -rf "$tmp_root"' EXIT
GOMODCACHE="$tmp_root/mod" go mod download github.com/reearth/ygo@v1.57.1
diff -ru \
  --exclude LEAFEN_UPSTREAM.md \
  "$tmp_root/mod/github.com/reearth/ygo@v1.57.1" \
  third_party/ygo
```

Review every reported difference. Do not overwrite the fork with that command;
apply a new upstream version and the Leafen delta through the ordinary guarded
issue loop. The shell trap removes only the newly created temporary root when
the comparison shell exits.

The upstream MIT license is retained as `LICENSE`.
