package app

import (
	"fmt"
	"io"
	"strings"
)

// What `leafen setup` says of the Notion connection and of signing in
// elsewhere — the road the MCP tools give too (notion_import_handoff.go): an
// agent with a browser, theirs or its own, gets the token in Notion itself
// after one yes and hands it to leafen_notion_migration_connect; one without
// goes through Notion's steps with the person, who pastes the token into the
// conversation for it to hand over the same way. Only a person who'd rather
// not paste it there puts it in the import screen, by where the setup person
// sees Leafen (setupFacts.Window, person_window.go): in the desktop app, its
// window, which comes to the front on the import screen, and no browser,
// which isn't signed in to Leafen there; on a server installed on its own,
// the browser at its address.

// setupNotionNext is the Notion move's road, for setup status' Next.
func setupNotionNext(facts setupFacts) string {
	road := "connected to Leafen's MCP, read leafen_notion_migration_guide and get the Notion token as its section 1 says, all in the conversation: " +
		"with a browser, theirs or your own, you get it in Notion yourself after one yes and hand it to leafen_notion_migration_connect; " +
		"without one, you open Notion's page in their browser, go through its steps with them one at a time, and they paste the token into the " +
		"conversation for you to pass on the same way. Only if they'd rather not paste it there, "
	if facts.Window == personWindowApp {
		road += "it goes into Leafen's own field in the Leafen app's window on this computer (not a browser: one isn't logged in to Leafen here), " +
			"which leafen setup notion, or leafen_notion_migration_sources waiting for the token, brings to the front on it"
	} else {
		road += "it goes into Leafen's import screen, which you open for them in their browser at " + setupNotionScreen(facts) +
			" (leafen setup notion also opens it in a Leafen window of theirs open on this server)"
	}
	return road + ". Left to do before the wait: its field and its button, to them; the next step comes once it is in."
}

// setupNotionScreen is the import screen's address on this computer, or its
// path where the server named none.
func setupNotionScreen(facts setupFacts) string {
	return facts.Here + notionImportScreenPath
}

// setupSignInNext is how the person opens Leafen signed in elsewhere.
func setupSignInNext(facts setupFacts) string {
	if facts.Window == personWindowApp {
		return "To open this Leafen on another device (a phone, another computer): leafen account device-code --name \"<their name>\" " +
			"gives a one-time link and code (ten minutes) for the person; the Leafen app's window on this computer is logged in already."
	}
	return "To open Leafen logged in, on this computer or another device: leafen account device-code --name \"<their name>\" " +
		"gives a one-time link and code (ten minutes) for the person; a Leafen window already open on its setup logs itself in."
}

// runSetupNotion asks the setup person's window to open the Notion import,
// the way of a person who'd rather not paste the Notion token into the
// conversation: they put the token they make with the agent in Leafen's own hidden
// field — in the desktop app, its window comes to the front on it, past the
// first run's agents step an agent is connected for (askWindowOpen) — and it
// says the import screen's last step in the person's words, for the agent to
// say before it waits; the agent then moves the workspace with its MCP tools.
func runSetupNotion(out io.Writer, dir string) error {
	if appFirstScreen(out) {
		return nil
	}
	state, answered, err := askSetup(dir, setupRequest{Open: "notion"})
	if err != nil {
		return err
	}
	switch {
	case !answered:
		fmt.Fprintln(out, setupNoServer(dir))
		return nil
	case !state.Facts.Configured:
		fmt.Fprintln(out, "Leafen isn't set up yet: start it first (leafen setup start).")
		return nil
	}
	facts, screen := state.Facts, ""
	if facts.Window == personWindowApp {
		fmt.Fprintln(out, "The Leafen app's window on this computer now comes to the front on Settings › Import › Notion "+
			"(that window, not a browser: one isn't logged in to Leafen here).")
	} else {
		screen = setupNotionScreen(facts)
		fmt.Fprintln(out, "A Leafen window of theirs open on this server now opens Settings › Import › Notion.")
		fmt.Fprintf(out, "In a browser that isn't open on Leafen, open %s for them %s.\n", screen, strings.TrimSpace(notionImportScreenSignIn))
	}
	fmt.Fprintln(out, "That screen is only for a person who'd rather not paste the Notion token into your conversation: by default you get it "+
		"in Notion yourself with a browser (theirs or your own) after one yes, or they paste the one they made with you, and you hand it to "+
		"leafen_notion_migration_connect. Either way, connected to Leafen's MCP, read leafen_notion_migration_guide and follow its section 1; "+
		"leafen_notion_migration_sources gives the steps.")
	fmt.Fprintf(out, "Once they have the token, left to do first: a line to them, in their language, carrying all that this says, the field and the button as the screen names them: %s\n",
		withScreenAddress(notionImportWords(textLanguageOfCode(chosenLanguageOrEnglish(facts.Language))).Token, screen))
	fmt.Fprintln(out, "Only then call leafen_notion_migration_sources with wait_seconds 45: it answers once the connection is added; "+
		"call again while it lists none, with a line to them each time that you're still waiting. `leafen setup status` shows the migration's state.")
	return nil
}
