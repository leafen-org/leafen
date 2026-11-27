package app

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

type graphExternalPDFBlockContent struct {
	URL            string          `json:"url,omitempty"`
	Caption        []graphTextSpan `json:"caption,omitempty"`
	CaptionVisible *bool           `json:"captionVisible,omitempty"`
	Width          float64         `json:"width,omitempty"`
	SizedWidth     float64         `json:"sizedWidth,omitempty"`
	Height         float64         `json:"height,omitempty"`
	Align          string          `json:"align,omitempty"`
}

func parseGraphExternalPDFBlockContent(raw json.RawMessage) (graphExternalPDFBlockContent, error) {
	content := graphExternalPDFBlockContent{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&content) != nil {
		return content, graphInputError(errorCodeGraphInvalid, "Check the PDF block content.")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return content, graphInputError(errorCodeGraphInvalid, "Check the PDF block content.")
	}
	if strings.TrimSpace(content.URL) == "" && len(content.Caption) == 0 {
		content.URL = ""
		return content, nil
	}
	if !validGraphStoredFileAddress(content.URL) || !validGraphBlockCaption(content.Caption) ||
		!validGraphImagePresentation(graphFileBlockContent{Width: content.Width, SizedWidth: content.SizedWidth, Height: content.Height, Align: content.Align}) {
		return content, graphInputError(errorCodeGraphInvalid, "Check the PDF block content.")
	}
	return content, nil
}
