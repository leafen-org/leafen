package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// The vault's own store: databases only the app's account sees (the
// manifest marks them hidden), one set for each organization. Nothing in
// them is a secret the server could read: items, folders and keys come
// encrypted by the person's Bitwarden app; the server keeps its own hash of
// the app's password hash, its token secret and the devices' refresh tokens
// as hashes. A row's Bitwarden id is its Leafen row id written as a UUID, so
// a request finds its row in one host call.

var (
	errMissing  = errors.New("not found")
	errNotSetUp = errors.New("the vault isn't set up in this organization")
)

const (
	statusPending = "pending"
	statusActive  = "active"
	statusClosed  = "closed"
)

// store is an organization's tables.
type store struct {
	tenancy                                     string
	accounts, devices, ciphers, folders, config *leafen.Table
	organizations, members, collections         *leafen.Table
	secretKey                                   []byte
}

// stores keeps each organization's tables while the instance lives.
var stores = map[string]*store{}

// openStore finds the organization's tables with one call.
func openStore(tenancy string) (*store, error) {
	if found, ok := stores[tenancy]; ok {
		return found, nil
	}
	// A call for an organization the app has no workspace in would end the
	// run; ask which it has first.
	tenancies, err := leafen.Tenancies()
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(tenancies, func(known leafen.Tenancy) bool { return known.Tenancy == tenancy }) {
		return nil, errNotSetUp
	}
	answer, err := leafen.API(leafen.APIRequest{Tenancy: tenancy, Method: "GET", Path: "/app-data"})
	if err != nil || answer.Status != 200 {
		return nil, errNotSetUp
	}
	data := struct {
		Items []struct {
			Key          string          `json:"key"`
			PageID       string          `json:"pageId"`
			DataSourceID string          `json:"dataSourceId"`
			Properties   []leafen.Column `json:"properties"`
		} `json:"items"`
	}{}
	if err := json.Unmarshal(answer.Body, &data); err != nil {
		return nil, err
	}
	tables := map[string]*leafen.Table{}
	for _, item := range data.Items {
		if item.DataSourceID != "" && item.PageID != "" && len(item.Properties) > 0 {
			tables[item.Key] = &leafen.Table{Tenancy: tenancy, Key: item.Key, PageID: item.PageID, DataSourceID: item.DataSourceID, Columns: item.Properties}
		}
	}
	s := &store{tenancy: tenancy, accounts: tables["accounts"], devices: tables["devices"], ciphers: tables["ciphers"],
		folders: tables["folders"], config: tables["config"], organizations: tables["organizations"], members: tables["members"],
		collections: tables["collections"]}
	if s.accounts == nil || s.devices == nil || s.ciphers == nil || s.folders == nil || s.config == nil || s.organizations == nil ||
		s.members == nil || s.collections == nil {
		return nil, errNotSetUp
	}
	stores[tenancy] = s
	return s, nil
}

// one is the first row a condition finds; a page may come back empty before
// the last, so it reads on.
func one(table *leafen.Table, column, value string) (leafen.Row, error) {
	for row, err := range table.Rows(leafen.Query{Where: []leafen.Where{{Column: column, Operator: "equals", Value: value}}}) {
		return row, err
	}
	return leafen.Row{}, errMissing
}

