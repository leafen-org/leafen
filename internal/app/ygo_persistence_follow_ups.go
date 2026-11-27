package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// What an operation leaves to write once it is committed — the block a page
// that left its database takes in the body it went under, a wiki page's
// link, a template's look, the pages anchored on the lines a change wrote,
// the blocks of blocks made pages — is written as an operation's own write
// is (runOperationTransaction): counted among those under way, which a
// closing server lets end until half its time (beginShutdown), and stopped
// with them after that, rolled back whole. Written through the server's own
// app with no stop, one held the write connection through the server's
// stop, the parts' last writes found the database busy, and it ended with
// the process (2026-11-18). One begun once the server is closing runs until
// the closing writes' stop (beginShutdown), as an operation begun then does.
//
// One the stop cut is done again as the server starts again, when nothing
// else brings it back (runKeptFollowUp): it is left by its kind and what it
// was given with the work requests leave (product_afterwork_kept.go). Its
// operation is made, so its client sending it again finds nothing to do —
// a database page moved out has left its database, and a move sent again
// moves no database page — and a page that left its database under the
// page holding it never had its block in that page's body (2026-11-18).
// So the operation answers as made, as an agent's call, the app's and the
// Notion API's alike: answered failed, "try once more", over a follow-up
// the next start does, its agent was sent to a move that was done
// (2026-11-18).
// What a job runs (people mapping, a long block move) goes on with its job,
// and a template's look is written again when its client sends the change
// again, the values found already written.

// runFollowUp runs one write an operation left to do.
func (p *pocketBaseYgoPersistence) runFollowUp(write func(core.App) error) error {
	ctx, cancel, startedBeforeShutdown, err := p.operationContext(context.Background(), "")
	if err != nil {
		return err
	}
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	return p.runOperationTransaction(p.operationStop(startedBeforeShutdown), write)
}

// runKeptFollowUp runs a follow-up, left to do again as kept when the
// server's stop cuts it: then nil, its operation's answer its own.
func (p *pocketBaseYgoPersistence) runKeptFollowUp(kept keptAfterwork, write func(core.App) error) error {
	end := productAfterworkOf(p.app).begin(kept)
	err := p.runFollowUp(write)
	if end(followUpCut(err)) {
		return nil
	}
	return err
}

// followUpCut is whether the server's stop cut a follow-up, or refused it.
func followUpCut(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// followUpApp is the app for what an operation leaves to write, for the
// helpers that take an app: each of its transactions is a follow-up
// (runFollowUp), kept as kept when it is not nil (runKeptFollowUp); its
// reads and the rest are the server's.
func (p *pocketBaseYgoPersistence) followUpApp(kept *keptAfterwork) core.App {
	return ygoFollowUpApp{App: p.app, persistence: p, kept: kept}
}

type ygoFollowUpApp struct {
	core.App
	persistence *pocketBaseYgoPersistence
	kept        *keptAfterwork
}

func (app ygoFollowUpApp) RunInTransaction(fn func(txApp core.App) error) error {
	if app.kept != nil {
		return app.persistence.runKeptFollowUp(*app.kept, fn)
	}
	return app.persistence.runFollowUp(fn)
}

// The follow-ups done again as the server starts, by kind.
const (
	followUpDatabasePageLeft = "leafen database page left its database's body"
	followUpWikiPageLinkKept = "leafen wiki page link kept"
	followUpMentionAnchors   = "leafen pages anchored on mentions"
)

// resumeFollowUp does again a follow-up the last stop cut, as work a
// request leaves is done: one the next stop cuts is kept again.
func resumeFollowUp(app core.App, kept keptAfterwork, write func(core.App) error) {
	productAfterworkOf(app).run(kept.Kind, kept.Input, func(context.Context) error {
		return app.RunInTransaction(write)
	})
}

// databasePageLeft is what a database page that left its database's body —
// moved out as a page of its own, or a wiki's page sent to the trash —
// leaves to write: its marked links out of the bodies they are in, and, for
// one moved under the page holding its database (holderID), its block at
// that page's body's end, once the row has left that body, where the record
// had the same ID.
type databasePageLeft struct{ ownerID, workspaceID, actorID, pageID, holderID string }

func (left databasePageLeft) write(txApp core.App) error {
	if err := dropWikiPageLinks(txApp, left.ownerID, left.workspaceID, left.actorID, left.pageID); err != nil || left.holderID == "" {
		return err
	}
	return placeChildPageBlock(
		txApp, left.ownerID, left.workspaceID, left.holderID, left.pageID, left.actorID, pageBodyPlacement{Type: "end"},
	)
}

func (left databasePageLeft) kept() keptAfterwork {
	return keptAfterwork{Kind: followUpDatabasePageLeft, Input: afterworkInput(
		left.ownerID, left.workspaceID, left.actorID, left.pageID, left.holderID,
	)}
}

func resumeDatabasePageLeft(app core.App, input []string) {
	left := databasePageLeft{ownerID: input[0], workspaceID: input[1], actorID: input[2], pageID: input[3], holderID: input[4]}
	resumeFollowUp(app, left.kept(), left.write)
}

// wikiPageLinkKept is keeping a wiki page's link in the body above it
// (keepWikiPageLink), as kept.
func wikiPageLinkKept(actorID, pageID string) keptAfterwork {
	return keptAfterwork{Kind: followUpWikiPageLinkKept, Input: afterworkInput(actorID, pageID)}
}

func resumeWikiPageLinkKept(app core.App, input []string) {
	resumeFollowUp(app, wikiPageLinkKept(input[0], input[1]), func(txApp core.App) error {
		return keepWikiPageLink(txApp, input[0], input[1])
	})
}

// mentionAnchorsKept is settling the pages anchored on the lines stored
// text wrote (settleMentionAnchoredPagesAfterText), as kept.
func mentionAnchorsKept(actorID, pageID string, blocks []string, whole bool) keptAfterwork {
	encoded, _ := json.Marshal(blocks)
	wholePage := ""
	if whole {
		wholePage = "whole"
	}
	return keptAfterwork{Kind: followUpMentionAnchors, Input: afterworkInput(actorID, pageID, string(encoded), wholePage)}
}

func resumeMentionAnchors(app core.App, input []string) {
	blocks := []string{}
	if json.Unmarshal([]byte(input[2]), &blocks) != nil {
		return
	}
	kept := mentionAnchorsKept(input[0], input[1], blocks, input[3] != "")
	productAfterworkOf(app).run(kept.Kind, kept.Input, func(context.Context) error {
		settleMentionAnchoredPagesAfterText(app, input[0], input[1], blocks, input[3] != "")
		return nil
	})
}
