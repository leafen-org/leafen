package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// What a person does in Leafen's Vault app, as themselves: prepare their
// vault (which lets one Bitwarden app make the account for them), see it,
// sign every device out, or start over when the master password is lost.
// And the daily tidy, which closes the vaults of people the organization no
// longer lets use the app.

// statusOf is what the app's screen shows of a person's vault.
func statusOf(s *store, acct account, found bool) map[string]any {
	answer := map[string]any{"organization": strings.TrimPrefix(s.tenancy, "org:"), "state": "none"}
	if !found {
		return answer
	}
	answer["email"] = acct.Email
	switch {
	case acct.Status == statusActive:
		answer["state"], answer["created"] = "active", acct.Created
		devices := []any{}
		if rows, err := owned(s.devices, acct.row.ID); err == nil {
			for _, device := range rows {
				if device.Text("Stamp") == acct.Stamp {
					devices = append(devices, map[string]any{"name": device.Text("Name"), "type": number(device, "Type"), "seen": number(device, "Seen")})
				}
			}
		}
		answer["devices"] = devices
		if count, err := s.ciphers.Count(leafen.Query{Where: []leafen.Where{{Column: "Account", Operator: "contains", Value: acct.row.ID}}}); err == nil {
			answer["items"] = count
		}
	case acct.waiting(nowMs()):
		answer["state"], answer["expires"] = "pending", acct.Expires
	}
	return answer
}

// personCall reads an action's call and finds the person's vault.
func personCall(input any) (leafen.ActionCall, *store, account, bool, error) {
	call, err := leafen.Action(input)
	if err != nil || call.Person == "" || call.Tenancy == "" {
		return call, nil, account{}, false, errors.New("open the Vault app in an organization")
	}
	s, err := openStore(call.Tenancy)
	if err != nil {
		return call, nil, account{}, false, err
	}
	acct, err := s.accountByPerson(call.Person)
	if errors.Is(err, errMissing) {
		return call, s, account{}, false, nil
	}
	return call, s, acct, err == nil, err
}

// status shows the person their vault.
//
//go:wasmexport status
func status() int32 {
	call, s, acct, found, err := personCall(nil)
	if err != nil {
		return fail(err.Error())
	}
	answer := statusOf(s, acct, found)
	// The organization's administrators also see its shared vaults.
	if oversees(call) {
		answer["oversees"] = true
	}
	_ = leafen.OutputJSON(answer)
	return 0
}

// prepare readies a vault for the email the person will give their
// Bitwarden app, for a day.
//
//go:wasmexport prepare
func prepare() int32 {
	input := struct {
		Email string `json:"email"`
	}{}
	call, s, acct, found, err := personCall(&input)
	if err != nil {
		return fail(err.Error())
	}
	email := normalEmail(input.Email)
	if parsed, err := mail.ParseAddress(email); err != nil || parsed.Address != email || len(email) > 254 {
		return fail("give an email address")
	}
	if found && acct.Status == statusActive {
		return fail("your vault is ready; log in to it from your Bitwarden app")
	}
	if theirs, err := someoneElsesEmail(call, email); err != nil {
		return fail(err.Error())
	} else if theirs {
		return fail("that email is someone else's in Leafen; use your own")
	}
	other, err := s.accountByEmail(email)
	switch {
	case err == nil && (!found || other.row.ID != acct.row.ID) && (other.Status == statusActive || other.waiting(nowMs())):
		return fail("that email has a vault here; choose another")
	case err == nil && (!found || other.row.ID != acct.row.ID):
		// Someone's prepared vault whose day passed gives the email up.
		budget := 60
		if err := closeAccount(s, other, &budget); err != nil {
			return fail(err.Error())
		}
	case err != nil && !errors.Is(err, errMissing):
		return fail(err.Error())
	}
	now := nowMs()
	values := map[string]any{"Email": email, "Person": call.Person, "Status": statusPending, "Created": float64(now),
		"Revision": float64(now), "Expires": float64(now + preparedFor)}
	if found {
		err = s.accounts.Update(acct.row.ID, values)
	} else {
		_, err = s.accounts.Add(values)
	}
	if err != nil {
		return fail(err.Error())
	}
	acct, err = s.accountByPerson(call.Person)
	if err != nil {
		return fail(err.Error())
	}
	_ = leafen.OutputJSON(statusOf(s, acct, true))
	return 0
}

// someoneElsesEmail says whether the email is one another of the app's people
// signs in to Leafen with. Shared vaults invite by a vault's email, so nobody
// takes an address that would bring them what was meant for its owner.
func someoneElsesEmail(call leafen.ActionCall, email string) (bool, error) {
	answer, err := leafen.API(leafen.APIRequest{Tenancy: call.Tenancy, Method: "GET", Path: "/app-people", Query: "email=" + url.QueryEscape(email)})
	if err != nil {
		return false, err
	}
	people := struct {
		People []struct {
			ID string `json:"id"`
		} `json:"people"`
	}{}
	if answer.Status != 200 || json.Unmarshal(answer.Body, &people) != nil {
		return false, errors.New("the app's people couldn't be read")
	}
	for _, person := range people.People {
		if person.ID != call.Person {
			return true, nil
		}
	}
	return false, nil
}

