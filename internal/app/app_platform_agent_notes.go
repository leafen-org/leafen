package app

import (
	"sync"
	"time"
)

// An installed app's notes for agents working through it reach a
// connection as a rule of that work (app_platform_agents.go): once, again
// when the app changes or after an hour.

const (
	mcpRuleResendAfter  = time.Hour
	mcpRuleTrackedLimit = 4096
)

// leafenMCPRule is an app's notes as a connection receives them.
type leafenMCPRule struct {
	Title string `json:"title"`
	URL   string `json:"url,omitempty"`
	Scope string `json:"scope"`
	Body  string `json:"body,omitempty"`
}

// Which notes each connection has had, and the app's version it had them at.
var mcpRulesSent = struct {
	sync.Mutex
	at map[string]mcpRuleDelivery
}{at: map[string]mcpRuleDelivery{}}

type mcpRuleDelivery struct {
	updated string
	sent    time.Time
}

func mcpRuleDue(key, updated string) bool {
	mcpRulesSent.Lock()
	defer mcpRulesSent.Unlock()
	delivery, ok := mcpRulesSent.at[key]
	return !ok || delivery.updated != updated || time.Since(delivery.sent) > mcpRuleResendAfter
}

func mcpRuleMarkSent(key, updated string) {
	mcpRulesSent.Lock()
	defer mcpRulesSent.Unlock()
	if len(mcpRulesSent.at) >= mcpRuleTrackedLimit {
		for stale, delivery := range mcpRulesSent.at {
			if time.Since(delivery.sent) > mcpRuleResendAfter {
				delete(mcpRulesSent.at, stale)
			}
		}
		if len(mcpRulesSent.at) >= mcpRuleTrackedLimit {
			mcpRulesSent.at = map[string]mcpRuleDelivery{}
		}
	}
	mcpRulesSent.at[key] = mcpRuleDelivery{updated: updated, sent: time.Now()}
}
