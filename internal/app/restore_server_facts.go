package app

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// dropServerAddresses takes out of data on its way in by a restore the
// addresses its setup facts name (setup-state.json's here and outside):
// they say where the server the data came from answered, not where this
// copy will, and a command on a copy not yet served printed a sign-in link
// to that server — on the same computer, the live one (2026-11-19). The
// server that serves the data writes its own as it starts; until then a
// command names no address. Every restore goes through restoreDataRoot or
// leaveRestoreNote, which call this.
func dropServerAddresses(root string) error {
	path := filepath.Join(root, setupStateFile)
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	// Read loosely, so whatever else the file holds stays as it was.
	state := map[string]json.RawMessage{}
	facts := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &state) != nil || json.Unmarshal(state["facts"], &facts) != nil {
		return nil
	}
	if facts["here"] == nil && facts["outside"] == nil {
		return nil
	}
	delete(facts, "here")
	delete(facts, "outside")
	if state["facts"], err = json.Marshal(facts); err != nil {
		return err
	}
	return writeSetupFile(root, setupStateFile, state)
}
