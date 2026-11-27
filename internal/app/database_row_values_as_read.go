package app

import (
	"encoding/json"
	"errors"
	"reflect"

	"github.com/pocketbase/pocketbase/core"
)

// A value can be written on the condition that the row still holds it as its
// writer read it: a title typed in one window and merged with the title the
// server held then is written only while the server still holds that title.
// Two windows saving the same instant both read the same title; the second
// to arrive finds it changed, is refused, and its writer merges again with
// the title now held and sends that (rowTitleSave.ts), so both typings stay
// (2026-11-11: two saves 20ms apart kept only the later). Its words are what
// count: marks changed alone are not a change of it.

var errDatabaseRowPageValueChanged = errors.New(errorCodeDatabaseRowPageValueChanged)

// databaseRowValuesAsRead refuses with errDatabaseRowPageValueChanged when
// the row no longer holds one of the values its writer read.
func databaseRowValuesAsRead(app core.App, authority databaseRowPageAuthority, read map[string]graphRowValue) error {
	if len(read) == 0 {
		return nil
	}
	if len(read) > databaseRowPageValuesLimit {
		return errDatabaseRowPageValuesInvalid
	}
	stored, err := loadGraphRowValues(app, authority.OwnerID, authority.ContainerPageID, []string{authority.RowID})
	if err != nil {
		return err
	}
	held := make(map[string]string, len(stored))
	for _, value := range stored {
		held[value.PropertyID] = value.Value
	}
	for propertyID, value := range read {
		if !sameRowValueWords(held[propertyID], value.Value) {
			return errDatabaseRowPageValueChanged
		}
	}
	return nil
}

// sameRowValueWords is whether a stored value reads as a value read: an
// empty one as none at all.
func sameRowValueWords(stored string, read json.RawMessage) bool {
	decode := func(raw []byte) any {
		var value any
		if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
			return nil
		}
		switch typed := value.(type) {
		case string:
			if typed == "" {
				return nil
			}
		case []any:
			if len(typed) == 0 {
				return nil
			}
		}
		return value
	}
	return reflect.DeepEqual(decode([]byte(stored)), decode(read))
}
