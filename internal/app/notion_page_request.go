package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

type notionPageParentRequest struct {
	Type         string `json:"type,omitempty"`
	PageID       string `json:"page_id,omitempty"`
	DatabaseID   string `json:"database_id,omitempty"`
	DataSourceID string `json:"data_source_id,omitempty"`
	Workspace    bool   `json:"workspace,omitempty"`
}

// notionRequestProperties are a request's page properties, their keys in
// the order sent. The request check takes any value there but a falsy one
// (notionPagePropertiesShape). A database page reads the keys JavaScript's
// Object.keys finds (seen 2026-11-10: "properties": "xy" names "0" and "1",
// [1] names "0", 5 and true none); a page outside a database reads one that
// isn't an object as naming none (a page made with "properties": "x", []
// or 5 has no title).
type notionRequestProperties struct {
	keys     []string
	values   map[string]json.RawMessage
	isObject bool
}

func (properties *notionRequestProperties) UnmarshalJSON(raw []byte) error {
	value, err := parseNotionValue(raw)
	if err != nil {
		return err
	}
	read := notionRequestProperties{values: map[string]json.RawMessage{}}
	switch value.kind {
	case 'o':
		if err := json.Unmarshal(raw, &read.values); err != nil {
			return err
		}
		read.keys, read.isObject = value.keys, true
	case 'a':
		items := []json.RawMessage{}
		if err := json.Unmarshal(raw, &items); err != nil {
			return err
		}
		for index, item := range items {
			read.keys = append(read.keys, strconv.Itoa(index))
			read.values[strconv.Itoa(index)] = item
		}
	case 's':
		index := 0
		for _, letter := range value.text {
			encoded, _ := json.Marshal(string(letter))
			read.keys = append(read.keys, strconv.Itoa(index))
			read.values[strconv.Itoa(index)] = encoded
			index += utf16.RuneLen(letter)
		}
	}
	*properties = read
	return nil
}

// sortedNotionRequestProperties holds values whose sent order is gone (an
// MCP tool's, read into a map) in their keys' order, so they are read the
// same way each time.
func sortedNotionRequestProperties(values map[string]json.RawMessage) notionRequestProperties {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return notionRequestProperties{keys: keys, values: values, isObject: true}
}

// count is how many keys a database page reads.
func (properties notionRequestProperties) count() int { return len(properties.keys) }

// given tells properties the request sent, even none ({}).
func (properties notionRequestProperties) given() bool { return properties.values != nil }

// object is the properties a page outside a database reads: none unless
// they were an object.
func (properties notionRequestProperties) object() map[string]json.RawMessage {
	if !properties.isObject {
		return nil
	}
	return properties.values
}

type notionPageCreateRequest struct {
	Parent     notionPageParentRequest `json:"parent"`
	Properties notionRequestProperties `json:"properties"`
	Icon       json.RawMessage         `json:"icon,omitempty"`
	Cover      json.RawMessage         `json:"cover,omitempty"`
	IsLocked   *bool                   `json:"is_locked,omitempty"`
	Content    []json.RawMessage       `json:"content,omitempty"`
	Children   []json.RawMessage       `json:"children,omitempty"`
	Markdown   *string                 `json:"markdown,omitempty"`
	// MarkdownVersion is the version Markdown is written in
	// (notionMarkdownVersion2).
	MarkdownVersion string          `json:"markdown_version,omitempty"`
	AllowAsync      bool            `json:"allow_async,omitempty"`
	Template        json.RawMessage `json:"template,omitempty"`
	Position        json.RawMessage `json:"position,omitempty"`
}

type notionPageUpdateRequest struct {
	Properties notionRequestProperties `json:"properties,omitempty"`
	Icon       json.RawMessage         `json:"icon,omitempty"`
	Cover      json.RawMessage         `json:"cover,omitempty"`
	IsLocked   *bool                   `json:"is_locked,omitempty"`
	Template   json.RawMessage         `json:"template,omitempty"`
	Erase      *bool                   `json:"erase_content,omitempty"`
	InTrash    *bool                   `json:"in_trash,omitempty"`
	IsArchived *bool                   `json:"is_archived,omitempty"`
}

