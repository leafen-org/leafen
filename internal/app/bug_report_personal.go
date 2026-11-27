package app

import (
	"net"
	"net/url"
	"regexp"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A report goes to a public tracker, so it holds made-up data only
// (docs/bug-reports.md). bugReportPersonal names what in a draft looks like
// the person's own — their name or mail address, this server's address,
// another real mail address, a key or token — for the agent to replace
// before showing it; it names each by kind, never repeating it.

var (
	bugReportMailAddress = regexp.MustCompile(`[A-Za-z0-9._%+-]+@([A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+)`)
	bugReportTokenShape  = regexp.MustCompile(`\b(?:l[a-z]{1,4}|ntn|secret|ghp|gho|ghu|ghs|github_pat|sk|xox[abpr])_[A-Za-z0-9_-]{16,}`)
)

// bugReportMadeUpDomain is a mail domain kept for examples (RFC 2606).
func bugReportMadeUpDomain(domain string) bool {
	domain = strings.ToLower(domain)
	for _, kept := range []string{"example.com", "example.org", "example.net"} {
		if domain == kept || strings.HasSuffix(domain, "."+kept) {
			return true
		}
	}
	for _, kept := range []string{".test", ".example", ".invalid", ".localhost"} {
		if strings.HasSuffix(domain, kept) {
			return true
		}
	}
	return false
}

func bugReportPersonal(app core.App, authority mcpAuthority, texts ...string) []string {
	text := strings.Join(texts, "\n")
	lower := strings.ToLower(text)
	found := []string{}
	add := func(kind string) {
		for _, each := range found {
			if each == kind {
				return
			}
		}
		found = append(found, kind)
	}
	if record, err := app.FindRecordById("users", authority.owner()); err == nil {
		if name := strings.TrimSpace(record.GetString("name")); len([]rune(name)) >= 2 && strings.Contains(lower, strings.ToLower(name)) {
			add("the person's name")
		}
		if email := strings.TrimSpace(record.GetString("email")); email != "" && strings.Contains(lower, strings.ToLower(email)) {
			add("the person's mail address")
		}
	}
	if parsed, err := url.Parse(authority.Resource); err == nil && parsed.Hostname() != "" {
		host := strings.ToLower(parsed.Hostname())
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) && strings.Contains(lower, host) {
			add("this server's address")
		}
	}
	for _, match := range bugReportMailAddress.FindAllStringSubmatch(text, -1) {
		if !bugReportMadeUpDomain(match[1]) {
			add("a real-looking mail address (use one at example.com)")
		}
	}
	if bugReportTokenShape.MatchString(text) {
		add("a key or token")
	}
	return found
}
