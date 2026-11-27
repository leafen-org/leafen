package app

import (
	"encoding/json"
	"errors"
	"strings"
)

func normalizeNotionExternalMediaMutation(
	blockType string,
	incoming map[string]any,
	current map[string]any,
	update bool,
) (string, map[string]any, *[]graphTextSpan, error) {
	if err := allowOnlyNotionBlockKeys(incoming, "type", "external", "file_upload", "file", "caption"); err != nil {
		return "", nil, nil, err
	}
	declared, err := notionBlockFileSourceType(incoming)
	if err != nil {
		return "", nil, nil, errors.New("media source is invalid")
	}
	_, externalSpecified := incoming["external"]
	if request, managed, err := notionManagedFileRequest(declared, incoming); err != nil {
		return "", nil, nil, err
	} else if managed {
		caption, err := notionBlockMutationCaption(incoming, current)
		if err != nil {
			return "", nil, nil, err
		}
		request["caption"] = caption
		if update {
			notionKeptBlockSize(blockType, current).into(request)
		}
		return blockType, request, nil, nil
	}
	managed := stringNotionBlockValue(current["fileId"]) != ""
	if managed && !externalSpecified && declared == "" {
		caption, err := notionBlockMutationCaption(incoming, current)
		if err != nil {
			return "", nil, nil, err
		}
		current["caption"] = caption
		return blockType, current, nil, nil
	}
	if declared == graphExternalFileSource && !externalSpecified && stringNotionBlockValue(current["url"]) == "" {
		return "", nil, nil, errors.New("external media content is required")
	}
	if rawExternal, specified := incoming["external"]; specified {
		external, ok := rawExternal.(map[string]any)
		if !ok || len(external) != 1 {
			return "", nil, nil, errors.New("external media content is invalid")
		}
		url, ok := external["url"].(string)
		if !ok || url == "" || notionHostedFileURL(url) {
			return "", nil, nil, errors.New("external media url is invalid")
		}
		// A new one's address was checked with the request's others
		// (notionMediaLinkRequestFault); a changed one's names its kind.
		if update && !notionMediaUpdateTaken(blockType, url) {
			return "", nil, nil, notionMediaUpdateRefused(blockType, url)
		}
		clearNotionManagedFileContent(current)
		delete(current, "name")
		current["url"] = url
	}
	if stringNotionBlockValue(current["url"]) == "" {
		if !update || externalSpecified || declared != "" {
			return "", nil, nil, errors.New("external media url is required")
		}
		// An empty one, as page Markdown makes (notionMarkdownFileLine),
		// takes its caption alone and stays empty, as Notion's (seen 2026-11-11).
	}
	caption, err := notionBlockMutationCaption(incoming, current)
	if err != nil {
		return "", nil, nil, err
	}
	current["caption"] = caption
	return blockType, current, nil, nil
}

