// The built-in Team Quiz app's referee (docs/app-platform.md 12, 16): one
// loop for each table while people sit at it. Anyone at the table starts a
// game of up to five of the questions the team added; each question stays
// open for 20 seconds or until everyone answered, the right answer scores
// 10 and up to 10 more for speed, and the table's ranking is kept in the
// results database when the game ends. Leafen says who sent each message:
// a player is bound to the person or guest who sat down with it, so no one
// answers for another; people show by their names, guests by the name they
// give.
package main

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"time"

	"leafen.org/leafen/sdk/app/go/leafen"
)

const (
	questionsPerGame = 5
	questionTime     = 20 * time.Second
	revealTime       = 5 * time.Second
	endTime          = 15 * time.Second
)

type player struct {
	ID       string
	Sender   string
	Name     string
	Score    int
	Answer   int
	Answered bool
	Correct  bool
	Joined   int
}

type game struct {
	room, tenancy string
	phase         string // lobby, question, reveal, end
	players       map[string]*player
	joined        int
	questions     []question
	index         int
	deadline      time.Time
	ranking       []rank
	notice        string
}

type rank struct {
	Name  string `json:"name"`
	Score int    `json:"score"`
}

type incoming struct {
	T      string `json:"t"`
	ID     string `json:"id"`
	Guest  string `json:"guest"`
	Name   string `json:"name"`
	Q      int    `json:"q"`
	Choice int    `json:"choice"`
}

// handle applies one message from the table, from the sender Leafen names.
func (g *game) handle(raw string, from leafen.RoomSender) bool {
	message := incoming{}
	sender := from.Person
	if sender == "" && from.Guest != "" {
		sender = "guest:" + from.Guest
	}
	if json.Unmarshal([]byte(raw), &message) != nil || message.ID == "" || len(message.ID) > 64 || sender == "" {
		return false
	}
	// A player answers only for the one who sat down with it.
	seated := g.players[message.ID]
	if seated != nil && seated.Sender != sender {
		return false
	}
	switch message.T {
	case "hello":
		name := strings.TrimSpace(message.Name)
		if from.Person != "" && strings.TrimSpace(from.Name) != "" {
			name = strings.TrimSpace(from.Name)
		}
		if name == "" {
			name = "?"
		}
		if seated != nil {
			seated.Name = name
			return true
		}
		g.joined++
		g.players[message.ID] = &player{ID: message.ID, Sender: sender, Name: name, Joined: g.joined}
		return true
	case "bye":
		if seated == nil {
			return false
		}
		delete(g.players, message.ID)
		return true
	case "remove":
		// A person at the table took a guest out (Leafen blocked them).
		if from.Person == "" || message.Guest == "" {
			return false
		}
		removed := false
		for id, one := range g.players {
			if one.Sender == "guest:"+message.Guest {
				delete(g.players, id)
				removed = true
			}
		}
		return removed
	case "start":
		if g.phase != "lobby" || seated == nil {
			return false
		}
		questions, err := loadQuestions(g.tenancy)
		if err != nil {
			leafen.Log(err.Error())
			g.notice = "noQuestions"
			return true
		}
		if len(questions) == 0 {
			g.notice = "noQuestions"
			return true
		}
		rand.Shuffle(len(questions), func(i, j int) { questions[i], questions[j] = questions[j], questions[i] })
		if len(questions) > questionsPerGame {
			questions = questions[:questionsPerGame]
		}
		g.questions, g.index, g.notice = questions, 0, ""
		for _, one := range g.players {
			one.Score = 0
		}
		g.ask()
		return true
	case "answer":
		one := seated
		if g.phase != "question" || one == nil || one.Answered || message.Q != g.index || message.Choice < 1 || message.Choice > 4 {
			return false
		}
		one.Answered, one.Answer = true, message.Choice
		if message.Choice == g.questions[g.index].Answer {
			one.Correct = true
			one.Score += 10 + int(time.Until(g.deadline)/(questionTime/10))
		}
		if g.everyoneAnswered() {
			g.reveal()
		}
		return true
	}
	return false
}

func (g *game) everyoneAnswered() bool {
	for _, one := range g.players {
		if !one.Answered {
			return false
		}
	}
	return len(g.players) > 0
}

