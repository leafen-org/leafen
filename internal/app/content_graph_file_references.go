package app

import (
	"bytes"
	"cmp"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
)

const graphFileAuthorityBatchSize = 400

const (
	graphExternalFileSource = "external"
	// maxGraphExternalFileURLBytes bounds a stored web address — a file's,
	// a PDF's, a picture's, a text link's — by what one request may carry
	// and no less: Notion's page Markdown keeps addresses of 2,001, 9,000,
	// 50,000 and 200,000 characters (seen 2026-11-11, file, PDF, image,
	// video, audio, embed and text link alike), while its JSON caps each at
	// 2000 (notionBlockAddressShape and the rich text shape, as Notion's).
	maxGraphExternalFileURLBytes  = notionPublicAPIMaxReadBytes
	maxGraphExternalFileNameRunes = 255
)

// graphExternalFileBlockContent is a file block's linked file. GivenName is
// the name a writer gave through Notion's API, which names the file after
// each address it takes (notionExternalFileName); none where its name came
// from its address.
type graphExternalFileBlockContent struct {
	Source         string          `json:"source"`
	URL            string          `json:"url"`
	Name           string          `json:"name,omitempty"`
	GivenName      string          `json:"givenName,omitempty"`
	Caption        []graphTextSpan `json:"caption,omitempty"`
	CaptionVisible *bool           `json:"captionVisible,omitempty"`
	Color          string          `json:"color,omitempty"`
}

type graphFileRowConsumer struct {
	row        *graphDatabaseRow
	propertyID string
	items      graphFilesValue
}

func graphBlockCarriesManagedFileReference(block *graphBlock) bool {
	if block == nil {
		return false
	}
	if _, capable := graphManagedFileBlockTypes[block.Type]; !capable {
		return false
	}
	fields := map[string]json.RawMessage{}
	if json.Unmarshal(block.Content, &fields) != nil {
		return false
	}
	_, present := fields["fileId"]
	return present
}

func parseGraphExternalFileBlockContent(raw json.RawMessage) (graphExternalFileBlockContent, error) {
	content := graphExternalFileBlockContent{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&content) != nil {
		return content, graphInputError(errorCodeGraphInvalid, "Check the external file block content.")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return content, graphInputError(errorCodeGraphInvalid, "Check the external file block content.")
	}
	// Its name as given, spaces too, as Notion's API keeps one ("   .zip").
	content.Source = strings.TrimSpace(content.Source)
	if content.Source != graphExternalFileSource || !validGraphStoredFileAddress(content.URL) ||
		len([]rune(content.Name)) > maxGraphExternalFileNameRunes ||
		len([]rune(content.GivenName)) > maxGraphExternalFileNameRunes {
		return content, graphInputError(errorCodeGraphInvalid, "Check the external file block content.")
	}
	if !validGraphBlockCaption(content.Caption) || content.Color != "" && !validNotionBlockColor(content.Color) {
		return content, graphInputError(errorCodeGraphInvalid, "Check the external file caption.")
	}
	return content, nil
}

// validGraphStoredFileAddress is whether a file's, a PDF's or a files
// property's external address may be kept: any text, as given, as Notion's
// API keeps one ("a b.com", "mailto:…", "   ", seen 2026-11-10 and
// 2026-11-11); what the web opens of it is its own (safeMediaUrl,
// mediaBlockUrl.ts; fileAttachmentAddress, files.ts), and the editor gives a
// web address (givenWebLink).
func validGraphStoredFileAddress(value string) bool {
	return value != "" && len(value) <= maxGraphExternalFileURLBytes
}

func validGraphExternalFileURL(value string) bool {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	return value != "" && len(value) <= maxGraphExternalFileURLBytes && err == nil && parsed.Host != "" &&
		(parsed.Scheme == "http" || parsed.Scheme == "https")
}

func parseGraphFileBlockContent(raw json.RawMessage) (graphFileBlockContent, error) {
	return parseGraphFileBlockContentOf(raw, false)
}

