package app

import "encoding/json"

// UnmarshalJSON keeps a block's type object, such as "heading_1" or
// "callout", as Notion sent it, so the importer can convert every block type
// without a field for each one.
func (object *notionProviderObject) UnmarshalJSON(data []byte) error {
	type plain notionProviderObject
	decoded := plain{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		identity := struct {
			Object string `json:"object"`
			ID     string `json:"id"`
			Type   string `json:"type"`
		}{}
		if json.Unmarshal(data, &identity) != nil || identity.Object != "block" || identity.ID == "" {
			return err
		}
		*object = notionProviderObject{Object: identity.Object, ID: identity.ID, Type: identity.Type, Unreadable: true}
		return nil
	}
	*object = notionProviderObject(decoded)
	if object.Object != "block" || object.Type == "" {
		return nil
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	object.Payload = fields[object.Type]
	return nil
}
