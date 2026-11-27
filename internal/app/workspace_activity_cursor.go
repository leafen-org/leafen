package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

var errWorkspaceActivityInvalid = errors.New(errorCodeWorkspaceActivityInvalid)

// Version 2 walks the feed's own table (2026-10-18), version 3 orders a
// time's lines as they were written (2026-11-17); an earlier walk starts over.
const workspaceActivityCursorVersion = 3

// The frontier is private to the cursor's principal. A later inserted event
// cannot enter an older walk even if its event time sorts behind its boundary.
type workspaceActivityCursor struct {
	Version        int    `json:"v"`
	WorkspaceID    string `json:"w"`
	OrganizationID string `json:"o"`
	PrincipalID    string `json:"u"`
	Frontier       int64  `json:"f"`
	FrontierID     string `json:"a"`
	StartedAt      int64  `json:"s"`
	Time           int64  `json:"t"`
	ID             string `json:"i"`
	// Seq is the last line's order among those of its time, and Notice
	// whether it was a permission line (workspaceActivityOrder).
	Seq           int64 `json:"q"`
	Notice        bool  `json:"n,omitempty"`
	RetentionDays int   `json:"d"`
	// SourceID keeps a walk to one database's own updates (readWorkspaceSourceActivities).
	SourceID string `json:"src,omitempty"`
	// TrashOf keeps a walk to one page's moves to and from the trash (schema_baseline.json).
	TrashOf string `json:"trash,omitempty"`
}

func workspaceActivityCursorKey(user *core.Record) (string, error) {
	key, err := workspaceRefreshTokenKey(user)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte("leafen-workspace-activities-v1\x00" + key))
	return string(digest[:]), nil
}

func sealWorkspaceActivityCursor(user *core.Record, cursor workspaceActivityCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	key, err := workspaceActivityCursorKey(user)
	if err != nil {
		return "", err
	}
	cipher, err := security.Encrypt(data, key)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString([]byte(cipher)), nil
}

func openWorkspaceActivityCursor(user *core.Record, token string) (workspaceActivityCursor, error) {
	var cursor workspaceActivityCursor
	if len(token) < 64 || len(token) > 2048 {
		return cursor, errWorkspaceActivityInvalid
	}
	cipher, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return cursor, errWorkspaceActivityInvalid
	}
	key, err := workspaceActivityCursorKey(user)
	if err != nil {
		return cursor, err
	}
	data, err := security.Decrypt(string(cipher), key)
	if err != nil || json.Unmarshal(data, &cursor) != nil {
		return cursor, errWorkspaceActivityInvalid
	}
	now := time.Now().UTC().UnixMilli()
	if cursor.Version != workspaceActivityCursorVersion || cursor.PrincipalID != user.Id || cursor.WorkspaceID == "" || cursor.OrganizationID == "" ||
		cursor.Frontier <= 0 || cursor.FrontierID == "" || cursor.Time <= 0 || cursor.ID == "" || cursor.Seq <= 0 || cursor.StartedAt <= 0 || cursor.StartedAt > now || now-cursor.StartedAt > int64(24*time.Hour/time.Millisecond) {
		return cursor, errWorkspaceActivityInvalid
	}
	return cursor, nil
}