func normalizeNotionExternalFileMutation(
	incoming map[string]any,
	current map[string]any,
	existing *storedNotionBlock,
) (string, map[string]any, *[]graphTextSpan, error) {
	if err := allowOnlyNotionBlockKeys(incoming, "type", "external", "file_upload", "file", "caption", "name"); err != nil {
		return "", nil, nil, err
	}
	declared, err := notionBlockFileSourceType(incoming)
	if err != nil {
		return "", nil, nil, errors.New("file source is invalid")
	}
	_, externalSpecified := incoming["external"]
	if request, managed, err := notionManagedFileRequest(declared, incoming); err != nil {
		return "", nil, nil, err
	} else if managed {
		caption, err := notionBlockMutationCaption(incoming, current)
		if err != nil {
			return "", nil, nil, err
		}
		name, err := notionBlockMutationFileName(incoming, map[string]any{})
		if err != nil {
			return "", nil, nil, err
		}
		request["name"], request["caption"] = name, caption
		if existing != nil {
			notionKeptBlockSize(graphBlockTypeFile, current).into(request)
		}
		return graphBlockTypeFile, request, nil, nil
	}
	managed := stringNotionBlockValue(current["fileId"]) != ""
	if managed && !externalSpecified && declared == "" {
		caption, err := notionBlockMutationCaption(incoming, current)
		if err != nil {
			return "", nil, nil, err
		}
		name, err := notionBlockMutationFileName(incoming, current)
		if err != nil {
			return "", nil, nil, err
		}
		current["caption"], current["name"] = caption, name
		return graphBlockTypeFile, current, nil, nil
	}
	lastURL := stringNotionBlockValue(current["url"])
	if managed || stringNotionBlockValue(current["source"]) != graphExternalFileSource {
		lastURL = ""
	}
	if declared == graphExternalFileSource && !externalSpecified && lastURL == "" {
		return "", nil, nil, errors.New("external file content is required")
	}
	if rawExternal, specified := incoming["external"]; specified {
		external, ok := rawExternal.(map[string]any)
		if !ok || len(external) != 1 {
			return "", nil, nil, errors.New("external file content is invalid")
		}
		// Any text, a new one's checked with the request's others
		// (notionMediaLinkRequestFault).
		url, ok := external["url"].(string)
		if !ok || url == "" || len(url) > maxGraphExternalFileURLBytes || notionHostedFileURL(url) {
			return "", nil, nil, errors.New("external file url is invalid")
		}
		clearNotionManagedFileContent(current)
		current["url"] = url
	}
	url := stringNotionBlockValue(current["url"])
	if url == "" && existing != nil && !externalSpecified && declared == "" {
		return notionEmptyFileMutation(incoming, current)
	}
	if url == "" {
		return "", nil, nil, errors.New("external file url is required")
	}
	// Its name, as Notion's API gives it (notionExternalFileName), and the
	// name given, kept to name it after each address it takes.
	name, givenName := stringNotionBlockValue(current["name"]), stringNotionBlockValue(current["givenName"])
	if lastURL == "" {
		givenName = ""
	}
	if raw, specified := incoming["name"]; specified {
		given, ok := raw.(string)
		if !ok || len([]rune(given)) > maxGraphExternalFileNameRunes {
			return "", nil, nil, errors.New("file name is invalid")
		}
		name, givenName = notionExternalFileName(&given, url), given
	} else if lastURL == "" || name == "" {
		// One kept without a name has the one its address gives.
		name, givenName = notionExternalFileName(nil, url), ""
	} else if url != lastURL {
		renamed := notionExternalFileRenamed(name, givenName, lastURL, url)
		if givenName != "" && name != notionExternalFileName(&givenName, lastURL) {
			// Named since by other hands.
			givenName = ""
		}
		name = renamed
	}
	if givenName == "" {
		delete(current, "givenName")
	} else {
		current["givenName"] = givenName
	}
	if runes := []rune(name); len(runes) > maxGraphExternalFileNameRunes {
		name = string(runes[:maxGraphExternalFileNameRunes])
	}
	caption, err := notionBlockMutationCaption(incoming, current)
	if err != nil {
		return "", nil, nil, err
	}
	current["name"], current["caption"] = name, caption
	current["source"] = graphExternalFileSource
	encoded, err := json.Marshal(current)
	if err != nil {
		return "", nil, nil, err
	}
	canonical, err := parseGraphExternalFileBlockContent(encoded)
	if err != nil {
		return "", nil, nil, errors.New("external file content is invalid")
	}
	encoded, _ = json.Marshal(canonical)
	if json.Unmarshal(encoded, &current) != nil {
		return "", nil, nil, errors.New("external file content is invalid")
	}
	return graphBlockTypeFile, current, nil, nil
}

// notionEmptyFileMutation changes a file block of no file or address, as
// page Markdown makes one (notionMarkdownFileLine): it takes its caption and
// its name, as given, alone and stays empty, as Notion's (seen 2026-11-11).
func notionEmptyFileMutation(incoming, current map[string]any) (string, map[string]any, *[]graphTextSpan, error) {
	if raw, specified := incoming["name"]; specified {
		given, ok := raw.(string)
		if !ok || len([]rune(given)) > maxGraphExternalFileNameRunes {
			return "", nil, nil, errors.New("file name is invalid")
		}
		current["name"] = given
	}
	caption, err := notionBlockMutationCaption(incoming, current)
	if err != nil {
		return "", nil, nil, err
	}
	current["caption"] = caption
	return graphBlockTypeFile, current, nil, nil
}

