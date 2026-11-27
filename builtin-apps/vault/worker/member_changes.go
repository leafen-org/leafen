package main

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// What follows a change of a shared vault's members: the people who confirm
// newcomers are told someone waits, and when someone who had the vault's key
// goes, the others are told to change the passwords that person could see.

// askToConfirm tells the shared vault's owners and administrators that
// people wait for them.
func (s *store) askToConfirm(org organization, members []member, waiting int) {
	if waiting == 0 {
		return
	}
	for _, other := range members {
		if other.manages() {
			notify(s.tenancy, other.Person, fmt.Sprintf("공유 금고 '%s'에 확인을 기다리는 사람 %d명 — 웹 금고의 멤버에서 확인하세요", short(org.Name), waiting))
		}
	}
}

func notify(tenancy, person, text string) {
	if person == "" {
		return
	}
	if err := leafen.Notify(leafen.NoticeRequest{Person: person, Text: text, Path: "shared", Tenancy: tenancy}); err != nil {
		leafen.Log("notice not sent: " + err.Error())
	}
}

func short(text string) string {
	if utf8.RuneCountInString(text) <= 40 {
		return text
	}
	return string([]rune(text)[:39]) + "…"
}

// revoke stops members' access for now, or gives it back; a revoked
// member keeps their place and their key.
func (s *store) revoke(org organization, targets []member, revoke bool) error {
	for _, target := range targets {
		state := "confirmed"
		switch {
		case revoke:
			state = "revoked"
		case target.Key == "":
			state = "accepted"
		}
		if err := s.members.Update(target.row.ID, map[string]any{"Status": state, "Revision": float64(nowMs())}); err != nil {
			return err
		}
	}
	s.orgChanged(org.row.ID)
	if revoke {
		s.tellToChange(org, targets)
	}
	return nil
}

// removeMembers takes members out of a shared vault and tells the others
// to change the passwords those members could see.
func (s *store) removeMembers(org organization, targets []member) error {
	budget := 2 * len(targets)
	if err := writeRows(s.members, rowsOf(targets), map[string]any{"Key": ""}); err != nil {
		return err
	}
	if _, err := trashRows(s.tenancy, idsOfMembers(targets), &budget); err != nil {
		return err
	}
	s.tellToChange(org, targets)
	return s.orgRemoved(org.row.ID)
}

// tellToChange asks the shared vault's remaining members to change the
// passwords people who had its key could see: their devices may keep a
// copy.
func (s *store) tellToChange(org organization, gone []member) {
	keyed := []string{}
	for _, target := range gone {
		if target.Key != "" {
			keyed = append(keyed, target.Email)
		}
	}
	if len(keyed) == 0 {
		return
	}
	members, err := s.membersOf(org)
	if err != nil {
		leafen.Log("members not read: " + err.Error())
		return
	}
	left := map[string]bool{}
	for _, target := range gone {
		left[target.row.ID] = true
	}
	for _, other := range members {
		if !left[other.row.ID] && other.State == memberConfirmed {
			notify(s.tenancy, other.Person, fmt.Sprintf("공유 금고 '%s'에서 %s님이 빠졌습니다 — 그 사람이 볼 수 있던 비밀번호를 바꾸세요(기기에 사본이 남을 수 있음)",
				short(org.Name), strings.Join(keyed, ", ")))
		}
	}
}

func rowsOf(members []member) []leafen.Row {
	rows := make([]leafen.Row, 0, len(members))
	for _, m := range members {
		rows = append(rows, m.row)
	}
	return rows
}

func idsOfMembers(members []member) []string { return idsOf(rowsOf(members)) }
