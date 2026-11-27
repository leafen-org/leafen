package app

import "github.com/pocketbase/pocketbase/core"

// Leafen's own upkeep of the pages it keeps under their owner's authority
// (contentGraphMutationHooks.LeafenOwn) is no one's act from its start, not
// from its last step: what the change records on its way — its rows' pages'
// versions, which the database's projection records as it writes each row,
// and the events those versions tell — is Leafen's and reaches no webhook or
// app, as its page's version and its page interaction are (2026-11-17: a
// member's new name written into their People row, and an invitee turned
// into their account, were kept as row-page versions by the workspace's
// owner and told to the workspace's webhook as properties the owner
// updated, the transaction being kept from apps only once the change was
// written).

// contentGraphMutationTeller is whom a change tells of itself as, in its
// notices and the page's history: Leafen for its own upkeep; a form's answer
// written with another's access, its respondent, or "익명" with none; else
// whoever writes it.
func contentGraphMutationTeller(principalID string, hooks contentGraphMutationHooks) string {
	switch {
	case hooks.LeafenOwn:
		return leafenSystemAuthorID
	case principalID == "" && hooks.FormResponse && hooks.FormRespondent == "":
		return leafenAnonymousAuthorID
	case principalID == "" && hooks.FormResponse:
		return hooks.FormRespondent
	}
	return principalID
}

// beginLeafenOwnChange keeps Leafen's own change's transaction from its
// workspace's webhooks and apps from the change's first step on.
func beginLeafenOwnChange(txApp core.App, hooks contentGraphMutationHooks) {
	if hooks.LeafenOwn {
		keepTransactionFromApps(txApp)
	}
}

// leafenOwnRowVersionsBy is whom the rows' pages' versions a change writes
// are by when not its writer: Leafen for its own upkeep.
func leafenOwnRowVersionsBy(hooks contentGraphMutationHooks) string {
	if hooks.LeafenOwn {
		return leafenSystemAuthorID
	}
	return ""
}
