package app

import "strings"

// An app's guest rooms are named by keys its code uses ("table-1"); people
// read them by the names the app's screen gives them ("테이블 1", "Table 1"),
// which the manifest's guests.names gives in each language. The review, the
// consent window and the guest links name the rooms so, in the reader's
// language (2026-11-15: they listed "table-1, table-2, …" on Korean screens).

// appGuestRoomNameProblems checks that guests.names names declared rooms, in
// language tags, each name 1 to appNameMaxRunes characters.
func appGuestRoomNameProblems(guests *appManifestGuests, rooms map[string]bool, add func(string, ...any)) {
	for room, names := range guests.Names {
		if !rooms[room] {
			add("guests.names names %q, which isn't one of guests.rooms", room)
		}
		appTextProblems(names, "guest room "+room+" name", appNameMaxRunes, true, add)
	}
}

// appGuestRoomsNamed is the rooms by name in each language a room is named
// in, in the rooms' order — a room unnamed in a language by its English
// name, else its key — or nil when no room is named.
func appGuestRoomsNamed(guests *appManifestGuests) map[string]string {
	languages := map[string]bool{}
	for _, names := range guests.Names {
		for language := range names {
			languages[language] = true
		}
	}
	if len(languages) == 0 {
		return nil
	}
	named := map[string]string{}
	for language := range languages {
		parts := make([]string, 0, len(guests.Rooms))
		for _, room := range guests.Rooms {
			name := guests.Names[room][language]
			if name == "" {
				name = guests.Names[room]["en"]
			}
			if name == "" {
				name = room
			}
			parts = append(parts, name)
		}
		named[language] = strings.Join(parts, ", ")
	}
	return named
}

// appGuestsApproved is what an approval holds of an app's guests: the rooms
// and how many each takes, not what they are called, so naming them keeps an
// app's approval.
func appGuestsApproved(guests *appManifestGuests) *appManifestGuests {
	if guests == nil || len(guests.Names) == 0 {
		return guests
	}
	approved := *guests
	approved.Names = nil
	return &approved
}