type notionPageMoveRequest struct {
	Parent notionPageParentRequest `json:"parent"`
}

type notionPagePresentationRequest struct {
	Icon            *string
	IconType        *string
	IconManaged     bool
	IconCustomEmoji bool
	Cover           *string
	CoverManaged    bool
}

// trashState reads in_trash; archived, which in_trash replaced, is not part
// of this API version. is_archived is the separate archive state and never
// moves a page to trash.
func (input notionPageUpdateRequest) trashState(current bool) (bool, bool, error) {
	if input.InTrash == nil {
		return current, false, nil
	}
	return *input.InTrash, true, nil
}

func (input notionPageUpdateRequest) hasTrashState() bool {
	return input.InTrash != nil
}

// notionOrdinaryPageTitle reads a page's title as Notion's API takes it: rich
// text of text, mentions and equations, with their marks and links (seen
// 2026-11-10), its mentions checked and named as a database's name's are
// (notionNameMentions), its spaces, tabs and line breaks kept where they
// are, its ends too, as Notion's API keeps them (seen 2026-11-13: "끝 ",
// " 앞", "\t탭\t", "\n줄\n바꿈\n" and "   " read back as sent, an empty
// piece left out). The spans are none for a title of plain words
// (page_title_spans.go). A mention Notion refuses for what it names is said
// alone (notionMentionRefusedAlone).
//
// The request check (notionPagePropertiesShape) has passed each value; a page
// outside a database then reads them as Notion's does (seen 2026-11-10):
// "title" alone names its property, an object under another key is refused
// and anything else there is left; the title is {"title": [...]} or the
// list itself, null empties it, another kind's object leaves it as it is,
// and a plain value is refused.
func notionOrdinaryPageTitle(
	properties map[string]json.RawMessage, mentions func([]graphTextSpan) error,
) (string, []graphTextSpan, bool, error) {
	if properties == nil {
		return "", nil, false, nil
	}
	found := false
	var spans []graphTextSpan
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		raw := bytes.TrimSpace(properties[key])
		object := len(raw) > 0 && raw[0] == '{'
		if key != notionTitlePropertyID {
			if object {
				return "", nil, false, fmt.Errorf("Invalid property identifier %s", key)
			}
			continue
		}
		var items json.RawMessage
		switch {
		case object:
			fields := map[string]json.RawMessage{}
			if json.Unmarshal(raw, &fields) != nil {
				return "", nil, false, errors.New("title rich text is invalid")
			}
			items = fields[notionTitlePropertyID]
		case len(raw) > 0 && raw[0] == '[':
			items = raw
		case string(raw) == "null":
			found, spans = true, nil
			continue
		default:
			return "", nil, false, fmt.Errorf("%s has a value that doesn't fit its property type: undefined.", key)
		}
		if items == nil {
			continue
		}
		found = true
		list := []any{}
		if json.Unmarshal(items, &list) != nil {
			return "", nil, false, errors.New("title rich text is invalid")
		}
		var err error
		if spans, err = notionTextRequestGraphSpans(list); err != nil {
			if alone, refused := notionMentionRefusedAlone(err); refused {
				return "", nil, false, alone
			}
			return "", nil, false, errors.New("title rich text is invalid")
		}
	}
	if err := mentions(spans); err != nil {
		if alone, refused := notionMentionRefusedAlone(err); refused {
			return "", nil, false, alone
		}
		var requestError *graphRequestError
		if errors.As(err, &requestError) {
			return "", nil, false, errors.New(requestError.message)
		}
		return "", nil, false, err
	}
	title := spelledName(spans)
	if len([]rune(title)) > maxGraphTitleRunes {
		return "", nil, false, errors.New("title is too long")
	}
	words, _ := json.Marshal(title)
	kept, err := normalizeGraphRowValueSpans(graphValueTypeTitle, words, spans)
	if err != nil {
		return "", nil, false, errors.New("title rich text is invalid")
	}
	return title, kept, found, nil
}

