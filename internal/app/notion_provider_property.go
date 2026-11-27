package app

import "encoding/json"

// notionProviderProperty is a Notion property as a database describes it or
// as a page holds its value. Settings is the object under the property's
// type, for a database the property's settings and for a page its value; a
// relation value says with HasMore that Notion left some related pages out.
type notionProviderProperty struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	Description *string         `json:"description,omitempty"`
	Title       json.RawMessage `json:"title"`
	RichText    json.RawMessage `json:"rich_text"`
	Number      json.RawMessage `json:"number"`
	People      json.RawMessage `json:"people"`
	Settings    json.RawMessage `json:"-"`
	HasMore     bool            `json:"-"`
}

func (property *notionProviderProperty) UnmarshalJSON(data []byte) error {
	type plain notionProviderProperty
	decoded := plain{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	decoded.Settings = fields[decoded.Type]
	if raw, present := fields["has_more"]; present {
		_ = json.Unmarshal(raw, &decoded.HasMore)
	}
	*property = notionProviderProperty(decoded)
	return nil
}
