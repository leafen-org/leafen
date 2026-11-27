package app

import (
	"errors"
	"runtime"
	"strings"
	"sync"
	"time"
	"weak"

	"github.com/reearth/ygo/crdt"
	ygowebsocket "github.com/reearth/ygo/provider/websocket"
)

// Text written in the editor goes through the organization's DLP policy as
// every other write does (2026-10-16: a sensitive term typed in the editor
// was stored while the same term through the API was refused): a write that
// would add a sensitive term to a block's text is dropped and the connection
// closed with ygoDLPRefusedCloseCode, so the page lets go of that text and
// says why. The collaboration server asks it of each write of a writable
// peer, as upstream Ygo's Server.OnPeerUpdate does, before the room applies
// it.
const (
	ygoDLPRefusedCloseCode = 4451
	ygoDLPRefusedReason    = "dlp"
	// How long the policy read for a workspace is kept before it is read again.
	ygoDLPPolicyRefresh = 5 * time.Second
)

// OnPeerUpdate checks a writable peer's write against the DLP policy of the
// room's workspace's organization, and applies it when the policy lets it
// through. The policy is read again every few seconds, so turning blocking on
// reaches open pages; while it doesn't block, a write costs nothing more.
func (a *ygoRoomAuthorization) OnPeerUpdate(info ygowebsocket.PeerUpdateInfo, apply func() error) error {
	if organizationPolicy.DLP == nil {
		return apply()
	}
	workspaceID, found := a.roomWorkspace(info.Room)
	if !found {
		// A room of no workspace known here takes no write.
		return errYgoRoomUnknown
	}
	if ygoDLPRefuses(a.dlpPolicy(workspaceID), info.Doc, info.Update) {
		return &ygowebsocket.PeerClose{Code: ygoDLPRefusedCloseCode, Reason: ygoDLPRefusedReason}
	}
	return apply()
}

var errYgoRoomUnknown = errors.New("the room's workspace isn't known")

// roomWorkspace is the workspace of a room: the one it was opened in, or its
// page's.
func (a *ygoRoomAuthorization) roomWorkspace(room string) (string, bool) {
	a.mu.Lock()
	binding, bound := a.roomBindings[room]
	a.mu.Unlock()
	if bound {
		return binding.workspaceID, true
	}
	page, err := findRecordRemembered(a.app, pagesCollection, room)
	if err != nil || page.GetString("workspace") == "" {
		return "", false
	}
	return page.GetString("workspace"), true
}

// ygoDLPPolicy is a workspace's organization's policy as last read.
type ygoDLPPolicy struct {
	mu      sync.Mutex
	scanner organizationDLPContentScanner
	readAt  time.Time
}

// dlpPolicy is a workspace's organization's DLP policy, read again every few
// seconds; a policy that can't be read keeps the one read before.
func (a *ygoRoomAuthorization) dlpPolicy(workspaceID string) organizationDLPContentScanner {
	found, _ := a.dlpPolicies.LoadOrStore(workspaceID, &ygoDLPPolicy{})
	policy := found.(*ygoDLPPolicy)
	policy.mu.Lock()
	defer policy.mu.Unlock()
	if time.Since(policy.readAt) < ygoDLPPolicyRefresh {
		return policy.scanner
	}
	workspace, err := findRecordRemembered(a.app, workspacesCollection, workspaceID)
	if err != nil {
		return policy.scanner
	}
	next, err := newOrganizationDLPContentScanner(a.app, workspace.GetString("organization"))
	if err != nil {
		return policy.scanner
	}
	policy.scanner, policy.readAt = next, time.Now()
	return policy.scanner
}

// ygoDLPRefuses tells whether a write would add a sensitive term to a
// block's text. Only a term the write adds: a block that held one before the
// policy blocked it still takes edits.
func ygoDLPRefuses(check organizationDLPContentScanner, doc *crdt.Doc, update []byte) bool {
	if !check.blocks() {
		ygoDLPLetShadowGo(doc)
		return false
	}
	return ygoDLPShadowFor(doc).refuses(check, doc, update)
}

// A room's shadow is a copy of its document that follows it: each change the
// room makes is applied to the shadow too, a change's own size, and a write
// is checked by applying it to the shadow and reading the blocks it changed.
// Copied for each write, the room's whole document was encoded and applied
// for every character typed on a page while DLP blocked (2026-10-19). A
// write applied to the shadow comes again from the room once the room takes
// it, which changes nothing; one refused, or one the shadow couldn't take,
// leaves the shadow to be copied again at the next check. The shadow goes
// with its room's document, and while DLP doesn't block.
type ygoDLPShadow struct {
	mu  sync.Mutex
	doc *crdt.Doc
}

var ygoDLPShadows sync.Map // weak.Pointer[crdt.Doc] -> *ygoDLPShadow

func ygoDLPShadowFor(room *crdt.Doc) *ygoDLPShadow {
	key := weak.Make(room)
	if found, ok := ygoDLPShadows.Load(key); ok {
		return found.(*ygoDLPShadow)
	}
	shadow := &ygoDLPShadow{}
	if found, loaded := ygoDLPShadows.LoadOrStore(key, shadow); loaded {
		return found.(*ygoDLPShadow)
	}
	room.OnUpdate(func(update []byte, _ any) { shadow.follow(update) })
	runtime.AddCleanup(room, func(key weak.Pointer[crdt.Doc]) { ygoDLPShadows.Delete(key) }, key)
	return shadow
}

func ygoDLPLetShadowGo(room *crdt.Doc) {
	if found, ok := ygoDLPShadows.Load(weak.Make(room)); ok {
		shadow := found.(*ygoDLPShadow)
		shadow.mu.Lock()
		shadow.doc = nil
		shadow.mu.Unlock()
	}
}

func ygoIsBlockTextRoot(name string) bool { return strings.HasPrefix(name, ygoBlockTextRootPrefix) }

// follow applies a change the room made; one the shadow can't take leaves
// it to be copied again.
func (shadow *ygoDLPShadow) follow(update []byte) {
	shadow.mu.Lock()
	defer shadow.mu.Unlock()
	if shadow.doc != nil && crdt.ApplyUpdateV1(shadow.doc, update, nil) != nil {
		shadow.doc = nil
	}
}

func (shadow *ygoDLPShadow) refuses(check organizationDLPContentScanner, room *crdt.Doc, update []byte) bool {
	shadow.mu.Lock()
	defer shadow.mu.Unlock()
	if shadow.doc == nil {
		copied := crdt.New(crdt.WithTextRoots(ygoIsBlockTextRoot))
		if err := crdt.ApplyUpdateV1(copied, crdt.EncodeStateAsUpdateV1(room, nil), nil); err != nil {
			return false
		}
		shadow.doc = copied
	}
	var changed []string
	stop := shadow.doc.OnUpdateDetails(func(details crdt.UpdateDetails) {
		for _, change := range details.TextChanges {
			if ygoIsBlockTextRoot(change.Name) {
				changed = append(changed, change.Name)
			}
		}
	})
	err := crdt.ApplyUpdateV1(shadow.doc, update, nil)
	stop()
	// A write that doesn't apply is the room's to handle.
	if err != nil {
		shadow.doc = nil
		return false
	}
	for _, name := range changed {
		if check.enforce(shadow.doc.GetText(name).ToString()) != nil && check.enforce(room.GetText(name).ToString()) == nil {
			shadow.doc = nil
			return true
		}
	}
	return false
}