func notionPagePresentation(icon, cover json.RawMessage) (notionPagePresentationRequest, error) {
	result := notionPagePresentationRequest{}
	if icon != nil {
		empty := ""
		none := "none"
		result.Icon, result.IconType = &empty, &none
		if string(icon) != "null" {
			value := struct {
				Type     string `json:"type"`
				Emoji    string `json:"emoji,omitempty"`
				External *struct {
					URL string `json:"url"`
				} `json:"external,omitempty"`
				FileUpload *struct {
					ID string `json:"id"`
				} `json:"file_upload,omitempty"`
				CustomEmoji *struct {
					ID string `json:"id"`
				} `json:"custom_emoji,omitempty"`
				Icon *struct {
					Name  string  `json:"name"`
					Color *string `json:"color"`
				} `json:"icon,omitempty"`
			}{}
			if json.Unmarshal(icon, &value) != nil {
				return notionPagePresentationRequest{}, errors.New("invalid page icon")
			}
			// Notion takes an icon without its type, from the one key it
			// carries ({"emoji": "🥬"}, as its own docs show it; seen 2026-10-17).
			if value.Type == "" {
				switch {
				case value.Emoji != "":
					value.Type = "emoji"
				case value.External != nil:
					value.Type = "external"
				case value.FileUpload != nil:
					value.Type = "file_upload"
				case value.CustomEmoji != nil:
					value.Type = "custom_emoji"
				case value.Icon != nil:
					value.Type = nativeIconType
				}
			}
			switch value.Type {
			case "emoji":
				if strings.TrimSpace(value.Emoji) == "" {
					return notionPagePresentationRequest{}, errors.New("invalid page emoji")
				}
				emoji, kind := value.Emoji, "emoji"
				result.Icon, result.IconType = &emoji, &kind
			case "external":
				if value.External == nil {
					return notionPagePresentationRequest{}, errors.New("invalid external page icon")
				}
				// An image of Notion's icon set sets that built-in icon.
				if stored, notion, err := nativeIconFromNotionURL(value.External.URL); notion {
					if err != nil {
						return notionPagePresentationRequest{}, err
					}
					kind := nativeIconType
					result.Icon, result.IconType = &stored, &kind
					break
				}
				// An address Notion can't take is said in its words (seen 2026-11-10).
				if notionPageExternalFile(value.External.URL) == nil {
					return notionPagePresentationRequest{}, errors.New("Invalid page icon URL.")
				}
				url, kind := value.External.URL, "image"
				result.Icon, result.IconType = &url, &kind
			case "file_upload":
				fileUploadID, ok := "", false
				if value.FileUpload != nil {
					fileUploadID, ok = normalizeNotionUUIDRequest(value.FileUpload.ID)
				}
				if !ok {
					return notionPagePresentationRequest{}, errors.New("invalid page icon file_upload")
				}
				kind := "image"
				result.Icon, result.IconType, result.IconManaged = &fileUploadID, &kind, true
			case "custom_emoji":
				if value.CustomEmoji == nil || !validCustomEmojiID(value.CustomEmoji.ID) {
					return notionPagePresentationRequest{}, errors.New("invalid page custom_emoji")
				}
				emojiID, kind := value.CustomEmoji.ID, "custom_emoji"
				result.Icon, result.IconType, result.IconCustomEmoji = &emojiID, &kind, true
			case nativeIconType:
				if value.Icon == nil {
					return notionPagePresentationRequest{}, errors.New("invalid page icon")
				}
				stored, err := nativeIconRequestValue(value.Icon.Name, value.Icon.Color)
				if err != nil {
					return notionPagePresentationRequest{}, err
				}
				kind := nativeIconType
				result.Icon, result.IconType = &stored, &kind
			default:
				return notionPagePresentationRequest{}, errors.New("unsupported page icon type")
			}
		}
	}
	if cover != nil {
		empty := ""
		result.Cover = &empty
		if string(cover) != "null" {
			value := struct {
				Type     string `json:"type"`
				External *struct {
					URL string `json:"url"`
				} `json:"external,omitempty"`
				FileUpload *struct {
					ID string `json:"id"`
				} `json:"file_upload,omitempty"`
			}{}
			if json.Unmarshal(cover, &value) != nil {
				return notionPagePresentationRequest{}, errors.New("unsupported page cover type")
			}
			// A cover without its type too.
			if value.Type == "" {
				switch {
				case value.External != nil:
					value.Type = "external"
				case value.FileUpload != nil:
					value.Type = "file_upload"
				}
			}
			switch value.Type {
			case "external":
				if value.External == nil || notionPageExternalFile(value.External.URL) == nil {
					return notionPagePresentationRequest{}, errors.New("Invalid page cover URL.")
				}
				url := value.External.URL
				result.Cover = &url
			case "file_upload":
				fileUploadID, ok := "", false
				if value.FileUpload != nil {
					fileUploadID, ok = normalizeNotionUUIDRequest(value.FileUpload.ID)
				}
				if !ok {
					return notionPagePresentationRequest{}, errors.New("invalid page cover file_upload")
				}
				result.Cover, result.CoverManaged = &fileUploadID, true
			default:
				return notionPagePresentationRequest{}, errors.New("unsupported page cover type")
			}
		}
	}
	return result, nil
}

