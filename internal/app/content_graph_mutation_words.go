package app

import (
	"context"
	"encoding/base64"
	"net/http"
	"slices"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A change of a page's blocks made in its editor — a line merged into the one
// above, a line split by Enter, a line put back by ⌘Z — carries the
// collaborative text written with it, one Yjs update the editor held for it
// (web lib/textBehindBlockChanges.ts), as "words". The server stores that
// text in the change's own transaction, as the page's socket stores the text
// it is sent: through the organization's DLP policy, in the page's history,
// as its writer's edit and for search. Once committed, the page's open room
// takes it, after it is told the graph's new position. Sent apart, the text
// reached the server a moment after its blocks: a page opened in that moment
// drew a merged line beside its emptied one, or a line put back without its
// place (2026-11-13). A change without words goes as before, and the receipt
// says whether its words were stored (wordsStored): those of a change
// already made are not, and its writer sends them through the room.
//
// The text the editor wrote before the change, which waited for the block
// changes before it, goes with it apart, as "typing": stored first in the
// change's transaction, before the change does anything, as the typing it
// is — a version of its own before the change's, or the minute's typing
// version it goes on in (page_history_block_change_text.go). Merged into
// the change's words, it was the change's own text: a line typed in the
// second after Enter and removed with the block menu went in one update
// with its emptying, so its words were in no version, and reverting the
// removal brought the line back empty (2026-11-18, a NAS-class server).

// graphMutationWordsMaxBytes bounds the words one change carries.
const graphMutationWordsMaxBytes = 4 << 20

// takeGraphMutationText reads a change's words and the typing before it, and
// takes them off the input, which is known by the rest.
func takeGraphMutationText(input *graphMutationInput) (words, typing []byte, err error) {
	if words, err = decodeGraphMutationWords(input.Words); err != nil {
		return nil, nil, err
	}
	if typing, err = decodeGraphMutationWords(input.Typing); err != nil {
		return nil, nil, err
	}
	input.Words, input.Typing = "", ""
	return words, typing, nil
}

// storeGraphMutationTyping stores the typing a change carries from before it,
// first in the change's transaction, as the words are (storeGraphMutationWords)
// but as typing in the page's history; once committed, the page's open room
// takes it, ahead of the change's words.
func storeGraphMutationTyping(
	ctx context.Context, app core.App, principalID string, authorization pageAuthorization, typing []byte,
) (ygoTextStored, error) {
	if len(typing) == 0 {
		return ygoTextStored{}, nil
	}
	stored := ygoTextStored{}
	if err := asTypingBeforeBlockChange(app, func() (err error) {
		stored, err = storeGraphMutationWords(ctx, app, principalID, authorization, typing)
		return err
	}); err != nil {
		return ygoTextStored{}, err
	}
	publishStoredYgoUpdateAfterCommit(app, authorization.Page.Id, typing)
	return stored, nil
}

// and is what storing other after stored changed, as one.
func (stored ygoTextStored) and(other ygoTextStored) ygoTextStored {
	if !stored.changed {
		return other
	}
	if !other.changed {
		return stored
	}
	if other.actorID == "" {
		other.actorID = stored.actorID
	}
	for _, block := range stored.textBlocks {
		if !slices.Contains(other.textBlocks, block) {
			other.textBlocks = append(other.textBlocks, block)
		}
	}
	other.wholePage = other.wholePage || stored.wholePage
	return other
}

// decodeGraphMutationWords reads a change's words; none without them.
func decodeGraphMutationWords(raw string) ([]byte, error) {
	if raw == "" {
		return nil, nil
	}
	if base64.StdEncoding.DecodedLen(len(raw)) > graphMutationWordsMaxBytes {
		return nil, graphInputError(errorCodeGraphChangeTooLarge, "The text written with the change is too large for one change.")
	}
	words, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(words) == 0 {
		return nil, graphRefused(errorCodeGraphInvalid, "Check the content change details.", "words is the editor's text update in base64; leave it out")
	}
	return words, nil
}

// storeGraphMutationWords stores the words a change carries in its
// transaction, as the page's socket would have, for someone whose socket
// takes their writes: a page they may change, outside the trash, not locked
// to them.
func storeGraphMutationWords(
	ctx context.Context, app core.App, principalID string, authorization pageAuthorization, words []byte,
) (ygoTextStored, error) {
	if len(words) == 0 {
		return ygoTextStored{}, nil
	}
	pageID := authorization.Page.Id
	if err := authorizeGraphMutationWords(app, principalID, authorization); err != nil {
		return ygoTextStored{}, err
	}
	refused, err := graphMutationWordsRefused(app, authorization.Workspace.Organization.OrganizationID, pageID, words)
	if err != nil {
		return ygoTextStored{}, err
	}
	if refused {
		return ygoTextStored{}, errDLPPolicyBlocked
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return storeAttributedYgoUpdatesIn(ctx, app, pageID, []attributedYgoUpdate{{
		update: words, actorID: principalID, at: time.Now(),
	}}, true)
}

// authorizeGraphMutationWords lets through the words of someone the page's
// socket takes writes from (evaluateRoomAccessAs): a row's body they may
// write, or a page they may change that isn't locked to them.
func authorizeGraphMutationWords(app core.App, principalID string, authorization pageAuthorization) error {
	pageID := authorization.Page.Id
	viewOnly := &graphRequestError{status: http.StatusForbidden, code: errorCodePermissionDenied, message: "You can only view this page."}
	if rowPage, err := isDatabaseRowPageRoom(app, pageID); err != nil {
		return err
	} else if rowPage {
		_, readOnly, err := authorizeDatabaseRowBodyRoom(app, principalID, pageID)
		if isPermissionDenied(err) || err == nil && readOnly {
			return viewOnly
		}
		return err
	}
	if _, err := authorizePage(app, principalID, pageID, permissionWorkspaceWrite); isPermissionDenied(err) {
		return viewOnly
	} else if err != nil {
		return err
	}
	locked, err := pageLockedFor(app, authorization.Page, principalID)
	if err != nil {
		return err
	}
	if locked {
		return pageLockedError()
	}
	return nil
}

// graphMutationWordsRefused tells whether the organization's DLP policy
// refuses the words, as it refuses a socket's write that adds a sensitive
// term to a block's text (ygo_dlp_write_check.go). Checked on a copy of the
// page's text, its open room's or else its stored one: the room takes the
// words only once the change is committed.
func graphMutationWordsRefused(app core.App, organizationID, pageID string, words []byte) (bool, error) {
	scanner, err := newOrganizationDLPContentScanner(app, organizationID)
	if err != nil || !scanner.blocks() {
		return false, err
	}
	document := openPageTextRoom(app, pageID)
	if document == nil {
		stored, _, err := loadYgoDoc(app, pageID)
		if err != nil {
			return false, err
		}
		document = stored
	}
	if document == nil {
		return false, nil
	}
	return (&ygoDLPShadow{}).refuses(scanner, document, words), nil
}