// signout ends every device's sign-in.
//
//go:wasmexport signout
func signout() int32 {
	_, s, acct, found, err := personCall(nil)
	if err != nil {
		return fail(err.Error())
	}
	if !found || acct.Status != statusActive {
		return fail("you have no vault here")
	}
	if err := s.accounts.Update(acct.row.ID, map[string]any{"Stamp": randomToken(18)}); err != nil {
		return fail(err.Error())
	}
	acct, _ = s.accountByRow(acct.row.ID)
	_ = leafen.OutputJSON(statusOf(s, acct, true))
	return 0
}

// reset closes the person's vault and everything in it, so they can
// prepare a new one; with the master password lost, nothing in it can be
// opened again anyway.
//
//go:wasmexport reset
func reset() int32 {
	_, s, acct, found, err := personCall(nil)
	if err != nil {
		return fail(err.Error())
	}
	if found {
		budget := 150
		if err := closeAccount(s, acct, &budget); err != nil {
			return fail(err.Error())
		}
	}
	_ = leafen.OutputJSON(statusOf(s, account{}, false))
	return 0
}

// closeAccount ends a vault: no sign-in and no email from then on, keys
// and hashes gone, and what its items, folders and devices kept wiped. Their
// emptied rows go to the trash as far as budget, a run's host calls to
// spend, allows, and the nightly tidy goes on with the rest; the account's
// row goes last.
func closeAccount(s *store, acct account, budget *int) error {
	// Closing and wiping take about ten calls before any row is trashed.
	if *budget < 20 {
		return nil
	}
	*budget -= 10
	if err := s.accounts.Update(acct.row.ID, map[string]any{"Status": statusClosed, "Email": "", "Key": "", "Public key": "",
		"Private key": "", "Hash": "", "Salt": "", "Hint": "", "Name": "", "Stamp": randomToken(18), "Revision": float64(nowMs())}); err != nil {
		return err
	}
	// Its places in shared vaults go, and the others there are told.
	places, err := s.memberships(acct)
	if err != nil {
		return err
	}
	for _, m := range places {
		org, err := s.organizationByID(bitwardenID(m.Organization))
		if errors.Is(err, errMissing) {
			continue
		}
		if err != nil {
			return err
		}
		if err := s.removeMembers(org, []member{m}); err != nil {
			return err
		}
	}
	wiped := map[*leafen.Table]map[string]any{
		s.ciphers: {"Data": "", "Kind": ""}, s.folders: {"Name": ""}, s.devices: {"Refresh": "", "Stamp": "", "Name": ""},
	}
	for table, values := range wiped {
		rows, err := owned(table, acct.row.ID)
		if err != nil {
			return err
		}
		if err := writeRows(table, rows, values); err != nil {
			return err
		}
	}
	return clearAccount(s, acct, budget)
}

// clearAccount trashes a closed vault's emptied rows, then the account's.
func clearAccount(s *store, acct account, budget *int) error {
	for _, table := range []*leafen.Table{s.ciphers, s.folders, s.devices} {
		rows, err := owned(table, acct.row.ID)
		if err != nil {
			return err
		}
		if done, err := trashRows(s.tenancy, idsOf(rows), budget); err != nil || !done {
			return err
		}
	}
	_, err := trashRows(s.tenancy, []string{acct.row.ID}, budget)
	return err
}

// tidy goes through an organization's vaults each night: a closed one's
// rows go to the trash, and a prepared one whose day passed goes. The vault
// of someone who may no longer use the app stays whole: they can't sign in
// (passwordGrant and the token refresh ask mayUse), and if they may again it
// is as it was. Only they close it (reset), or an administrator's decision
// does (2026-10-13: it closed and was wiped the first night, though a group
// change or a SCIM sync may take access away by mistake, and end-to-end
// encryption leaves no way back).
//
//go:wasmexport tidy
func tidy() int32 {
	call := leafen.ScheduleCall{}
	if err := leafen.InputJSON(&call); err != nil || call.Tenancy == "" {
		return fail("the schedule names no organization")
	}
	s, err := openStore(call.Tenancy)
	if errors.Is(err, errNotSetUp) {
		return 0
	}
	if err != nil {
		return fail(err.Error())
	}
	now := nowMs()
	gone := []account{}
	for row, err := range s.accounts.Rows(leafen.Query{}) {
		if err != nil {
			return fail(err.Error())
		}
		switch acct := accountOf(row); {
		case acct.Status == statusActive:
		case acct.Status == statusClosed || !acct.waiting(now):
			gone = append(gone, acct)
		}
	}
	// A run has 200 host calls; reading the accounts took some.
	budget := 120
	for _, acct := range gone {
		if err := closeAccount(s, acct, &budget); err != nil {
			return fail(err.Error())
		}
	}
	if len(gone) > 0 {
		leafen.Log(fmt.Sprintf("closed %d vaults", len(gone)))
	}
	return 0
}
