package app

// A browser asks to be signed in by the person's agent (agent_sign_in_requests.go):
// its request waits five minutes under a short code the screen shows, which
// the person gives an agent already connected; the browser holds a secret
// of its own to take the sign-in with, so only that browser signs in. The row
// keeps where it asked from, who approved or declined it through which
// connection, and the sign-in it made, as that sign-in's record. An account
// that turned agents' approvals off has a row in the second table.

const (
	agentSignInRequestsTable = "leafen_agent_sign_in_requests"
	agentSignInOffTable      = "leafen_agent_sign_in_off"
)