// parseGraphFileBlockContentOf reads a file block's content; fromFile takes
// one a write names by its fileId alone, its name, size, type and hash then
// the file's (validateAndCanonicalizeGraphFileReferences fills them, and
// replaces any given but the name), as notion-update-page takes an upload by
// its ID (2026-11-24: an agent's leafen_call had to give a row's file its
// size and sha256, where the MCP tool took the upload's ID alone).
func parseGraphFileBlockContentOf(raw json.RawMessage, fromFile bool) (graphFileBlockContent, error) {
	content := graphFileBlockContent{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&content); err != nil {
		return graphFileBlockContent{}, graphInputError(
			errorCodeGraphInvalid, "Check the file block content.",
		)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return graphFileBlockContent{}, graphInputError(
			errorCodeGraphInvalid, "Check the file block content.",
		)
	}
	fileID, err := normalizeGraphID(content.FileID)
	if err != nil {
		return graphFileBlockContent{}, graphInputError(
			errorCodeGraphInvalid, "Check the file block content.",
		)
	}
	content.FileID = fileID
	content.Name = strings.TrimSpace(content.Name)
	content.ContentType = strings.TrimSpace(content.ContentType)
	content.SHA256 = strings.TrimSpace(content.SHA256)
	shaBytes, shaErr := hex.DecodeString(content.SHA256)
	described := fromFile && content.Size == 0 && content.ContentType == "" && content.SHA256 == ""
	if content.Name == "" && !fromFile || content.Size < 0 || !described && (content.ContentType == "" ||
		shaErr != nil || len(shaBytes) != 32 || content.SHA256 != strings.ToLower(content.SHA256)) {
		return graphFileBlockContent{}, graphInputError(
			errorCodeGraphInvalid, "Check the file block content.",
		)
	}
	if !validGraphBlockCaption(content.Caption) || content.Color != "" && !validNotionBlockColor(content.Color) {
		return graphFileBlockContent{}, graphInputError(
			errorCodeGraphInvalid, "Check the file block caption.",
		)
	}
	content.Align = strings.TrimSpace(content.Align)
	content.AltText = strings.TrimSpace(content.AltText)
	content.ImageLink = strings.TrimSpace(content.ImageLink)
	if !validGraphImagePresentation(content) {
		return graphFileBlockContent{}, graphInputError(
			errorCodeGraphInvalid, "Check the image size, alignment, alt text or link.",
		)
	}
	return content, nil
}

// validGraphImagePresentation admits how an uploaded image is shown, as the
// editor sets it for a linked image too: the width its side handles keep, the
// block's in px as Notion keeps it (sizedWidth, frameKeptWidth.ts) or, as kept
// before, a share of the column from the least the editor keeps to the most a
// picture past its column goes (MediaBlockContent.tsx), its alignment, its alt
// text and the web link it opens.
// A PDF's box keeps the height its corners set (BlockCornerHandles.tsx).
const (
	minGraphBlockBoxHeight = 100
	maxGraphBlockBoxHeight = 4000
)

func validGraphImagePresentation(content graphFileBlockContent) bool {
	return (content.Width == 0 || content.Width >= 1 && content.Width <= 1000) &&
		(content.SizedWidth == 0 || content.SizedWidth > 16 && content.SizedWidth <= maxGraphImagePixelSide) &&
		validGraphImagePixelSize(content.PixelWidth, content.PixelHeight) &&
		(content.Height == 0 || content.Height >= minGraphBlockBoxHeight && content.Height <= maxGraphBlockBoxHeight) &&
		(content.Align == "" || content.Align == "left" || content.Align == "center" || content.Align == "right") &&
		utf8.RuneCountInString(content.AltText) <= maxGraphTextCharacters &&
		(content.ImageLink == "" || validGraphExternalFileURL(content.ImageLink))
}

// maxGraphImagePixelSide bounds an image's kept pixel size, past any picture a
// browser draws.
const maxGraphImagePixelSide = 100000

