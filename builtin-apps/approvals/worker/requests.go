package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// The requests database the app's data setup made (data/setup.json), read
// and written as the app's account. People only view it; these functions
// are the only way it changes.

const (
	columnTitle     = "제목"
	columnStatus    = "상태"
	columnRequester = "요청자"
	columnApprover  = "결재자"
	columnAmount    = "금액"
	columnReason    = "사유"
	columnPage      = "관련 페이지"
	columnNote      = "결정 메모"
	columnDecided   = "결정일"

	statusDraft    = "초안"
	statusWaiting  = "대기"
	statusApproved = "승인"
	statusRejected = "반려"
)

type requests struct{ *leafen.Table }

// request is a row as the app's screen and agents read it.
type request struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Status      string  `json:"status"`
	Requester   string  `json:"requester"`
	Approver    string  `json:"approver"`
	Amount      float64 `json:"amount"`
	Reason      string  `json:"reason"`
	Page        string  `json:"page"`
	Note        string  `json:"note"`
	Decided     string  `json:"decided"`
	RequestedAt string  `json:"requestedAt"`
}

// loadRequests finds the requests database in the organization's app
// workspace and checks it has every column the rules use.
func loadRequests(tenancy string) (*requests, error) {
	table, err := leafen.OpenTable(tenancy, "requests")
	if err != nil {
		return nil, err
	}
	for _, name := range []string{columnTitle, columnStatus, columnRequester, columnApprover, columnAmount, columnReason, columnPage, columnNote, columnDecided} {
		if _, ok := table.Column(name); !ok {
			return nil, fmt.Errorf("the requests database has no %q column", name)
		}
	}
	return &requests{table}, nil
}

// read turns a row into a request.
func read(row leafen.Row) request {
	first := func(ids []string) string {
		if len(ids) == 0 {
			return ""
		}
		return ids[0]
	}
	return request{
		ID: row.ID, Title: row.Text(columnTitle), Status: row.Option(columnStatus),
		Requester: first(row.IDs(columnRequester)), Approver: first(row.IDs(columnApprover)),
		Amount: row.Number(columnAmount), Reason: row.Text(columnReason), Page: row.Text(columnPage),
		Note: row.Text(columnNote), Decided: row.Date(columnDecided).Start, RequestedAt: row.CreatedAt,
	}
}

// find reads one request by its ID.
func (r *requests) find(id string) (request, bool, error) {
	row, err := r.Row(id)
	if errors.Is(err, leafen.ErrNoRow) {
		return request{}, false, nil
	}
	if err != nil {
		return request{}, false, err
	}
	return read(row), true, nil
}

// each goes through the requests every condition holds for.
func (r *requests) each(where []leafen.Where, visit func(request)) error {
	for row, err := range r.Rows(leafen.Query{Where: where}) {
		if err != nil {
			return err
		}
		visit(read(row))
	}
	return nil
}

// waitingFor keeps what waits for a person's decision.
func waitingFor(person string) []leafen.Where {
	return []leafen.Where{
		{Column: columnApprover, Operator: "contains", Value: person},
		{Column: columnStatus, Operator: "equals", Value: statusWaiting},
	}
}

// quietPeople keeps the people columns from mentioning whom they name: the
// app tells them itself, once, with a link into the app.
func (r *requests) quietPeople() error {
	operations := []any{}
	for _, name := range []string{columnRequester, columnApprover} {
		column, _ := r.Column(name)
		config := map[string]any{}
		for key, value := range column.Config {
			config[key] = value
		}
		if config["notify"] == "none" {
			continue
		}
		config["notify"] = "none"
		operations = append(operations, map[string]any{"kind": "upsertProperty", "property": map[string]any{
			"id": column.ID, "dataSourceId": r.DataSourceID, "type": column.Type, "name": column.Name,
			"position": column.Position, "config": config,
		}})
	}
	if len(operations) == 0 {
		return nil
	}
	answer, err := leafen.API(leafen.APIRequest{Tenancy: r.Tenancy, Method: "POST", Path: "/pages/" + r.PageID + "/content-graph/mutations",
		Body: map[string]any{"mutationId": randomID(), "operations": operations}})
	if err == nil && answer.Status >= 300 {
		err = fmt.Errorf("the people columns didn't change (%d): %s", answer.Status, answer.Body)
	}
	return err
}

func randomID() string {
	bytes := make([]byte, 12)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func today() string { return time.Now().UTC().Format("2006-01-02") }
