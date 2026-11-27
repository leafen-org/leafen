package app

import (
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// The guide's first words — the road, why this road, nothing moves before
// they approve, how long, when the agent needs the person — lead what the
// agent says about a move, ahead of whatever the sources answer has it do
// with the person (2026-11-16: two of three logged-in agents read the guide,
// then went straight to part 1's first step, which the answer with no
// connection handed them). Leafen words them (the import screen's catalog,
// importDialog handoff.firstWords) and they come where the agent is talking
// to the person anyway (2026-11-21, the real Claude Code 2.1.330 on the
// Windows test machine, five conversations begun by connecting it from the
// import screen's text: after the person's /reload-plugins and ‘continue’ it
// read the guide and sources and went straight to Notion's first step,
// whether the next asked for the points "if you haven't yet told the
// person", spelled them out unconditionally, or gave them as words to say —
// while a conversation the person began with the move had all five, and the
// connection report said every line the text asked for). So:
//   - the import screen's move text, at the end of the connection text, has
//     the agent say them in the answer that reports its connection, before
//     it asks for /reload-plugins (and in its first answer when it is
//     connected already);
//   - the start offered once connected (agentStartAsk, setup status's start,
//     the web's agentPrompt startInChat) tells them in its question, so the
//     person knows how a move goes before choosing it (the same day, a move
//     the person chose after connecting had them once in two walks with
//     sources alone, and part of them when they were for the answer after
//     the choice);
//   - a move's first sources answer for a connection in a workspace, once in
//     notionMigrationFirstWordsAgain, leads its next with them, to say unless
//     said already in the conversation.
// The guide's "Your first words" says the same order.

// notionMigrationFirstWordsNext leads a sources answer's next with the first
// words, in the language the person's Leafen shows, as the import screen's
// catalog words them (importDialog handoff.firstWords, generated into
// notion_import_steps.generated.go): Leafen words them, as it words what to
// tell after connecting (claudeCodeReloadTell), since the agent that skipped
// the points it was to compose said those words as they were. The look
// Notion's API doesn't give is matched only by an agent that can see Notion
// in a browser, so the words say both ways.
// They come as the answer's first_words, beside next, which names them:
// inside next, five agents told what was left to do went on to the steps
// below without them (2026-11-23).
func notionMigrationFirstWords(app core.App, ownerID string) string {
	return notionImportWords(personLanguageCode(app, ownerID, nil)).FirstWords
}

// notionMigrationFirstWordsNext is what a sources answer's next says of its
// first_words, ahead of its own next.
const notionMigrationFirstWordsNext = "first_words is how a move goes, as Leafen's import screen words it in the person's language. " +
	"Left to do first, before the steps below, unless this conversation told the person already (what was said while connecting to Leafen isn't it): " +
	"a message to them with first_words, the road their move takes. Then: "

// How long a connection's first words in a workspace count as said: a move
// is read, planned and run within it, and one begun later hears them again.
const notionMigrationFirstWordsAgain = 12 * time.Hour

// notionMigrationFirstWordsSaid holds when each connection was last asked
// for them in each workspace.
var notionMigrationFirstWordsSaid sync.Map

// notionMigrationFirstWordsDue reports whether the sources answer for this
// connection in this workspace asks for the first words, and counts them
// asked when it does.
func notionMigrationFirstWordsDue(connectionID, workspaceID string) bool {
	key := connectionID + "\x00" + workspaceID
	now := time.Now()
	if said, found := notionMigrationFirstWordsSaid.Load(key); found && now.Sub(said.(time.Time)) < notionMigrationFirstWordsAgain {
		return false
	}
	notionMigrationFirstWordsSaid.Store(key, now)
	return true
}