// validGraphImagePixelSize admits the pixel size an image's block keeps so its
// place is held before the file comes (MediaBlockContent.tsx): both sides or
// neither. A file that is no picture drops it.
func validGraphImagePixelSize(width, height int) bool {
	if width == 0 && height == 0 {
		return true
	}
	return width >= 1 && width <= maxGraphImagePixelSide && height >= 1 && height <= maxGraphImagePixelSide
}

func graphImagePresentationSet(content graphFileBlockContent) bool {
	return content.AltText != "" || content.ImageLink != ""
}

// validGraphBlockCaption admits a caption as the same bounded rich text a
// table cell keeps in block content (validGraphRichText): text, an inline
// equation, or a page, database, custom emoji, person, date or link mention.
// Readers check each reference again.
func validGraphBlockCaption(caption []graphTextSpan) bool {
	return validGraphRichText(caption, maxGraphTextCharacters)
}

func parseGraphFileValueItems(raw json.RawMessage) (graphFilesValue, error) {
	return parseGraphFileValueItemsOf(raw, false)
}

// parseGraphFileValueItemsOf reads a files value; fromFile takes an item a
// write names by its fileId alone (parseGraphFileBlockContentOf).
func parseGraphFileValueItemsOf(raw json.RawMessage, fromFile bool) (graphFilesValue, error) {
	encodedItems := []json.RawMessage{}
	if err := json.Unmarshal(raw, &encodedItems); err != nil {
		return nil, graphInputError(errorCodeGraphReferenceInvalid, "A files value is "+graphFilesValueShape+".")
	}
	items := make(graphFilesValue, 0, len(encodedItems))
	for _, encoded := range encodedItems {
		item := graphFileValueItem{}
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&item) != nil {
			return nil, graphInputError(errorCodeGraphReferenceInvalid, "A files value is "+graphFilesValueShape+".")
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			return nil, graphInputError(errorCodeGraphReferenceInvalid, "A files value is "+graphFilesValueShape+".")
		}
		item.Source = strings.TrimSpace(item.Source)
		item.FileID = strings.TrimSpace(item.FileID)
		item.ContentType = strings.TrimSpace(item.ContentType)
		item.SHA256 = strings.TrimSpace(item.SHA256)
		if item.Source == graphExternalFileSource {
			// A linked file keeps its address and name as given, spaces too,
			// as Notion's API keeps them ("a b.com", "   ", seen 2026-11-11);
			// what opens of it is the web's (fileAttachmentAddress, files.ts).
			if item.FileID != "" || item.Name == "" ||
				len([]rune(item.Name)) > maxGraphExternalFileNameRunes ||
				item.Size != 0 || item.ContentType != "" || item.SHA256 != "" ||
				!validGraphStoredFileAddress(item.URL) {
				return nil, graphInputError(errorCodeGraphReferenceInvalid, "Check the external file property value.")
			}
		} else {
			item.URL = strings.TrimSpace(item.URL)
			item.Name = strings.TrimSpace(item.Name)
			if item.Source != "" || item.URL != "" {
				return nil, graphInputError(errorCodeGraphReferenceInvalid, "A files value is "+graphFilesValueShape+".")
			}
			managed, err := parseGraphFileBlockContentFromValue(item, fromFile)
			if err != nil {
				return nil, graphInputError(errorCodeGraphReferenceInvalid, "A files value is "+graphFilesValueShape+".")
			}
			item = graphFileValueItemFromManaged(managed)
		}
		items = append(items, item)
	}
	return items, nil
}

func parseGraphFileBlockContentFromValue(item graphFileValueItem, fromFile bool) (graphFileBlockContent, error) {
	encoded, err := json.Marshal(graphFileBlockContent{
		FileID: item.FileID, Name: item.Name, Size: item.Size,
		ContentType: item.ContentType, SHA256: item.SHA256,
	})
	if err != nil {
		return graphFileBlockContent{}, err
	}
	return parseGraphFileBlockContentOf(encoded, fromFile)
}

