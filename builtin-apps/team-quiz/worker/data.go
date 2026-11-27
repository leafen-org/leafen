package main

import (
	"fmt"
	"strings"
	"time"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// The quiz's two databases in the organization's app workspace, read and
// written as the app's account: the questions people add, and the results
// only the app writes.

type question struct {
	Text    string
	Choices [4]string
	Answer  int // 1 to 4
}

// loadQuestions reads the questions people added; one without its text,
// its four choices or an answer from 1 to 4 is left out.
func loadQuestions(tenancy string) ([]question, error) {
	found, err := leafen.OpenTable(tenancy, "questions")
	if err != nil {
		return nil, err
	}
	names := []string{"질문", "보기 1", "보기 2", "보기 3", "보기 4", "정답"}
	for _, name := range names {
		if _, ok := found.Column(name); !ok {
			return nil, fmt.Errorf("the questions have no %q column", name)
		}
	}
	questions := []question{}
	for row, err := range found.Rows(leafen.Query{}) {
		if err != nil {
			return nil, err
		}
		item := question{Text: row.Text("질문")}
		answer := row.Number("정답")
		item.Answer = int(answer)
		complete := strings.TrimSpace(item.Text) != "" && item.Answer >= 1 && item.Answer <= 4 && float64(item.Answer) == answer
		for index := range item.Choices {
			item.Choices[index] = row.Text(fmt.Sprintf("보기 %d", index+1))
			complete = complete && strings.TrimSpace(item.Choices[index]) != ""
		}
		if complete {
			questions = append(questions, item)
		}
	}
	return questions, nil
}

// saveResult keeps a finished game in the results database.
func saveResult(tenancy, room, ranking string, count int) error {
	found, err := leafen.OpenTable(tenancy, "results")
	if err != nil {
		return err
	}
	_, err = found.Add(map[string]any{"방": room, "끝난 때": time.Now().UTC().Format("2006-01-02"), "순위": ranking, "문제 수": count})
	return err
}
