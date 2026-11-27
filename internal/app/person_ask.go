package app

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// One boundary asks the person behind an agent's request before it goes on,
// inside their conversation with the agent and never through a link to a
// Leafen page (decided 2026-11-13), and always the one way: the agent asks
// its person in the conversation in the words Leafen gives and passes their
// yes on with the request's id (decided 2026-11-21: no MCP elicitation form
// — a program that says it takes forms may close one by itself unseen, as
// the Claude desktop app's Code tab did within a second, and Leafen can't
// tell that from a person's no). Leafen can't tell a yes passed on from the
// agent's own, as the token's page says. What a person does on Leafen's own
// screens keeps its screen's confirm.
//
// Each kind of request keeps its waiting requests in its own place
// (personAskRequests): Leafen's own actions and an outside service's tools
// in leafen_mcp_service_approvals (mcp_service_approvals.go). An app's
// tools, a connection's consent and the first run's name ask through here
// the same way: words for the question (personAsk), a place for requests,
// and the yes the call passes on (personAskCall).

// personAsk is one question, in the person's language: what is asked, what
// a yes lets happen, the input as sent, and what "for an hour" or "always"
// would allow when they are offered.
type personAsk struct {
	Question string `json:"question"`
	Effect   string `json:"effect,omitempty"`
	Input    string `json:"input,omitempty"`
	ForHour  string `json:"forHour,omitempty"`
	Always   string `json:"always,omitempty"`
	Language string `json:"language"`
}

// personAskCall is what the call brings: the id of a request the agent
// passes on after asking in the conversation — "<id>:hour" or
// "<id>:always" where the person said yes for an hour or always, where the
// question offered that.
type personAskCall struct {
	PassedOn string
}

// personAskRequests keeps one kind of request: the request this call waits
// on (the same call still waiting is that request), how a request stands for
// this call ("pending", "approved", "declined", "used", or "" when there is
// no such request or it ran out), the person's answer, and a yes spent once.
type personAskRequests interface {
	waiting() (id string, expiresAt int64, err error)
	status(id string) string
	decide(id string, yes, forHour, always bool) error
	spend(id string) error
}

// personAskOutcome is how the call goes on: at once, refused because the
// person said no earlier, or waiting for them on request ID.
type personAskOutcome struct {
	Proceed   bool
	Declined  bool
	ID        string
	ExpiresAt int64
}

// Where the person's yes was for longer than once, the id passed on ends so.
const (
	personAskPassedForHour = ":hour"
	personAskPassedAlways  = ":always"
)

// askPerson decides a call that needs its person's yes.
func askPerson(requests personAskRequests, call personAskCall, ask personAsk) (personAskOutcome, error) {
	if passed := strings.TrimSpace(call.PassedOn); passed != "" {
		id, forHour, always := passed, false, false
		if cut, found := strings.CutSuffix(passed, personAskPassedForHour); found {
			id, forHour = cut, ask.ForHour != ""
		} else if cut, found := strings.CutSuffix(passed, personAskPassedAlways); found {
			id, always = cut, ask.Always != ""
		}
		switch requests.status(id) {
		case "pending":
			if err := requests.decide(id, true, forHour, always); err != nil {
				return personAskOutcome{}, err
			}
			fallthrough
		case "approved":
			if err := requests.spend(id); err != nil {
				return personAskOutcome{}, err
			}
			return personAskOutcome{Proceed: true}, nil
		case "declined":
			return personAskOutcome{Declined: true}, nil
		case "used":
			return personAskOutcome{}, fmt.Errorf("%w: that yes was used; call without it to ask again", errMCPToolInvalid)
		default:
			return personAskOutcome{}, fmt.Errorf("%w: no such request waits for this call; call without it to ask again", errMCPToolInvalid)
		}
	}
	id, expiresAt, err := requests.waiting()
	if err != nil {
		return personAskOutcome{}, err
	}
	return personAskOutcome{ID: id, ExpiresAt: expiresAt}, nil
}

// The longer yes a question offers, said to the person beside it.
var (
	personAskWordsForHour = mcpWords{"“1시간 동안”으로 답하면: %s", "If you answer “for an hour”: %s"}
	personAskWordsAlways  = mcpWords{"“항상”으로 답하면: %s", "If you answer “always”: %s"}
)

// personAskConversation tells the agent to ask its person in the
// conversation, and how to pass a yes on (passOn, which names the request's
// id): the question, what a yes lets happen and the longer yes it offers,
// without the input it sent itself, each of its lines a sentence of its own.
func personAskConversation(ask personAsk, passOn string) string {
	lines := append(strings.Split(ask.Question, "\n"), ask.Effect)
	if ask.ForHour != "" {
		lines = append(lines, fmt.Sprintf(personAskWordsForHour.in(ask.Language), ask.ForHour))
	}
	if ask.Always != "" {
		lines = append(lines, fmt.Sprintf(personAskWordsAlways.in(ask.Language), ask.Always))
	}
	sentences := []string{}
	for _, line := range lines {
		if sentence := personAskSentence(line); sentence != "" {
			sentences = append(sentences, sentence)
		}
	}
	longer := ""
	if ask.ForHour != "" {
		longer += " If they say yes for an hour, pass the id on with “" + personAskPassedForHour + "” after it."
	}
	if ask.Always != "" {
		longer += " If they say yes always, pass the id on with “" + personAskPassedAlways + "” after it."
	}
	return "Left to do before going on: one question to your person in this conversation, in their language, carrying all that " +
		"this says: “" + strings.Join(sentences, " ") + "” The answer is theirs alone. Only on their yes, " + passOn + longer +
		" On a no, nothing more. The request waits for a day."
}

// personAskSentenceEnds end a sentence in Korean and English alike; an
// aside in brackets may follow ("…할까요? (계정: …)"). A quoted name's own
// mark is the name's, so a line ending in a quote isn't a sentence's end.
const personAskSentenceEnds = ".?!…。？！"

// personAskSentence is one line of a question read out as a sentence: a line
// such as "Organization: …" or "…하려고 합니다: …" that doesn't end one ends
// with a period, so the next line doesn't run on into it.
func personAskSentence(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	end := line
	if strings.HasSuffix(end, ")") {
		for index, depth := len(end)-1, 0; index >= 0; index-- {
			if end[index] == ')' {
				depth++
			} else if end[index] == '(' {
				if depth--; depth == 0 {
					end = strings.TrimSpace(end[:index])
					break
				}
			}
		}
	}
	if last, _ := utf8.DecodeLastRuneInString(end); strings.ContainsRune(personAskSentenceEnds, last) {
		return line
	}
	return line + "."
}
