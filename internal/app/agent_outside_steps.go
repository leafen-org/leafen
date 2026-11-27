package app

import "strings"

// How an agent takes the person through what they do outside Leafen — on
// another service's site (Notion's connection, Google's app password or
// client, Tailscale's sign-in, a service's sign-in) or a secret Leafen needs
// — the one rule every guide and tool answer that hands the person such steps
// carries (2026-11-16, the user: the agent only named where to go in Notion;
// it should open the browser with them, ask them to sign in, say which page
// and which button; the same day's move report: Leafen's import screen asked
// the agent's browser to sign in — a browser not signed in is signed in by
// the agent's approval, leafen_sign_in_requests; 2026-11-19, the s251 round:
// the Mac app's person got Leafen's screen in a browser not signed in, while
// the app's window was — person_window.go and person_links.go decide where;
// 2026-11-19 22:1x, the user: "에이전트에서 최대한 안빠져나오고 다 해결할수있도록"
// — an agent with a browser does the steps itself in a tab of its own, the
// person's hand only signing in and one "yes" before something is made on
// their account or a site's terms are agreed to; 2026-11-20 04:5x–05:1x, the
// user, after the Claude app's agent, whose only browser was the one inside
// the app, took that for no browser and handed every Notion step to the
// person, opened Notion without a word of why and worked there in silence:
// the agent's own browser counts too — the person signs in there once —, it
// says what it opens and why before opening, a line again as it opens, and
// keeps telling them how far it is while it works there; and a secret comes
// through the conversation to the tool that takes it, Leafen's window only
// for a person who'd rather not paste it here). Only an agent without a
// browser, or a step it can't do, goes through the steps with the person one
// at a time — 2026-11-22, the user: "mcp로 모든게 다 되야되", an agent never
// stops at "I can't, please do it": it does everything up to the step only
// the person can do and shows just that step, marked on a picture where it
// can; and after asking once it signs in a Leafen screen signed out in a
// browser it works in, the only code it takes on its own being the one on a
// Leafen screen it opened. A guide puts agentOutsideStepsMark where the rule
// goes (withAgentOutsideSteps); a tool's answer that hands the agent a page says
// agentInTheirBrowser; the steps themselves stay the guide's or its tool's,
// in the person's words. Since 2026-11-23 (the user, on the main loop's
// question) the rule gives what the person hears as facts and points — the
// agent's own sentences in the person's language carry them — never English
// words to relay, and quotes only another service's buttons, menus and
// fields as its screen shows them; what is asked and shown stays as it was.

const agentOutsideStepsMark = "{{doing-outside-steps-with-the-person}}"

const agentOutsideStepsRule = `## Doing outside steps with the person

What holds when something is done outside Leafen — on another service's
site, or a secret Leafen needs. The person stays in this conversation, and
the agent does as much of it as it can; a list handed to them, or "I can't,
please do it", is no step. What the person hears is in their language, in
the agent's own sentences; below are the points those carry. Only another
service's buttons, menus and fields are quoted exactly as its screen shows
them.

- **Before anything opens.** Before a site or any other screen opens in a
  browser, a line to the person carries what opens, why, and what is done
  there; it opens once they've understood, and when a yes is needed below,
  that line is in the same message as the question. As it opens, a line
  says so (which site, for what). While the work goes on there, a line
  every minute or so carries what is being done and how far it is —
  waiting for their login, what is read or made, done; no stretch of it
  goes without a word.
- **With a browser, the agent does the steps.** Any browser it can work in
  counts: theirs (such as Claude in Chrome) or its own (such as the browser
  inside the Claude app). The page opens in a tab of its own and the steps
  are done there by the agent. If the site asks them to log in, they log
  in once in that tab — its own browser asks the first time. Before
  something is made on their account or a site's terms are agreed to for
  them, one question asks for a yes, all at once, carrying what is made,
  where, and with which settings. Where the steps given recommend a choice,
  the agent makes it without asking. Their hand is only what only they can
  do: logging in, a password or a code the site asks for, and that yes.
  Where something stops the agent — a screen it can't get past, a window
  its tool doesn't reach — it goes on with them from that step, as below.
- **Without one, the page opens for them.** Run on their computer with
  commands, the agent opens it in their browser itself:
  ` + "`open \"<url>\"`" + ` on macOS, ` + "`xdg-open \"<url>\"`" + ` on Linux,
  ` + "`start \"\" \"<url>\"`" + ` on Windows. A chat app that can't gives them
  the exact link to open. A line to them then carries which page opened
  and, if the site asks, that logging in there is theirs; the next waits
  until they have.
- **Then one step at a time.** Each line carries the next step alone, its
  buttons and fields quoted as that screen names them in its language (the
  steps given quote them) and, where one can be shown, a picture of that
  screen with the place marked; the next waits for their word that it's
  done. If they see something else, what the screen shows decides, and the
  question is what it shows.
- **A secret comes through this conversation, never into the agent's
  answers.** Where the guide or tool names a tool that takes it, a secret
  the agent made in its tab goes there at once, and one they made they
  paste here and the agent passes it on at once, unrepeated. Leafen's own
  window takes it only when they'd rather not paste it into the
  conversation, or where no tool takes it: the agent types it there where
  its tab has Leafen open (logging it in as below if it asks), else they
  do. Where Leafen is the app on their computer, that is the app's own
  window, not a browser, which isn't logged in to Leafen there: the tool or
  guide that gives the steps says so, and brings that window to the front
  itself or gives a leafen:// link that opens it there (opened as any
  link). Elsewhere it is Leafen in their browser, at the link given with
  the steps, which the agent opens for them. Before the wait, a line
  carries which field and which button; the tool that waits for it notices
  when it arrives, so the agent goes on without being told, and a line now
  and then while it waits says it still is.
- **A browser not logged in to Leafen.** If Leafen opens on its login
  screen in a browser the agent works in, pasting is no way round it: one
  question asks whether the agent may log it in as them; on a yes, the
  agent presses “Log in with AI”, right under “Log in with a code”,
  there itself (as that screen names it in its language) and approves the
  code it shows with ` + "`leafen_sign_in_requests`" + `, and the
  window opens logged in. The agent takes only the code on a Leafen screen
  it opened with their yes; any other code only from the person: in a
  browser it doesn't work in, they press it and send the sentence it shows.`

// agentOpensInTheirBrowser is the rule's "without one" part for a tool's
// answer: the agent opens the page in the person's browser itself.
const agentOpensInTheirBrowser = "in their browser yourself (open, xdg-open or start on their computer; a chat app gives them the link)"

// agentInTheirBrowser is the rule's first parts for a tool's answer that
// hands the agent a page for the person (a sign-in, a token's screen): said
// first what it opens and why, then in a tab of the agent's own in a browser
// it works in — theirs or its own —, its steps done by the agent up to what
// only the person does; else opened for them.
const agentInTheirBrowser = "in a tab of your own in a browser you work in (theirs, or your own such as the Claude app's, where they log in once if it asks), " +
	"after a line to them, in their language, carrying what opens and why, its steps done by you up to what only they can do (logging in, a code, " +
	"one yes before you make something on their account), with lines on how far it is; without a browser, " + agentOpensInTheirBrowser

// withAgentOutsideSteps puts the rule where a guide marks it.
func withAgentOutsideSteps(guide string) string {
	return strings.Replace(guide, agentOutsideStepsMark, agentOutsideStepsRule, 1)
}