// notionBlockFileSourceType reads a file block's declared source: external,
// file_upload or a copied Notion-hosted file. One source may be given.
func notionBlockFileSourceType(incoming map[string]any) (string, error) {
	declared := ""
	if raw, specified := incoming["type"]; specified {
		var ok bool
		if declared, ok = raw.(string); !ok {
			return "", errors.New("file source is invalid")
		}
	}
	sources := 0
	for _, key := range []string{graphExternalFileSource, "file_upload", "file"} {
		if _, specified := incoming[key]; specified {
			sources++
			if declared != "" && declared != key {
				return "", errors.New("file source is invalid")
			}
		}
	}
	if sources > 1 || declared != "" && declared != graphExternalFileSource &&
		declared != "file_upload" && declared != "file" {
		return "", errors.New("file source is invalid")
	}
	return declared, nil
}

// notionManagedFileRequest reads a file_upload or a copied Notion-hosted file
// object into the request the mutation resolves to a managed file. It reports
// false when the block names neither.
func notionManagedFileRequest(declared string, incoming map[string]any) (map[string]any, bool, error) {
	if rawUpload, specified := incoming["file_upload"]; specified || declared == "file_upload" {
		if !specified {
			return nil, false, errors.New("file_upload content is invalid")
		}
		fileUploadID, err := notionBlockFileUploadRequestID(rawUpload)
		if err != nil {
			return nil, false, err
		}
		return map[string]any{"fileUploadId": fileUploadID}, true, nil
	}
	if rawHosted, specified := incoming["file"]; specified || declared == "file" {
		if !specified {
			return nil, false, errors.New("file content is invalid")
		}
		request, err := parseNotionHostedFileRequest(rawHosted)
		if err != nil {
			return nil, false, err
		}
		return map[string]any{"hostedFile": request}, true, nil
	}
	return nil, false, nil
}

func notionBlockFileUploadRequestID(raw any) (string, error) {
	value, ok := raw.(map[string]any)
	if !ok || len(value) != 1 {
		return "", errors.New("file_upload content is invalid")
	}
	publicID, _ := value["id"].(string)
	publicID, ok = normalizeNotionUUIDRequest(publicID)
	if !ok {
		return "", errors.New("file_upload id is invalid")
	}
	return publicID, nil
}

func notionBlockMutationCaption(
	incoming map[string]any,
	current map[string]any,
) ([]map[string]any, error) {
	if raw, specified := incoming["caption"]; specified {
		return notionNativeCaptionRequest(raw)
	}
	raw, present := current["caption"]
	if !present {
		return []map[string]any{}, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, errors.New("block caption is invalid")
	}
	decoded := []map[string]any{}
	if json.Unmarshal(encoded, &decoded) != nil {
		return nil, errors.New("block caption is invalid")
	}
	return decoded, nil
}

func notionBlockMutationFileName(
	incoming map[string]any,
	current map[string]any,
) (string, error) {
	value := current["name"]
	if raw, specified := incoming["name"]; specified {
		value = raw
	}
	if value == nil {
		return "", nil
	}
	name, ok := value.(string)
	name = strings.TrimSpace(name)
	if !ok || len([]rune(name)) > maxGraphExternalFileNameRunes {
		return "", errors.New("file name is invalid")
	}
	return name, nil
}

// clearNotionManagedFileContent takes a block's file away for a link: its
// file's keys, and the size it took as an upload (naturalSize, columnWide),
// a link being drawn so; the size it was given stays.
func clearNotionManagedFileContent(content map[string]any) {
	for _, key := range []string{"fileId", "size", "contentType", "sha256", "fileUploadId", "naturalSize", "columnWide"} {
		delete(content, key)
	}
}

