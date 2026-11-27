package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// accountLabel is how an account is named where its email used to stand in
// for a missing name: its name, its email, or the start of its ID.
func accountLabel(user *core.Record) string {
	if name := strings.TrimSpace(user.GetString("name")); name != "" {
		return name
	}
	if email := strings.TrimSpace(user.Email()); email != "" {
		return email
	}
	if len(user.Id) > 6 {
		return user.Id[:6]
	}
	return user.Id
}

// nameWithEmail is a person in a message's line: their name with the email
// after it in brackets (after a space in English), or the name alone where
// they have no email.
func nameWithEmail(name, email, space string) string {
	if email = strings.TrimSpace(email); email == "" {
		return name
	}
	return name + space + "(" + email + ")"
}
