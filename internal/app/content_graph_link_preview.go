package app

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// A link preview keeps the URL a Notion link preview was made from, so the
// block stays a link preview through import, duplication, trash and archives
// instead of turning into a bookmark. Its preview is not fetched from the
// provider; the URL is its whole content.

type graphLinkPreviewBlockContent struct {
	URL string `json:"url"`
}

func parseGraphLinkPreviewBlockContent(raw json.RawMessage) (graphLinkPreviewBlockContent, error) {
	content := graphLinkPreviewBlockContent{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&content) != nil {
		return content, graphInputError(errorCodeGraphInvalid, "Check the link preview block content.")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return content, graphInputError(errorCodeGraphInvalid, "Check the link preview block content.")
	}
	content.URL = strings.TrimSpace(content.URL)
	if !validGraphExternalFileURL(content.URL) {
		return content, graphInputError(errorCodeGraphInvalid, "Check the link preview block content.")
	}
	return content, nil
}