// owned is every row of an account's in a table, found by the account
// column, which Leafen reads five thousand rows a call.
func owned(table *leafen.Table, accountRow string) ([]leafen.Row, error) {
	rows := []leafen.Row{}
	for row, err := range table.Rows(leafen.Query{Where: []leafen.Where{{Column: "Account", Operator: "contains", Value: accountRow}}}) {
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// ownedRow reads a row by its Bitwarden id when it is the account's.
func ownedRow(table *leafen.Table, accountRow, id string) (leafen.Row, error) {
	rowID, ok := rowOf(id)
	if !ok {
		return leafen.Row{}, errMissing
	}
	row, err := table.Row(rowID)
	if errors.Is(err, leafen.ErrNoRow) {
		return row, errMissing
	}
	if err != nil {
		return row, err
	}
	for _, owner := range row.IDs("Account") {
		if owner == accountRow {
			return row, nil
		}
	}
	return row, errMissing
}

// mutate makes changes to one of the tables in one call, which the app's
// budget counts as one write.
func mutate(table *leafen.Table, operations []any) error {
	answer, err := leafen.API(leafen.APIRequest{Tenancy: table.Tenancy, Method: "POST", Path: "/pages/" + table.PageID + "/content-graph/mutations",
		Body: map[string]any{"mutationId": hexID(), "operations": operations}})
	if err == nil && answer.Status >= 300 {
		err = fmt.Errorf("the %s didn't change (%d): %s", table.Key, answer.Status, answer.Body)
	}
	return err
}

// rowsAtOnce is how many rows one change takes.
const rowsAtOnce = 100

// writeRows sets the same plain values (text, numbers, checkboxes) on many
// rows, a hundred a call.
func writeRows(table *leafen.Table, rows []leafen.Row, values map[string]any) error {
	encoded := map[string]any{}
	for name, value := range values {
		column, ok := table.Column(name)
		if !ok {
			return fmt.Errorf("the %s have no %q column", table.Key, name)
		}
		encoded[column.ID] = map[string]any{"type": column.Type, "value": value}
	}
	for start := 0; start < len(rows); start += rowsAtOnce {
		operations := []any{}
		for _, row := range rows[start:min(start+rowsAtOnce, len(rows))] {
			operations = append(operations, map[string]any{"kind": "upsertRow", "row": map[string]any{
				"id": row.ID, "dataSourceId": table.DataSourceID, "position": row.Position, "values": encoded}})
		}
		if err := mutate(table, operations); err != nil {
			return err
		}
	}
	return nil
}

// trashRows puts rows in the app workspace's trash, as a person removing
// them would — two calls each — while budget, a run's host calls to spend,
// lasts; done says every one went.
func trashRows(tenancy string, ids []string, budget *int) (done bool, err error) {
	for _, id := range ids {
		if *budget < 2 {
			return false, nil
		}
		*budget -= 2
		if err := trashRow(tenancy, id); err != nil {
			return false, err
		}
	}
	return true, nil
}

func trashRow(tenancy, rowID string) error {
	read, err := leafen.API(leafen.APIRequest{Tenancy: tenancy, Method: "GET", Path: "/database-rows/" + rowID + "/page"})
	if err != nil {
		return err
	}
	if read.Status == 404 {
		return nil
	}
	page := struct {
		Row struct {
			AuthorityRevision int    `json:"authorityRevision"`
			TrashedAt         string `json:"trashedAt"`
		} `json:"row"`
	}{}
	if read.Status != 200 || json.Unmarshal(read.Body, &page) != nil {
		return fmt.Errorf("the row couldn't be read (%d)", read.Status)
	}
	if page.Row.TrashedAt != "" {
		return nil
	}
	answer, err := leafen.API(leafen.APIRequest{Tenancy: tenancy, Method: "POST", Path: "/database-rows/" + rowID + "/page/trash",
		Body: map[string]any{"requestId": "vault-" + hexID(), "expectedAuthorityRevision": page.Row.AuthorityRevision}})
	if err == nil && answer.Status >= 300 && answer.Status != 404 {
		err = fmt.Errorf("the row wasn't removed (%d): %s", answer.Status, answer.Body)
	}
	return err
}

func idsOf(rows []leafen.Row) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}

func hexID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// bitwardenID writes a row's id (24 hexadecimal digits, as the SDK makes
// them) as a UUID.
func bitwardenID(rowID string) string {
	if len(rowID) != 24 {
		return rowID
	}
	h := rowID + "00000000"
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// rowOf reads a row's id back from its Bitwarden id.
func rowOf(id string) (string, bool) {
	h := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(id)), "-", "")
	if len(h) != 32 || !strings.HasSuffix(h, "00000000") {
		return "", false
	}
	for _, c := range h {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", false
		}
	}
	return h[:24], true
}