// normalizeNotionEmbedMutation takes an embed url or a completed file_upload,
// never both. An uploaded embed keeps its file until a url replaces it; a
// changed url "" leaves it as it is, as Notion's.
func normalizeNotionEmbedMutation(
	incoming map[string]any,
	current map[string]any,
	update bool,
) (string, map[string]any, *[]graphTextSpan, error) {
	if err := allowOnlyNotionBlockKeys(incoming, "url", "file_upload", "caption"); err != nil {
		return "", nil, nil, err
	}
	if rawURL, isText := incoming["url"].(string); update && isText && rawURL == "" {
		delete(incoming, "url")
	}
	// An embed keeps its caption across a new address or file, as Notion's
	// (2026-11-05: the API refused a caption and dropped one on a new url).
	caption, err := notionBlockMutationCaption(incoming, current)
	if err != nil {
		return "", nil, nil, err
	}
	_, urlSpecified := incoming["url"]
	rawUpload, uploadSpecified := incoming["file_upload"]
	if urlSpecified && uploadSpecified {
		return "", nil, nil, errors.New("embed takes a url or a file_upload, not both")
	}
	if uploadSpecified {
		fileUploadID, err := notionBlockFileUploadRequestID(rawUpload)
		if err != nil {
			return "", nil, nil, err
		}
		return graphBlockTypeEmbed, map[string]any{"fileUploadId": fileUploadID, "caption": caption}, nil, nil
	}
	if !urlSpecified && stringNotionBlockValue(current["fileId"]) != "" {
		current["caption"] = caption
		return graphBlockTypeEmbed, current, nil, nil
	}
	if rawURL, _ := incoming["url"].(string); notionHostedFileURL(rawURL) {
		// An uploaded embed reads as its hosted file URL; sending it back
		// keeps that file.
		return graphBlockTypeEmbed, map[string]any{
			"hostedFile": notionHostedFileRequest{URL: strings.TrimSpace(rawURL)}, "caption": caption,
		}, nil, nil
	}
	if urlSpecified {
		clearNotionManagedFileContent(current)
		delete(current, "name")
	}
	current["caption"] = caption
	return normalizeNotionURLBlockMutation(graphBlockTypeEmbed, incoming, current, update)
}

// notionAPIEmbedBlockWidth is how wide Notion draws an embed its block API
// made, which gives it no size (seen 2026-11-11): a 400px box, its frame 384
// and centred, no wider than its column, a full-width page's too. One made
// from page Markdown or in the editor spans the column.
const notionAPIEmbedBlockWidth = 400

// sizeNewNotionAPIEmbed gives a link embed new through the block API Notion's
// width for it ("blockWidth", px, the editor's EmbedBlock). The import reads
// embeds through Notion's API too but cannot tell how one was made there, so
// its embeds keep spanning the column.
func sizeNewNotionAPIEmbed(blockType string, content map[string]any) {
	if blockType != graphBlockTypeEmbed || content == nil {
		return
	}
	for _, uploaded := range []string{"fileUploadId", "fileId", "hostedFile"} {
		if _, ok := content[uploaded]; ok {
			return
		}
	}
	content["blockWidth"] = notionAPIEmbedBlockWidth
}

// notionBookmarkPreviewKeys are what a bookmark's card shows of its page
// besides its address, and the site's name it keeps for its mention, read
// by the editor (bookmarkMetadata.ts).
var notionBookmarkPreviewKeys = []string{"title", "description", "iconUrl", "imageUrl", "provider"}

// plainNotionAPIBookmark gives a bookmark whose address the block API writes
// Notion's plain card: its host over its address, no page read for it (seen
// 2026-11-11: made through the API, or its address written again — the same
// one too — the card is 98 high with no name, description, icon or picture,
// an editor-made one's preview gone; a caption alone keeps the preview).
// "metadataUrl" says that address's page has been read, so the editor reads
// it no more. A link the editor makes is read, as Notion's editor reads one;
// an import, which cannot tell how a bookmark was made there, reads its page.
func plainNotionAPIBookmark(blockType string, raw json.RawMessage, created bool, content map[string]any) {
	if blockType != graphBlockTypeBookmark || content == nil || !created && !notionBlockFieldSpecified(raw, "url") {
		return
	}
	address, _ := content["url"].(string)
	if strings.TrimSpace(address) == "" {
		return
	}
	for _, key := range notionBookmarkPreviewKeys {
		content[key] = ""
	}
	content["metadataUrl"] = strings.TrimSpace(address)
}
