// The built-in Approvals app's rules (docs/app-platform.md 16): a request is
// asked for, decided and listed only through these functions, which run as
// the app's account and change the requests database only when the rules
// hold — whoever calls them: the app's screen, a menu line or an agent. The
// app's screen and an agent can be changed or led astray; these rules can't.
package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// A request's title, reason and a decision's note are as long as the person
// writes them, held only to the action's input (2026-10-13: 200, 2,000 and
// 2,000 characters were refused, and the screen stopped typing there).
const amountMax = 1e13

// pageIDPattern is a page's ID as Leafen opens one from an app (the
// screen's openPage): a UUID, as every page has had since 2026-11-14, the
// 15 characters of one made before, or a row's longer ID — letters, digits,
// "_" and "-". The page a request links to is checked here alone: the
// action's input and the app's screen pass it on as it is (2026-11-15: the
// 15 characters alone were taken, so no request could link a page).
var pageIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,255}$`)

func fail(message string) int32 { return leafen.Fail(message) }

func notify(tenancy, person, text, path string) {
	if person == "" {
		return
	}
	if err := leafen.Notify(leafen.NoticeRequest{Person: person, Text: text, Path: path, Tenancy: tenancy}); err != nil {
		leafen.Log("notice not sent: " + err.Error())
	}
}

func short(text string) string {
	if utf8.RuneCountInString(text) <= 60 {
		return text
	}
	return string([]rune(text)[:59]) + "…"
}

// submit asks for approval: a new request, or the person's own draft sent
// on. The approver hears of it.
//
//go:wasmexport submit
func submit() int32 {
	input := struct {
		RequestID string  `json:"requestId"`
		Title     string  `json:"title"`
		Approver  string  `json:"approver"`
		Amount    float64 `json:"amount"`
		Reason    string  `json:"reason"`
		PageID    string  `json:"pageId"`
	}{}
	call, err := leafen.Action(&input)
	if err != nil {
		return fail("the input isn't JSON")
	}
	input.Title, input.Reason, input.Approver = strings.TrimSpace(input.Title), strings.TrimSpace(input.Reason), strings.TrimSpace(input.Approver)
	switch {
	case input.Title == "":
		return fail("a request needs a title")
	case input.Approver == "" || input.Approver == call.Person:
		return fail("choose someone else to approve it")
	case input.Amount < 0 || input.Amount > amountMax:
		return fail("the amount is 0 or more")
	case input.PageID != "" && !pageIDPattern.MatchString(input.PageID):
		return fail("that isn't a page")
	}
	found, err := loadRequests(call.Tenancy)
	if err != nil {
		return fail(err.Error())
	}
	page := ""
	if input.PageID != "" {
		page = "/p/" + input.PageID
	}
	if input.RequestID != "" {
		existing, ok, err := found.find(input.RequestID)
		if err != nil {
			return fail(err.Error())
		}
		if !ok || existing.Requester != call.Person || existing.Status != statusDraft {
			return fail("only your own draft can be sent")
		}
		if page == "" {
			page = existing.Page
		}
	}
	if err := found.quietPeople(); err != nil {
		return fail(err.Error())
	}
	values := map[string]any{
		columnTitle: input.Title, columnStatus: statusWaiting, columnRequester: []string{call.Person},
		columnApprover: []string{input.Approver}, columnAmount: input.Amount, columnReason: input.Reason, columnPage: page,
	}
	id := input.RequestID
	if id != "" {
		err = found.Update(id, values)
	} else {
		id, err = found.Add(values)
	}
	if err != nil {
		return fail(err.Error())
	}
	notify(call.Tenancy, input.Approver, "결재 요청: "+short(input.Title), "waiting")
	_ = leafen.OutputJSON(map[string]any{"requestId": id})
	return 0
}

// draft starts a request from a page or a database row's menu, linked to
// it; the person finishes and sends it in the app.
//
//go:wasmexport draft
func draft() int32 {
	input := struct {
		PageID string `json:"pageId"`
	}{}
	call, err := leafen.Action(&input)
	if err != nil {
		return fail("the input isn't JSON")
	}
	if !pageIDPattern.MatchString(input.PageID) {
		return fail("that isn't a page")
	}
	found, err := loadRequests(call.Tenancy)
	if err != nil {
		return fail(err.Error())
	}
	if err := found.quietPeople(); err != nil {
		return fail(err.Error())
	}
	id, err := found.Add(map[string]any{
		columnStatus: statusDraft, columnRequester: []string{call.Person}, columnPage: "/p/" + input.PageID,
	})
	if err != nil {
		return fail(err.Error())
	}
	_ = leafen.OutputJSON(map[string]any{"requestId": id})
	return 0
}

// decide approves or rejects a waiting request: only its approver, only
// once. The person who asked hears of it.
//
//go:wasmexport decide
func decide() int32 {
	input := struct {
		RequestID string `json:"requestId"`
		Note      string `json:"note"`
	}{}
	call, err := leafen.Action(&input)
	if err != nil {
		return fail("the input isn't JSON")
	}
	input.Note = strings.TrimSpace(input.Note)
	decision := map[string]string{"approve": statusApproved, "reject": statusRejected}[call.Action]
	if decision == "" {
		return fail("approve or reject")
	}
	found, err := loadRequests(call.Tenancy)
	if err != nil {
		return fail(err.Error())
	}
	existing, ok, err := found.find(input.RequestID)
	switch {
	case err != nil:
		return fail(err.Error())
	case !ok:
		return fail("no such request")
	case existing.Approver != call.Person:
		return fail("only its approver decides it")
	case existing.Status != statusWaiting:
		return fail("it was already decided")
	}
	if err := found.Update(existing.ID, map[string]any{
		columnStatus: decision, columnNote: input.Note, columnDecided: today(),
	}); err != nil {
		return fail(err.Error())
	}
	verb := map[string]string{statusApproved: "승인", statusRejected: "반려"}[decision]
	notify(call.Tenancy, existing.Requester, fmt.Sprintf("결재 %s: %s", verb, short(existing.Title)), "mine")
	_ = leafen.OutputJSON(map[string]any{"requestId": existing.ID, "status": decision})
	return 0
}

// pending lists what waits for the person to decide.
//
//go:wasmexport pending
func pending() int32 {
	call, err := leafen.Action(nil)
	if err != nil {
		return fail("the input isn't JSON")
	}
	found, err := loadRequests(call.Tenancy)
	if err != nil {
		return fail(err.Error())
	}
	waiting := []request{}
	if err := found.each(waitingFor(call.Person), func(item request) { waiting = append(waiting, item) }); err != nil {
		return fail(err.Error())
	}
	_ = leafen.OutputJSON(map[string]any{"waiting": waiting})
	return 0
}

// overview is the person's own view: what waits for them, what they asked
// for, and how many requests the organization has in each state.
//
//go:wasmexport overview
func overview() int32 {
	call, err := leafen.Action(nil)
	if err != nil {
		return fail("the input isn't JSON")
	}
	found, err := loadRequests(call.Tenancy)
	if err != nil {
		return fail(err.Error())
	}
	waiting, mine := []request{}, []request{}
	if err := found.each(waitingFor(call.Person), func(item request) { waiting = append(waiting, item) }); err != nil {
		return fail(err.Error())
	}
	if err := found.each([]leafen.Where{{Column: columnRequester, Operator: "contains", Value: call.Person}}, func(item request) {
		mine = append(mine, item)
	}); err != nil {
		return fail(err.Error())
	}
	counts := map[string]int{}
	for _, status := range []string{statusWaiting, statusApproved, statusRejected} {
		if counts[status], err = found.Count(leafen.Query{Where: []leafen.Where{{Column: columnStatus, Operator: "equals", Value: status}}}); err != nil {
			return fail(err.Error())
		}
	}
	_ = leafen.OutputJSON(map[string]any{"waiting": waiting, "mine": mine, "counts": counts})
	return 0
}

// remind tells each person, on weekday mornings, how many requests wait
// for them.
//
//go:wasmexport remind
func remind() int32 {
	call := leafen.ScheduleCall{}
	if err := leafen.InputJSON(&call); err != nil {
		return fail("the input isn't JSON")
	}
	if call.Person == "" {
		return 0
	}
	found, err := loadRequests(call.Tenancy)
	if err != nil {
		// No app workspace yet: nothing waits.
		leafen.Log(err.Error())
		return 0
	}
	count, err := found.Count(leafen.Query{Where: waitingFor(call.Person)})
	if err != nil {
		return fail(err.Error())
	}
	if count > 0 {
		notify(call.Tenancy, call.Person, fmt.Sprintf("결재 대기 %d건이 있습니다", count), "waiting")
	}
	return 0
}

func main() {}