func randomToken(size int) string {
	b := make([]byte, size)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func nowMs() int64 { return time.Now().UnixMilli() }

// stamp is a time as Bitwarden's apps read it.
func stamp(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000000Z")
}

func number(row leafen.Row, column string) int64 { return int64(row.Number(column)) }

func normalEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// account is a person's vault account as kept.
type account struct {
	row                                           leafen.Row
	Email, Person, Name, Hint, Status             string
	Key, PublicKey, PrivateKey, Stamp, Hash, Salt string
	Kdf, Iterations, Memory, Parallelism          int64
	Created, Revision, Expires                    int64
}

func accountOf(row leafen.Row) account {
	return account{row: row, Email: row.Text("Email"), Person: row.Text("Person"), Name: row.Text("Name"),
		Hint: row.Text("Hint"), Status: row.Option("Status"), Key: row.Text("Key"), PublicKey: row.Text("Public key"),
		PrivateKey: row.Text("Private key"), Stamp: row.Text("Stamp"), Hash: row.Text("Hash"), Salt: row.Text("Salt"),
		Kdf: number(row, "Kdf"), Iterations: number(row, "Iterations"), Memory: number(row, "Memory"),
		Parallelism: number(row, "Parallelism"), Created: number(row, "Created"), Revision: number(row, "Revision"),
		Expires: number(row, "Expires")}
}

// id is the account's Bitwarden id.
func (acct account) id() string { return bitwardenID(acct.row.ID) }

// waiting says whether an account prepared in Leafen still waits for its
// Bitwarden app, within its day.
func (acct account) waiting(now int64) bool {
	return acct.Status == statusPending && acct.Expires > now
}

// accountByEmail finds the account an email names, of those not closed.
func (s *store) accountByEmail(email string) (account, error) {
	email = normalEmail(email)
	if email == "" {
		return account{}, errMissing
	}
	row, err := one(s.accounts, "Email", email)
	if err != nil {
		return account{}, err
	}
	acct := accountOf(row)
	if acct.Status == statusClosed {
		return account{}, errMissing
	}
	return acct, nil
}

// accountByPerson finds a person's account, of those not closed.
func (s *store) accountByPerson(person string) (account, error) {
	for row, err := range s.accounts.Rows(leafen.Query{Where: []leafen.Where{{Column: "Person", Operator: "equals", Value: person}}}) {
		if err != nil {
			return account{}, err
		}
		if acct := accountOf(row); acct.Status != statusClosed {
			return acct, nil
		}
	}
	return account{}, errMissing
}

func (s *store) accountByRow(rowID string) (account, error) {
	row, err := s.accounts.Row(rowID)
	if errors.Is(err, leafen.ErrNoRow) {
		return account{}, errMissing
	}
	return accountOf(row), err
}

// An account's revision tells its devices when to sync: the newest change
// to it, its items or its folders. Items and folders keep their own, and the
// app's temporary store keeps the newest, so a change costs one write; a
// removal, which leaves no row behind, also moves the account's own on.

func revisionKey(acct account) string { return "revision:" + acct.row.ID }

// changed notes a change to an account's items or folders.
func (s *store) changed(acct account) {
	_, _ = leafen.KV(leafen.KVRequest{Command: "SET", Key: revisionKey(acct), Value: strconv.FormatInt(nowMs(), 10), TTL: 3600, Tenancy: s.tenancy})
}

// removed notes a removal, in the account's row too.
func (s *store) removed(acct account) error {
	s.changed(acct)
	return s.accounts.Update(acct.row.ID, map[string]any{"Revision": float64(nowMs())})
}

// revision is the account's newest change, or its shared vaults'; when the
// temporary store forgot it, its items and folders tell.
func (s *store) revision(acct account) (int64, error) {
	newest := acct.Revision
	kept, err := leafen.KV(leafen.KVRequest{Command: "GET", Key: revisionKey(acct), Tenancy: s.tenancy})
	if err == nil && countOf(kept) > 0 {
		newest = max(newest, int64(countOf(kept)))
	} else {
		for _, table := range []*leafen.Table{s.ciphers, s.folders} {
			rows, err := owned(table, acct.row.ID)
			if err != nil {
				return 0, err
			}
			for _, row := range rows {
				newest = max(newest, number(row, "Revision"))
			}
		}
		_, _ = leafen.KV(leafen.KVRequest{Command: "SET", Key: revisionKey(acct), Value: strconv.FormatInt(newest, 10), TTL: 3600, Tenancy: s.tenancy})
	}
	places, err := s.memberships(acct)
	if err != nil {
		return 0, err
	}
	for _, m := range places {
		// A change to the account's place moves it on too.
		newest = max(newest, m.Revision)
		if m.State == memberConfirmed {
			shared, err := s.orgRevision(m.Organization)
			if err != nil && !errors.Is(err, leafen.ErrNoRow) {
				return 0, err
			}
			newest = max(newest, shared)
		}
	}
	return newest, nil
}

// secret is the organization's token-signing secret, made on first use.
func (s *store) secret() ([]byte, error) {
	if s.secretKey != nil {
		return s.secretKey, nil
	}
	row, err := one(s.config, "Name", "token secret")
	if errors.Is(err, errMissing) {
		if _, err := s.config.Add(map[string]any{"Name": "token secret", "Value": randomToken(32)}); err != nil {
			return nil, err
		}
		// Two first uses at once both keep the one found first.
		row, err = one(s.config, "Name", "token secret")
	}
	if err != nil {
		return nil, err
	}
	key, err := base64.RawURLEncoding.DecodeString(row.Text("Value"))
	if err == nil {
		s.secretKey = key
	}
	return key, err
}