func graphFileValueItemFromManaged(content graphFileBlockContent) graphFileValueItem {
	return graphFileValueItem{
		FileID: content.FileID, Name: content.Name, Size: content.Size,
		ContentType: content.ContentType, SHA256: content.SHA256,
	}
}

func canonicalGraphFileContent(record *core.Record) graphFileBlockContent {
	return graphFileBlockContent{
		FileID: record.Id, Name: record.GetString("original_name"),
		Size:        int64(record.GetFloat("size")),
		ContentType: record.GetString("content_type"),
		SHA256:      record.GetString("sha256"),
	}
}

func loadGraphFileAuthorityRecords(
	app core.App,
	workspaceID string,
	fileIDs []string,
) (map[string]graphFileBlockContent, error) {
	result, err := availableGraphFileRecords(app, workspaceID, fileIDs)
	if err != nil {
		return nil, err
	}
	if len(result) != len(fileIDs) {
		return nil, graphInputError(errorCodeGraphReferenceInvalid, "Check the file link: each fileId names a file uploaded whole into this page's workspace.")
	}
	return result, nil
}

// availableGraphFileRecords reads the workspace's files among fileIDs that are
// whole: not being deleted, not still uploading and in the workspace.
func availableGraphFileRecords(
	app core.App,
	workspaceID string,
	fileIDs []string,
) (map[string]graphFileBlockContent, error) {
	result := make(map[string]graphFileBlockContent, len(fileIDs))
	pendingDeletes, err := pendingProductFileDeletionIDs(app, fileIDs)
	if err != nil {
		return nil, err
	}
	incompleteUploads, err := incompleteProductFileUploadIDs(app, fileIDs)
	if err != nil {
		return nil, err
	}
	for start := 0; start < len(fileIDs); start += graphFileAuthorityBatchSize {
		end := min(start+graphFileAuthorityBatchSize, len(fileIDs))
		records, err := app.FindRecordsByIds(filesCollection, fileIDs[start:end])
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			_, deleting := pendingDeletes[record.Id]
			_, uploading := incompleteUploads[record.Id]
			if !deleting && !uploading && workspaceFileRecordBelongsTo(record, workspaceID) {
				result[record.Id] = canonicalGraphFileContent(record)
			}
		}
	}
	return result, nil
}

