package app

import (
	"crypto/rand"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A server's own number, made once with its data and kept through backups,
// restores and moves of that data, by which the server names itself; a new
// server gets a new one. It reads "LFN-XXXX-XXXX-XXXX-XXXX" in Crockford's
// base 32.

const serverIdentityAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func newServerID() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	var id strings.Builder
	id.WriteString("LFN")
	for index, value := range random {
		if index%4 == 0 {
			id.WriteByte('-')
		}
		id.WriteByte(serverIdentityAlphabet[int(value)%len(serverIdentityAlphabet)])
	}
	return id.String(), nil
}

func renewServerID(app core.App) error {
	serverID, err := newServerID()
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`UPDATE {{leafen_server_identity}} SET [[server_id]] = {:serverId}, [[created_at]] = unixepoch('subsec') * 1000
		WHERE [[id]] = 'current'`).Bind(map[string]any{"serverId": serverID}).Execute()
	return err
}

// loadServerID reads the server's own number.
func loadServerID(app core.App) (string, error) {
	serverID := ""
	err := app.DB().NewQuery(`SELECT [[server_id]] FROM {{leafen_server_identity}} WHERE [[id]] = 'current'`).Row(&serverID)
	return serverID, err
}
