package app

import "encoding/json"

// The size of a framed block the block API writes has one shape,
// notionBlockSize: what sizeNewNotionAPIBlock gives a new block, what a
// block whose file the API changes keeps of its own (notionKeptBlockSize),
// and what a file_upload's or a copied Notion-hosted file's request reads
// back (notionBlockFileUploadContent), which puts it on the file's content
// where the block keeps it (graphFileBlockSizes). A key the sizing gives is
// one the request reads: before 2026-11-27 a PDF's 500px pages, its height,
// were a key the request didn't know, and every PDF uploaded through the
// block API was refused.

// notionBlockSize is a framed block's size, named as a file block's content
// names it (graphFileBlockContent).
type notionBlockSize struct {
	Width       float64 `json:"width,omitempty"`
	SizedWidth  float64 `json:"sizedWidth,omitempty"`
	Height      float64 `json:"height,omitempty"`
	Align       string  `json:"align,omitempty"`
	NaturalSize bool    `json:"naturalSize,omitempty"`
	ColumnWide  bool    `json:"columnWide,omitempty"`
}

// notionAPIPDFHeight is how high Notion draws the pages of a PDF its block
// API made, uploaded or linked, as it has no height of its own (seen
// 2026-11-15 at 1440, twice: a 720×516 block, its pages 704×500), where one
// made in its editor stands 320 high (PdfPages' own).
const notionAPIPDFHeight = 500

// notionBlockSizeOf reads a block content's size.
func notionBlockSizeOf(content map[string]any) notionBlockSize {
	size := notionBlockSize{}
	encoded, err := json.Marshal(content)
	if err != nil || json.Unmarshal(encoded, &size) != nil {
		return notionBlockSize{}
	}
	return size
}

// into writes the size's keys into a block's content.
func (size notionBlockSize) into(content map[string]any) {
	encoded, err := json.Marshal(size)
	if err != nil {
		return
	}
	keys := map[string]any{}
	if json.Unmarshal(encoded, &keys) != nil {
		return
	}
	for key, value := range keys {
		content[key] = value
	}
}

// onto puts the size on a block's file content where the block keeps it,
// by its type and the file it holds.
func (size notionBlockSize) onto(blockType string, content *graphFileBlockContent) {
	kept := graphFileBlockSizesOf(blockType, content.ContentType)
	content.NaturalSize = size.NaturalSize && kept.Natural
	content.ColumnWide = size.ColumnWide && kept.Column
	content.Height = 0
	if kept.Height {
		content.Height = size.Height
	}
	content.Width, content.SizedWidth, content.Align = 0, 0, ""
	if kept.Width {
		content.Width, content.SizedWidth, content.Align = size.Width, size.SizedWidth, size.Align
	}
}

// uploadedThroughAPI is the size of an upload the block API makes: a video
// or PDF spans its column ("columnWide", the editor's MediaBlock and
// PdfBlock), as Notion draws one its API made, which it gives no box (seen
// 2026-11-15 at 1440: its block "width: 100%", the video and the PDF 704
// wide, no "맞춤" in the video's menu), where one uploaded in its editor
// keeps a 708px box (the video 692). A picture so made is drawn at its own
// size at the column's start ("naturalSize", the editor's ImageBlock), as a
// linked one, with no "맞춤" in its menu until sized (seen 2026-11-27 at 1440,
// default and full width, twice: 240, 480 and 1600 wide pictures drawn 240,
// 480 and the column at its start), where one uploaded in the editor is drawn
// 32px narrower in the middle. The import reads uploads through Notion's API
// too but cannot tell how one was made there, so its videos and PDFs keep the
// editor's box.
func (size notionBlockSize) uploadedThroughAPI(blockType string) notionBlockSize {
	switch blockType {
	case graphBlockTypeImage:
		size.NaturalSize = true
	case graphBlockTypeVideo, graphBlockTypePDF:
		size.ColumnWide = true
	}
	return size
}

// notionContentNamesUpload says whether a block's content names a file the
// request brings: a file_upload or a copied Notion-hosted file.
func notionContentNamesUpload(content map[string]any) bool {
	_, upload := content["fileUploadId"]
	_, hosted := content["hostedFile"]
	return upload || hosted
}

// newNotionAPIBlockSize is the size a framed block new through the API
// takes, through its block API or its page Markdown alike (seen 2026-11-27
// at 1440, dpr 1: a picture, video and PDF linked through Notion's page
// Markdown were drawn as the block API's — the picture at its own size at
// the column's start, the video 704 across the column, the PDF's pages 500
// high): one whose file the write brings the size an upload the API makes
// takes, and a PDF its 500px pages.
func newNotionAPIBlockSize(blockType string, size notionBlockSize, brought bool) notionBlockSize {
	if brought {
		size = size.uploadedThroughAPI(blockType)
	}
	if blockType == graphBlockTypePDF && size.Height == 0 {
		size.Height = notionAPIPDFHeight
	}
	return size
}

// sizeNewNotionAPIBlock gives a framed block new through the block API the
// size Notion gives one made so: an embed its box, an upload no box, a PDF
// its 500px pages.
func sizeNewNotionAPIBlock(blockType string, content map[string]any) {
	if content == nil {
		return
	}
	sizeNewNotionAPIEmbed(blockType, content)
	newNotionAPIBlockSize(blockType, notionBlockSizeOf(content), notionContentNamesUpload(content)).into(content)
}

// sizeNewNotionMarkdownBlock gives a picture, video or PDF new through page
// Markdown — the public API's "v1" and "v2", a new page's first body, the
// MCP's — the size a block new through the API takes (newNotionAPIBlockSize),
// brought saying its line named a stored file (file-upload://, or a file on
// the page). One waiting for its file or address takes none; an embed keeps
// spanning its column, as Notion draws one its Markdown made.
func sizeNewNotionMarkdownBlock(blockType string, content json.RawMessage, brought bool) (json.RawMessage, error) {
	if blockType != graphBlockTypeImage && blockType != graphBlockTypeVideo && blockType != graphBlockTypePDF {
		return content, nil
	}
	fields := map[string]any{}
	if err := json.Unmarshal(content, &fields); err != nil {
		return nil, err
	}
	if !brought && stringNotionBlockValue(fields["url"]) == "" {
		return content, nil
	}
	newNotionAPIBlockSize(blockType, notionBlockSizeOf(fields), brought).into(fields)
	return json.Marshal(fields)
}

// notionKeptBlockSize is the size a block whose file the block API changes
// to an upload keeps, as Notion's keeps how it is drawn (seen 2026-11-27 at
// 1440, dpr 1: a picture, video and PDF uploaded through its API, and ones
// linked through it, given another upload each stayed at the picture's own
// size at the column's start, the video across the column, 704, and the
// PDF's pages 500 high): its own, and, from a link or no file, the size an
// upload the block API makes takes, as a link is drawn.
func notionKeptBlockSize(blockType string, current map[string]any) notionBlockSize {
	size := notionBlockSizeOf(current)
	if stringNotionBlockValue(current["fileId"]) == "" {
		size = size.uploadedThroughAPI(blockType)
	}
	return size
}
