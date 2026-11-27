package app

import "errors"

// A page an operation holds for a while (page_busy.go) — its blocks being
// moved, a wiki's change, its copy — refuses everyone while it does, and a
// person's decision on it (decidePageAccessRole) says so where the person
// has the role asked apart from the hold: a refusal that is a permission
// denial to everything that reads one, so the page's own routes keep their
// answers (page_held_refusal.go tells its members it busy), but that says
// which operation holds the page (pageAccessHeld) and carries the decision
// the page gives once it is let go. What asks who has a page, rather than
// what a person may do on it this moment — who may approve a request for it,
// who hears its reminders and notices, which pages a guest invite link's
// maker shares — counts such a person as having it (pageAccessDespiteHold),
// not as someone without access (2026-11-23: a request for a page whose
// blocks were being moved notified no one; a one-person guest invite link
// taken meanwhile left that page out and was spent). A page being deleted
// for good, its workspace moving away, or one not yet shown is no hold: it
// stays a refusal like any other.

// pageHold is a refusal that says which operation holds its page.
type pageHold interface {
	pageHeldBy() string
}

// pageHoldDenial is a person's decision refused only because an operation
// holds the page: the decision it would give otherwise rides with it.
type pageHoldDenial struct {
	*permissionError
	busy          string
	authorization pageAuthorization
}

func (denial *pageHoldDenial) Unwrap() error      { return denial.permissionError }
func (denial *pageHoldDenial) pageHeldBy() string { return denial.busy }

// heldDecision is a decision made on a page the operation busy holds ("" for
// none): one that gave the person the role is refused as held; one that
// didn't stays as it was refused, and as before for anyone without it.
func heldDecision(busy string, authorization pageAuthorization, err error) (pageAuthorization, error) {
	if busy == "" {
		return authorization, err
	}
	if err == nil {
		return pageAuthorization{}, &pageHoldDenial{
			permissionError: &permissionError{code: errorCodePermissionDenied}, busy: busy, authorization: authorization,
		}
	}
	if pageAccessHeld(err) == "" && isPermissionDenied(err) {
		return pageAuthorization{}, permissionDenied()
	}
	return pageAuthorization{}, err
}

// pageAccessHeld is the operation err says holds its page from a person who
// otherwise has the role asked; "" for any other refusal.
func pageAccessHeld(err error) string {
	var held pageHold
	if errors.As(err, &held) {
		return held.pageHeldBy()
	}
	return ""
}

// pageAccessHeldOn is the page and the operation a person's decision refused
// only as held names; "" for any other refusal.
func pageAccessHeldOn(err error) (pageID, busy string) {
	var held *pageHoldDenial
	if errors.As(err, &held) && held.authorization.Page != nil {
		return held.authorization.Page.Id, held.busy
	}
	return "", ""
}

// pageAccessDespiteHold is a decision that counts a page an operation holds
// for a while as the person's where only the hold refused it.
func pageAccessDespiteHold(authorization pageAuthorization, err error) (pageAuthorization, error) {
	var held *pageHoldDenial
	if errors.As(err, &held) {
		return held.authorization, nil
	}
	return authorization, err
}

// heldRefusal is a road's own refusal of a page an operation holds, made
// from a decision refused as held: it keeps saying which operation does.
type heldRefusal struct {
	busy  string
	cause error
}

func (refusal heldRefusal) Error() string      { return refusal.cause.Error() }
func (refusal heldRefusal) Unwrap() error      { return refusal.cause }
func (refusal heldRefusal) pageHeldBy() string { return refusal.busy }

// refusalKeepingHold is the refusal a road makes of a decision err, which
// keeps the hold err says, if any.
func refusalKeepingHold(err, refusal error) error {
	if busy := pageAccessHeld(err); busy != "" {
		return heldRefusal{busy: busy, cause: refusal}
	}
	return refusal
}