func notionPageParentKind(parent notionPageParentRequest) string {
	count := 0
	kind := ""
	if strings.TrimSpace(parent.PageID) != "" {
		count++
		kind = "page"
	}
	if strings.TrimSpace(parent.DatabaseID) != "" {
		count++
		kind = "database"
	}
	if strings.TrimSpace(parent.DataSourceID) != "" {
		count++
		kind = "data_source"
	}
	if parent.Workspace {
		count++
		kind = "workspace"
	}
	if count != 1 {
		return ""
	}
	return kind
}

// notionPageTemplateRequest is a page create template: none, the data
// source's default template, or one of its templates by ID.
type notionPageTemplateRequest struct {
	Type       string `json:"type"`
	TemplateID string `json:"template_id,omitempty"`
	Timezone   string `json:"timezone,omitempty"`
}

func (template notionPageTemplateRequest) applies() bool {
	return template.Type == "default" || template.Type == "template_id"
}

func parseNotionPageCreateTemplate(raw json.RawMessage) (notionPageTemplateRequest, error) {
	if raw == nil || string(raw) == "null" {
		return notionPageTemplateRequest{Type: "none"}, nil
	}
	template := notionPageTemplateRequest{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&template) != nil {
		return template, errors.New("page template is invalid")
	}
	valid := false
	switch template.Type {
	case "none":
		valid = template.TemplateID == "" && template.Timezone == ""
	case "default":
		valid = template.TemplateID == ""
	case "template_id":
		valid = strings.TrimSpace(template.TemplateID) != ""
	}
	if !valid {
		return template, errors.New("page template is invalid")
	}
	// Leafen templates hold no date variables, so the timezone only has to be
	// a valid IANA name.
	if template.Timezone != "" {
		if _, err := time.LoadLocation(template.Timezone); err != nil || template.Timezone == "Local" {
			return template, errors.New("page template timezone is invalid")
		}
	}
	return template, nil
}

func validateNotionPageCreateTemplate(raw json.RawMessage) error {
	_, err := parseNotionPageCreateTemplate(raw)
	return err
}
