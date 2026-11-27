package app

import "github.com/pocketbase/pocketbase/core"

// A page's or database page's lock for me takes {unlocked} as its state for
// the person asking, or {locked}, as the lock itself names it (PUT
// /database-rows/{rowId}/lock), meaning the other way round: locked true
// locks it for them again (2026-11-16: an agent sent {locked: true} as the
// row's lock takes it and was refused). Both, at odds, are refused.
type pageLockForMeState struct {
	Unlocked *bool `json:"unlocked"`
	Locked   *bool `json:"locked"`
}

// pageLockForMeRefusal names the fields a lock for me takes.
const pageLockForMeRefusal = "Give unlocked: true to unlock the locked page for you, or false (or locked: true) to lock it for you again."

// bindPageLockForMe reads whether the request unlocks the page for its
// person; ok is false when the body says neither, or both at odds.
func bindPageLockForMe(event *core.RequestEvent) (unlocked bool, ok bool) {
	input := pageLockForMeState{}
	if event.BindBody(&input) != nil {
		return false, false
	}
	switch {
	case input.Unlocked != nil && input.Locked != nil:
		return *input.Unlocked, *input.Unlocked != *input.Locked
	case input.Unlocked != nil:
		return *input.Unlocked, true
	case input.Locked != nil:
		return !*input.Locked, true
	}
	return false, false
}