func validateAndCanonicalizeGraphFileReferences(
	app core.App,
	workspaceID string,
	operations []graphOperation,
) error {
	// A database's new icon and cover show pictures (look_picture_files.go).
	if err := checkGraphDataSourceLookFiles(app, workspaceID, operations); err != nil {
		return err
	}
	blocksByFileID := map[string][]*graphBlock{}
	calloutIcons := map[string]struct{}{}
	rowConsumers := []graphFileRowConsumer{}
	allFileIDs := map[string]struct{}{}
	for index := range operations {
		operation := &operations[index]
		// A callout's uploaded icon is a workspace picture it keeps (block_kept_file.go).
		if operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil {
			if fileID := graphCalloutIconFileID(operation.Block.Type, operation.Block.Content); fileID != "" {
				calloutIcons[fileID] = struct{}{}
				allFileIDs[fileID] = struct{}{}
			}
		}
		if operation.Kind == graphOperationKindUpsertBlock &&
			graphBlockCarriesManagedFileReference(operation.Block) {
			content, err := parseGraphFileBlockContentOf(operation.Block.Content, true)
			if err != nil {
				return err
			}
			blocksByFileID[content.FileID] = append(blocksByFileID[content.FileID], operation.Block)
			allFileIDs[content.FileID] = struct{}{}
		}
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		for propertyID, value := range operation.Row.Values {
			if value.Type != graphValueTypeFiles || graphRowValueIsCleared(value.Value) {
				continue
			}
			items, err := parseGraphFileValueItemsOf(value.Value, true)
			if err != nil {
				return err
			}
			rowConsumers = append(rowConsumers, graphFileRowConsumer{
				row: operation.Row, propertyID: propertyID, items: items,
			})
			for _, item := range items {
				if item.FileID != "" {
					allFileIDs[item.FileID] = struct{}{}
				}
			}
		}
	}
	fileIDs := make([]string, 0, len(allFileIDs))
	for fileID := range allFileIDs {
		fileIDs = append(fileIDs, fileID)
	}
	sort.Strings(fileIDs)
	recordsByID := map[string]graphFileBlockContent{}
	if len(fileIDs) != 0 {
		var err error
		recordsByID, err = graphFilesWhole(app, workspaceID, fileIDs)
		if err != nil {
			return err
		}
	}
	for fileID := range calloutIcons {
		if !notionManagedFileContentCompatible(graphBlockTypeImage, recordsByID[fileID]) {
			return graphCalloutIconKindRefused(recordsByID[fileID])
		}
	}
	for _, fileID := range fileIDs {
		for _, block := range blocksByFileID[fileID] {
			requested, err := parseGraphFileBlockContentOf(block.Content, true)
			if err != nil {
				return err
			}
			canonicalContent := recordsByID[fileID]
			canonicalContent.Name = cmp.Or(requested.Name, canonicalContent.Name)
			canonicalContent.Caption = requested.Caption
			canonicalContent.CaptionVisible = requested.CaptionVisible
			canonicalContent.Color = requested.Color
			kept := graphFileBlockSizesOf(block.Type, canonicalContent.ContentType)
			canonicalContent.NaturalSize = requested.NaturalSize && kept.Natural
			canonicalContent.SizeHidden = requested.SizeHidden && block.Type == graphBlockTypeFile
			canonicalContent.ColumnWide = requested.ColumnWide && kept.Column
			if !kept.Width && (requested.Width != 0 || requested.SizedWidth != 0 || requested.Align != "") {
				return graphInputError(errorCodeGraphInvalid, "Only visual media keeps a size or alignment.")
			}
			if requested.Height != 0 && !kept.Height {
				return graphInputError(errorCodeGraphInvalid, "Only a PDF or an embed keeps a height.")
			}
			canonicalContent.Width, canonicalContent.Height, canonicalContent.Align = requested.Width, requested.Height, requested.Align
			canonicalContent.SizedWidth = requested.SizedWidth
			// A file can be explicitly turned back into its media kind. Keep
			// bounded presentation with the same image bytes across that turn.
			if strings.HasPrefix(strings.ToLower(canonicalContent.ContentType), "image/") {
				canonicalContent.AltText, canonicalContent.ImageLink = requested.AltText, requested.ImageLink
				canonicalContent.PixelWidth, canonicalContent.PixelHeight = requested.PixelWidth, requested.PixelHeight
			} else if graphImagePresentationSet(requested) {
				return graphInputError(
					errorCodeGraphInvalid, "Only image bytes keep alt text or an image link.",
				)
			}
			if !notionManagedFileContentCompatible(block.Type, canonicalContent) {
				return graphFileKindRefused(block.Type, canonicalContent)
			}
			canonical, err := json.Marshal(canonicalContent)
			if err != nil {
				return err
			}
			block.Content = canonical
		}
	}
	for _, consumer := range rowConsumers {
		seen := map[string]struct{}{}
		canonical := make(graphFilesValue, 0, len(consumer.items))
		for _, item := range consumer.items {
			if item.Source == graphExternalFileSource {
				canonical = append(canonical, item)
				continue
			}
			if _, duplicate := seen[item.FileID]; duplicate {
				continue
			}
			seen[item.FileID] = struct{}{}
			// A row's file keeps the name it is given, as a file block does
			// (a skill's bundled file its path in the skill folder).
			stored := graphFileValueItemFromManaged(recordsByID[item.FileID])
			if item.Name != "" {
				stored.Name = item.Name
			}
			canonical = append(canonical, stored)
		}
		encoded, err := json.Marshal(canonical)
		if err != nil {
			return err
		}
		value := consumer.row.Values[consumer.propertyID]
		value.Value = encoded
		consumer.row.Values[consumer.propertyID] = value
	}
	return nil
}
