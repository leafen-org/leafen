// The built-in Web Clips app (docs/app-platform.md 16): a web clipper that
// sends its clips to a webhook — MarkSnip and the like, or a phone's
// shortcut — sends them to the app's endpoint, signed in with the person's
// app password, and each clip becomes a page in the person's own "웹 클립"
// database: its title, address and summary as the row's values, the page's
// markdown as its text. A clip of an address already kept is a new page;
// nothing is overwritten.
package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"leafen.org/leafen/sdk/app/go/leafen"
)

const (
	columnTitle    = "제목"
	columnURL      = "주소"
	columnExcerpt  = "요약"
	columnByline   = "글쓴이"
	columnKeywords = "키워드"
	columnNote     = "메모"
	columnSource   = "보낸 곳"
	columnReceived = "받은 때"
	// A piece of markdown Leafen takes in one write.
	markdownPiece = 192 << 10
	findMax       = 50
)

var errNoClips = errors.New("no clips table")

// clips is the person's own "웹 클립" database and its workspace.
type clips struct {
	*leafen.Table
	workspaceID string
}

// openClips finds the person's database, which Leafen makes as the app is
// first reached for them: its screen, a clip or an agent's tool.
func openClips(tenancy string) (clips, error) {
	answer, err := leafen.API(leafen.APIRequest{Tenancy: tenancy, Method: "GET", Path: "/app-data"})
	if err != nil {
		return clips{}, err
	}
	data := struct {
		WorkspaceID string `json:"workspaceId"`
		Items       []struct {
			Key          string          `json:"key"`
			PageID       string          `json:"pageId"`
			DataSourceID string          `json:"dataSourceId"`
			Place        string          `json:"place"`
			Properties   []leafen.Column `json:"properties"`
		} `json:"items"`
	}{}
	if answer.Status != 200 || json.Unmarshal(answer.Body, &data) != nil {
		return clips{}, fmt.Errorf("the app's data couldn't be read (%d)", answer.Status)
	}
	for _, item := range data.Items {
		if item.Key == "clips" && item.Place == "person" && item.DataSourceID != "" && len(item.Properties) > 0 {
			return clips{&leafen.Table{Tenancy: tenancy, Key: item.Key, PageID: item.PageID, DataSourceID: item.DataSourceID,
				Columns: item.Properties}, data.WorkspaceID}, nil
		}
	}
	return clips{}, errNoClips
}

var jsonHeaders = map[string]string{"Content-Type": "application/json; charset=utf-8", "Cache-Control": "no-store"}

func reply(status int, value any) leafen.EndpointAnswer {
	encoded, _ := json.Marshal(value)
	return leafen.EndpointAnswer{Status: status, Headers: jsonHeaders, Body: string(encoded)}
}

func refuse(status int, message string) leafen.EndpointAnswer {
	return reply(status, map[string]string{"error": message})
}

// clip takes one clip from a clipper.
//
//go:wasmexport clip
func clip() int32 {
	request := leafen.EndpointRequest{}
	if err := leafen.InputJSON(&request); err != nil {
		return leafen.Fail("the request isn't JSON")
	}
	answer, err := receive(request)
	if err != nil {
		leafen.Log("clip not kept: " + err.Error())
		answer = refuse(500, "The clip couldn't be kept just now; send it again.")
	}
	_ = leafen.OutputJSON(answer)
	return 0
}

func receive(request leafen.EndpointRequest) (leafen.EndpointAnswer, error) {
	switch {
	case request.Method == "OPTIONS":
		return leafen.EndpointAnswer{Status: 204, Headers: map[string]string{"Allow": "POST, OPTIONS"}}, nil
	case request.Method != "POST":
		return refuse(405, "Send clips with POST."), nil
	case request.BodyFile != nil:
		return refuse(413, "A clip is up to 1 MB."), nil
	}
	body, err := base64.StdEncoding.DecodeString(request.Body)
	if err != nil {
		return refuse(400, "The clip couldn't be read."), nil
	}
	c, refusal := readClip(request.Headers["content-type"], request.Query, body)
	if refusal != "" {
		return refuse(400, refusal), nil
	}
	table, err := openClips(request.Tenancy)
	if errors.Is(err, errNoClips) {
		return refuse(409, "Your Web Clips database isn't ready yet. Open Web Clips in Leafen once, then send the clip again."), nil
	}
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	values := map[string]any{columnTitle: c.Title, columnExcerpt: c.Excerpt, columnByline: c.Byline, columnKeywords: c.Keywords,
		columnNote: c.Note, columnSource: c.Source}
	if c.URL != "" {
		values[columnURL] = c.URL
	}
	rowID, err := table.Add(values)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	for index, piece := range pieces(c.Content, markdownPiece) {
		answer, err := leafen.API(leafen.APIRequest{Tenancy: request.Tenancy, Method: "POST",
			Path: "/workspaces/" + table.workspaceID + "/pages/" + rowID + "/imports/markdown",
			Body: map[string]any{"requestId": fmt.Sprintf("clip-%s-%d", rowID, index), "markdown": piece}})
		if err == nil && answer.Status >= 300 {
			err = fmt.Errorf("the text didn't go in (%d): %s", answer.Status, answer.Body)
		}
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
	}
	page := "/p/" + rowID
	if origin, _, found := strings.Cut(request.Address, "/app-endpoints/"); found {
		page = origin + page
	}
	return reply(201, map[string]string{"id": rowID, "title": c.Title, "page": page}), nil
}

// find looks through the person's clips for an agent or the app's screen:
// the newest first, by words in their title, address or summary.
//
//go:wasmexport find
func find() int32 {
	input := struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}{}
	call, err := leafen.Action(&input)
	if err != nil {
		return leafen.Fail("the input isn't JSON")
	}
	if input.Limit < 1 || input.Limit > findMax {
		input.Limit = 20
	}
	table, err := openClips(call.Tenancy)
	if errors.Is(err, errNoClips) {
		return leafen.Fail("the person's Web Clips database isn't ready yet; they can open Web Clips in Leafen once")
	}
	if err != nil {
		return leafen.Fail(err.Error())
	}
	found := []map[string]string{}
	query := leafen.Query{Search: strings.TrimSpace(input.Query), Sort: []leafen.Sort{{Column: columnReceived, Descending: true}}}
	for row, err := range table.Rows(query) {
		if err != nil {
			return leafen.Fail(err.Error())
		}
		found = append(found, map[string]string{"pageId": row.ID, "title": row.Text(columnTitle), "url": row.Text(columnURL),
			"excerpt": row.Text(columnExcerpt), "received": row.CreatedAt})
		if len(found) == input.Limit {
			break
		}
	}
	_ = leafen.OutputJSON(map[string]any{"clips": found})
	return 0
}

func main() {}
