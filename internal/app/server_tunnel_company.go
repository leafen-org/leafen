package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A company's server is offered its own address first (the agent decision
// of 2026-10-19 10:11): Tailscale's free plan isn't for business, and a
// company's network often blocks it or forbids a public address. A server
// looks like a company's when the administrator choosing its address mails
// from a domain of their own — not one of the common free webmail services —
// or when an organization on it has verified a domain or SAML domain.

// freeMailDomains are the free webmail services most people use, here and
// worldwide (Gmail, Outlook, iCloud, Yahoo, Proton, and Korea's Naver, Daum,
// Kakao and Nate, China's QQ and NetEase): a person, not a company. Any other
// domain is someone's own — a company's, a school's, or a person's who owns a
// domain, for whom their own address suits as well.
var freeMailDomains = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "outlook.com": true, "hotmail.com": true, "live.com": true, "msn.com": true,
	"icloud.com": true, "me.com": true, "mac.com": true, "yahoo.com": true, "proton.me": true, "protonmail.com": true,
	"naver.com": true, "daum.net": true, "hanmail.net": true, "kakao.com": true, "nate.com": true,
	"qq.com": true, "163.com": true, "126.com": true,
}

// companyServer says whether the server looks like a company's.
func companyServer(app core.App, email string) bool {
	if _, domain, found := strings.Cut(strings.ToLower(strings.TrimSpace(email)), "@"); found && domain != "" && !freeMailDomains[domain] {
		return true
	}
	verified := 0
	_ = app.DB().NewQuery(`SELECT
		(SELECT COUNT(*) FROM {{leafen_organization_domains}} WHERE [[status]] = 'verified') +
		(SELECT COUNT(*) FROM {{leafen_saml_domains}} WHERE [[status]] = 'verified')`).Row(&verified)
	return verified > 0
}
