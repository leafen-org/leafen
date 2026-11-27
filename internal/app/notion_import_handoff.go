package app

import "github.com/pocketbase/pocketbase/core"

// What an agent does with the person while the workspace has no Notion
// connection (2026-11-12, the user's own run on the NAS: the agent knew no
// link to the token's screen; 2026-11-16, the user: it only named where to go
// in Notion, where it should open the browser and go step by step with them;
// 2026-11-19 22:1x, the user: it should get the token itself and keep them
// in the conversation; 2026-11-20 04:5x, the user's NAS run: the Claude
// app's agent, whose only browser was its own, handed every step to the
// person, always made a new connection, and its connection text sent the
// person to the import screen for the key — "채팅내에서 엔드투엔드"): Notion's
// list of the person's connections, where a connection and its token are
// made. An agent with a browser — theirs, or its own, where they sign in to
// Notion once — says what it opens and why in one question that asks the
// one "yes" (which also agrees to Notion's developer terms when it makes
// one), looks at the list first, takes the token of the connection made for
// Leafen before — fixing only what it lacks — or makes one reading only,
// with every top page, asks once to delete other Leafen connections, and
// hands the token to leafen_notion_migration_connect (agent_outside_steps.go).
// One without, or one stopped on the way, goes through Notion's steps with
// the person one at a time as the import screen words them in the person's
// language (notion_import_steps.generated.go, from the web's catalog), the
// last pasting the token into the conversation. Only a person who'd rather
// not paste it there puts it in this server's import screen
// (screen_instead), where they see Leafen (person_window.go): in the desktop
// app's window, which the agent's wait for the token brings to the front on
// it, with no address (2026-11-19, the s251 round: the address opened in a
// browser not signed in, while the app's window was); else at the address
// the agent reached Leafen at, which the agent opens for them. The agent says
// that step — its field and button — before it waits (the same round: it
// waited two minutes in silence after "where do I put it?").

// notionImportScreenPath is the import screen with Notion's window open
// (settingsWindowModel's legacySettingsAddress), where the token goes; come
// by it from an agent (`via=agent`), the window opens at the token's step,
// saying the token stays in Leafen, and once it is saved, that the agent
// goes on.
const notionImportScreenPath = "/settings?section=import&source=notion&via=agent"

// notionConnectionsPage is Notion's page for making a connection and its
// token, its developer tools' "Developer connections" (notion.so/profile/
// integrations goes there, seen 2026-11-16).
const notionConnectionsPage = "https://app.notion.com/developers/connections"

type notionImportPersonText struct {
	Steps        []string
	Token        string
	PasteInstead string
	// The move's first words, as the agent says them
	// (notion_migration_first_words.go).
	FirstWords string
}

// notionImportHandoffFields are what leafen_notion_migration_sources gives
// an agent to make the connection with the person (notion_migration_guide.md
// section 1).
type notionImportHandoffFields struct {
	NotionPage   string   `json:"notion_page,omitempty"`
	ForThePerson []string `json:"for_the_person,omitempty"`
	// Leafen's own screen for the token, only for a person who'd rather not
	// paste it into the conversation.
	ScreenInstead string `json:"screen_instead,omitempty"`
	ImportScreen  string `json:"import_screen,omitempty"`
	// The import screen is in the Leafen app's window on their computer: no
	// address; the call that waits for the token brings it to the front.
	ImportInApp bool `json:"import_in_app,omitzero"`
}

// The next of the connections' first answer, when there are some.
const notionConnectionsListedNext = "Call again with connection_id to list what it reaches."

// The import screen in a browser not signed in to Leafen shows Leafen's
// sign-in first (2026-11-16, the user's move: the agent's own browser was
// stopped there and the token went through the conversation): the agent signs
// it in by approving its code — in a browser it works in, after asking once,
// pressing "Log in with AI" there itself (2026-11-22, the user); in
// another, with the sentence the person sends it (agentOutsideStepsRule).
const notionImportScreenSignIn = "(if it shows Leafen's login: in a browser you work in, one question to them first, then \"Log in with AI\" " +
	"pressed there by you and its code approved with leafen_sign_in_requests — only the code on a Leafen screen you opened with their yes; " +
	"elsewhere they press it and send you the sentence it shows) "

// notionImportScreenInstead is the way of a person who'd rather not paste
// the token into the conversation: screen_instead, said to them first, then
// waited on, by where the import screen is.
func notionImportScreenInstead(kind personWindowKind) string {
	way := "Only if they'd rather not paste it into this conversation, a line to them carries screen_instead, which names the field and the button. "
	if kind == personWindowApp {
		return way + "It is in the Leafen app's own window on their computer (import_in_app: no address — a browser isn't logged in to Leafen there); " +
			"this tool with wait_seconds 45 brings that window to the front on its import screen and answers as soon as the connection is there; " + notionImportWaitAgain
	}
	return way + "Open import_screen for them " + notionImportScreenSignIn + "— this tool with wait_seconds 45 answers as soon as the connection is there; " + notionImportWaitAgain
}

const notionImportWaitAgain = "it lists none until the token is in. "

