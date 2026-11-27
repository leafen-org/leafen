package app

import (
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The work requests leave to do (product_afterwork.go) that the server's
// stop cancelled or refused is written as the stop ends, each as its kind
// and what it was given, and done again when the server starts: read, taken
// out, and started as a request would start it. Work done twice changes
// nothing a second time — it reads again what it brings up to date — but
// a security notice, sent again only when its sending had not ended. A row lives only from a stop to the next start; what it holds — the
// ids work names, a renamed person's former name, a notice's addresses — is
// the server's own and goes in no export.

const productAfterworkLeftTable = "leafen_product_afterwork_left"

// keptAfterwork is a work by its kind and what it was given.
type keptAfterwork struct {
	Kind  string `db:"kind"`
	Input string `db:"input"`
}

// keptAfterworkKinds start each kind of work again from its input, as the
// request that left it did.
var keptAfterworkKinds = map[string]func(app core.App, input []string){
	afterworkPersonMentionNames: func(app core.App, input []string) {
		refreshPersonMentionsSoon(app, input[0], input[1])
	},
	afterworkBotMentionNames: func(app core.App, input []string) {
		refreshBotMentionsSoon(app, input[0], input[1], input[2])
	},
	afterworkPeopleRenamed:  func(app core.App, input []string) { admitPeopleRenamedSoon(app, input[0]) },
	afterworkPeopleDatabase: func(app core.App, input []string) { syncPeopleDatabaseSoon(app, input[0]) },
	afterworkSecurityNotice: func(app core.App, input []string) { resumeAccountSecurityNotice(app, input[0]) },
	// What an operation left to write that the stop cut (ygo_persistence_follow_ups.go).
	followUpDatabasePageLeft: resumeDatabasePageLeft,
	followUpWikiPageLinkKept: resumeWikiPageLinkKept,
	followUpMentionAnchors:   resumeMentionAnchors,
}

// keptAfterworkSizes are how many values each kind's input has.
var keptAfterworkSizes = map[string]int{
	afterworkPersonMentionNames: 2, afterworkBotMentionNames: 3,
	afterworkPeopleRenamed: 1, afterworkPeopleDatabase: 1, afterworkSecurityNotice: 1,
	followUpDatabasePageLeft: 5, followUpWikiPageLinkKept: 2, followUpMentionAnchors: 4,
}

const (
	afterworkPersonMentionNames = "leafen person mention names"
	afterworkBotMentionNames    = "leafen bot mention names"
	afterworkPeopleRenamed      = "leafen people automations rename"
	afterworkPeopleDatabase     = "leafen people database"
	afterworkSecurityNotice     = "leafen account security notice"
)

// afterworkInput is what a work is given, as its kind reads it again.
func afterworkInput(values ...string) string {
	encoded, _ := json.Marshal(values)
	return string(encoded)
}

// keepAfterworkLeft writes the work a stop left for the next start.
func keepAfterworkLeft(root core.App, left []keptAfterwork) error {
	if len(left) == 0 {
		return nil
	}
	if root == nil {
		return errAfterworkUnkept
	}
	now := time.Now().UTC().UnixMilli()
	return root.RunInTransaction(func(txApp core.App) error {
		for _, work := range left {
			if _, err := txApp.DB().NewQuery(`
				INSERT INTO {{leafen_product_afterwork_left}} ([[kind]], [[input]], [[noted_at]])
				VALUES ({:kind}, {:input}, {:now})
			`).Bind(dbx.Params{"kind": work.Kind, "input": work.Input, "now": now}).Execute(); err != nil {
				return err
			}
		}
		return nil
	})
}

// resumeProductAfterwork has the server's own app keep what the server's
// stop leaves, and starts again what the last stop left.
func resumeProductAfterwork(app core.App) {
	owner := productAfterworkOf(app)
	owner.mu.Lock()
	owner.root = app
	owner.mu.Unlock()
	left := []keptAfterwork{}
	err := app.RunInTransaction(func(txApp core.App) error {
		if err := txApp.DB().NewQuery(`
			SELECT [[kind]], [[input]] FROM {{leafen_product_afterwork_left}} ORDER BY [[id]]
		`).All(&left); err != nil {
			return err
		}
		_, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_product_afterwork_left}}`).Execute()
		return err
	})
	if err != nil {
		app.Logger().Warn("leafen request afterwork left by the last stop waits", "error", err)
		return
	}
	seen := map[keptAfterwork]bool{}
	for _, work := range left {
		if seen[work] {
			continue
		}
		seen[work] = true
		start, known := keptAfterworkKinds[work.Kind]
		input := []string{}
		if !known || json.Unmarshal([]byte(work.Input), &input) != nil || len(input) != keptAfterworkSizes[work.Kind] {
			app.Logger().Warn("leafen request afterwork left by the last stop is unknown", "kind", work.Kind)
			continue
		}
		start(app, input)
	}
}