func (g *game) ask() {
	g.phase, g.deadline = "question", time.Now().Add(questionTime)
	for _, one := range g.players {
		one.Answered, one.Answer, one.Correct = false, 0, false
	}
}

func (g *game) reveal() {
	g.phase, g.deadline = "reveal", time.Now().Add(revealTime)
}

// tick moves the game on when its time is up.
func (g *game) tick() bool {
	if g.phase == "lobby" || time.Now().Before(g.deadline) {
		return false
	}
	switch g.phase {
	case "question":
		g.reveal()
	case "reveal":
		if g.index+1 < len(g.questions) {
			g.index++
			g.ask()
		} else {
			g.finish()
		}
	case "end":
		g.phase, g.questions, g.ranking = "lobby", nil, nil
	}
	return true
}

func (g *game) finish() {
	g.ranking = g.ranks()
	lines := []string{}
	for index, entry := range g.ranking {
		lines = append(lines, fmt.Sprintf("%d. %s %d점", index+1, entry.Name, entry.Score))
	}
	if err := saveResult(g.tenancy, g.room, strings.Join(lines, "\n"), len(g.questions)); err != nil {
		leafen.Log("result not kept: " + err.Error())
	}
	g.phase, g.deadline = "end", time.Now().Add(endTime)
}

func (g *game) ranks() []rank {
	list := []rank{}
	ordered := make([]*player, 0, len(g.players))
	for _, one := range g.players {
		ordered = append(ordered, one)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Score != ordered[j].Score {
			return ordered[i].Score > ordered[j].Score
		}
		return ordered[i].Joined < ordered[j].Joined
	})
	for _, one := range ordered {
		list = append(list, rank{Name: one.Name, Score: one.Score})
	}
	return list
}

// state is what everyone at the table sees.
func (g *game) state() string {
	players := []map[string]any{}
	ordered := make([]*player, 0, len(g.players))
	for _, one := range g.players {
		ordered = append(ordered, one)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Joined < ordered[j].Joined })
	for _, one := range ordered {
		entry := map[string]any{"id": one.ID, "name": one.Name, "score": one.Score, "answered": one.Answered, "correct": one.Correct}
		// A guest's ID lets the people at the table report or block them.
		if guest, ok := strings.CutPrefix(one.Sender, "guest:"); ok {
			entry["guest"] = guest
		}
		players = append(players, entry)
	}
	out := map[string]any{"t": "state", "phase": g.phase, "players": players}
	if g.notice != "" {
		out["notice"] = g.notice
	}
	if g.phase == "question" || g.phase == "reveal" {
		current := g.questions[g.index]
		out["question"] = map[string]any{"n": g.index, "total": len(g.questions), "text": current.Text, "choices": current.Choices}
		out["remainingMs"] = max(time.Until(g.deadline).Milliseconds(), 0)
	}
	if g.phase == "reveal" {
		out["answer"] = g.questions[g.index].Answer
	}
	if g.phase == "end" {
		out["ranking"] = g.ranking
	}
	encoded, _ := json.Marshal(out)
	return string(encoded)
}

func (g *game) wait() time.Duration {
	if g.phase == "lobby" {
		return 30 * time.Second
	}
	return max(min(time.Until(g.deadline), time.Second), 50*time.Millisecond)
}

// referee runs a table's game while people sit at it.
//
//go:wasmexport referee
func referee() int32 {
	call := struct {
		Room    string `json:"room"`
		Tenancy string `json:"tenancy"`
	}{}
	if err := leafen.InputJSON(&call); err != nil {
		return leafen.Fail("the input isn't JSON")
	}
	g := &game{room: call.Room, tenancy: call.Tenancy, phase: "lobby", players: map[string]*player{}}
	for {
		messages, stop, _, err := leafen.RoomNextFrom(g.wait())
		if err != nil {
			return leafen.Fail(err.Error())
		}
		if stop {
			return 0
		}
		changed := false
		for _, message := range messages {
			changed = g.handle(message.Text, message.From) || changed
		}
		changed = g.tick() || changed
		// While a question is open, everyone's clock is kept in step.
		if changed || g.phase == "question" {
			if err := leafen.RoomSend(g.state()); err != nil {
				leafen.Log("not sent: " + err.Error())
			}
		}
	}
}

func main() {}