// notionConnectionsPageWorkspace is the check before the connections page
// is used: Notion opens it in the workspace used last, with no workspace in
// its address (2026-11-21, w2130's walk: a fresh tab opened it in another
// workspace than the one to move), so the agent looks which one it shows
// and switches first, and never uses or makes a connection in another.
const notionConnectionsPageWorkspace = "Notion opens that page in the workspace used last, so before anything else check which one it shows — its name at the foot " +
	"of the sidebar and in the list's installable-workspace column; if it isn't the workspace to move, switch to it first (the workspace switcher " +
	"at the foot of the sidebar, or a page of that workspace) and open notion_page again. Never look at, use or make a connection in another workspace. "

// notionImportMakeConnection is how the agent gets the token: itself, with a
// browser, else with them one step at a time up to pasting it here.
func notionImportMakeConnection(kind personWindowKind) string {
	return "With a browser — theirs (such as Claude in Chrome) or your own (such as the Claude app's), where they log in to Notion once — you get it. " +
		"Left to do first: one question to them, in their language, carrying all of these points: notion_page, Notion's list of their connections, " +
		"opens in your browser to give Leafen a key that only reads; it comes from the connection they made for Leafen before, if there is one, else " +
		"from a new one named Leafen in the Notion workspace to move (which one, if their Notion has more than one); that agrees to Notion's developer " +
		"terms; it gets all the workspace's top pages, and the one question later names what moves. On their yes, a line says it is opening, and " +
		"notion_page opens in a tab of your own; if Notion asks them to sign in, that is theirs: a line to them, then a wait. " + notionConnectionsPageWorkspace +
		"Then look at the list: " +
		"a connection made for Leafen before is opened, not made again — set only what it lacks of for_the_person's read capabilities and top pages; " +
		"with none, make one as for_the_person says, with every top page and only the read capabilities it names; with more than one made for Leafen, " +
		"use the one whose content access has the most pages, else the newest; whether to delete the others (the part of their page Notion names “Danger zone”, in its language) is one question " +
		"to them, and they go only on a yes. Read its token (ntn_…), showing it first if it's hidden, with a line to them as each part is done; hand it to " +
		"leafen_notion_migration_connect at once, without repeating it, and go on. " +
		"Where your browser can't go on, it goes on with them from that step as follows. Without a browser, open notion_page " + agentOpensInTheirBrowser +
		"; a line to them then carries that logging in to Notion is theirs if it asks, and that it must show the workspace to move, switched to there " +
		"first if not; then for_the_person goes to them one step at a time, each waiting for their word it's done — a connection they " +
		"made for Leafen before is opened, not made again, and at its content access they select all the top pages; its last has them paste the token " +
		"here: pass it on to leafen_notion_migration_connect at once, without repeating it. " + notionImportScreenInstead(kind)
}

// notionImportHandoffNext is what the agent does with them: with no
// connection, and with only connections whose token Notion refuses.
func notionImportHandoffNext(kind personWindowKind, reconnect bool) string {
	if reconnect {
		return "Notion no longer accepts the token of the person's Notion connection (needs_reconnect: revoked or replaced in Notion). " +
			"Get its token again the same way, as leafen_notion_migration_guide section 1 says. " + notionImportMakeConnection(kind) +
			"Once there is a connection without needs_reconnect, go on yourself with it: choose the roots with them (call with connection_id), start and scan."
	}
	return "There is no Notion connection yet. Get one as leafen_notion_migration_guide section 1 says. " +
		notionImportMakeConnection(kind) +
		"Once there is a connection, go on yourself: choose the roots with them (call with connection_id), start and scan."
}

// notionImportHandoff is what the agent needs to make the connection with
// the person in a workspace, in the language the person's Leafen shows, and
// what it does next.
func notionImportHandoff(app core.App, ownerID, resource, workspaceID string, reconnect bool) (notionImportHandoffFields, string) {
	kind := personWindowOf(app, ownerID)
	screen := ""
	if kind != personWindowApp {
		// The workspace's import screen, which the address names (person_links.go).
		path := workspacePath(app, workspaceID, notionImportScreenPath)
		var err error
		if screen, err = mcpServerURL(resource, path); err != nil {
			screen = path
		}
	}
	words := notionImportWords(personLanguageCode(app, ownerID, nil))
	// The token comes through the conversation: its pasting is the last step,
	// the import screen only the way of one who'd rather not.
	steps := append(append([]string{}, words.Steps...), words.PasteInstead)
	return notionImportHandoffFields{NotionPage: notionConnectionsPage, ForThePerson: steps, ScreenInstead: withScreenAddress(words.Token, screen),
		ImportScreen: screen, ImportInApp: kind == personWindowApp}, notionImportHandoffNext(kind, reconnect)
}

// notionImportWords are the person's steps in language, in English where the
// catalog doesn't have them all.
func notionImportWords(language string) notionImportPersonText {
	if words, found := notionImportPersonTexts[language]; found {
		return words
	}
	return notionImportPersonTexts[accountLanguageEn]
}

// notionImportWindowOpened is the agent's wait for the token asking the
// person's window to open the import screen (openForAgent): true once asked.
func notionImportWindowOpened(app core.App, ownerID string) bool {
	openForAgent(app, ownerID, "notion")
	return true
}
