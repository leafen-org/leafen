package app

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// What the reader hands in (notion_migration_formats_reader_hand_in.js): the
// key, where to come back, the parts' sizes as the reader counted them, then
// the parts — "discussions", which blocks have comments, "formats", the
// settings, and "orders", the rows' orders, the largest, last — each as JSON or, packed,
// as gzip in base64url ("formats.gz"). The same settings come once: a view
// or a database naming one before it ("like") is that one's, with its own
// database or title, and each rows' order once, by the views that have it
// (2026-11-23: 7,324 rows each holding the same linked views, their rows'
// order among them, came to more than 16 MiB, and Leafen took none of it).
// Past Leafen's size for one reading, the parts that came whole are taken
// and the others named with their sizes (notion_migration_formats_refused.go):
// without its rows' orders a reading is still matched, its views put in order
// by hand listed; without its settings it is refused, but which blocks have
// comments is kept, so the run's comments still ask only those. An earlier
// reader's plain "formats", with its discussions in it, is taken too.

// notionMigrationFormatsMostUnpacked is the most one part unpacks to.
const notionMigrationFormatsMostUnpacked = 64 << 20

var errNotionMigrationFormatsPartTooBig = errors.New("notion migration formats: a part is over the size Leafen takes")

// notionMigrationFormatsPart is a part the reader sent and Leafen didn't
// take, with its size as read and as sent, in bytes.
type notionMigrationFormatsPart struct {
	Name string `json:"name"`
	Read int64  `json:"read"`
	Sent int64  `json:"sent"`
}

type notionMigrationFormatsOrders struct {
	Lists [][]string     `json:"lists"`
	Of    map[string]int `json:"of"`
}

// readNotionMigrationFormatsForm reads the reader's form up to Leafen's size
// for one reading. Past it, the form is cut at the last field that came
// whole, and cut says so.
func readNotionMigrationFormatsForm(request *http.Request) (url.Values, bool, error) {
	body, err := io.ReadAll(io.LimitReader(request.Body, notionMigrationFormatsMaxBytes+1))
	if err != nil {
		return nil, false, err
	}
	cut := len(body) > notionMigrationFormatsMaxBytes
	if cut {
		body = body[:max(0, bytes.LastIndexByte(body[:notionMigrationFormatsMaxBytes], '&'))]
	}
	values, err := url.ParseQuery(string(body))
	return values, cut, err
}

// notionMigrationFormatsParts gives the reading the form holds and the parts
// sent that Leafen didn't take. A reading whose settings didn't come whole,
// or unpack past Leafen's size, is refused as too big
// (errNotionMigrationFormatsPartTooBig), with its discussions when they came.
func notionMigrationFormatsParts(form url.Values, cut bool) (notionMigrationFormatsRead, []notionMigrationFormatsPart, error) {
	read, sent, left := notionMigrationFormatsRead{}, notionMigrationFormatsSent(form.Get("sizes")), []notionMigrationFormatsPart{}
	var discussions *notionMigrationDiscussions
	if text, err := notionMigrationFormatsPartText(form, "discussions"); err == nil && text != nil && json.Unmarshal(text, &discussions) != nil {
		discussions = nil
	}
	settings, err := notionMigrationFormatsPartText(form, "formats")
	if err == nil && settings == nil && cut {
		err = errNotionMigrationFormatsPartTooBig
	}
	if err != nil {
		read.Discussions = discussions
		for _, part := range sent {
			if part.Name != "discussions" || discussions == nil {
				left = append(left, part)
			}
		}
		return read, left, err
	}
	if settings == nil || json.Unmarshal(settings, &read) != nil || read.Format != 1 {
		return read, nil, errors.New("notion migration formats: the reading can't be read")
	}
	if discussions != nil {
		read.Discussions = discussions
	}
	orders := notionMigrationFormatsOrders{}
	text, err := notionMigrationFormatsPartText(form, "orders")
	taken := err == nil && text != nil && json.Unmarshal(text, &orders) == nil
	if !taken && (err != nil || cut) {
		for _, part := range sent {
			if part.Name == "orders" {
				left = append(left, part)
			}
		}
	}
	// What names another is that one, from the reading as it came.
	databases := map[string]notionMigrationDatabaseFormat{}
	for id, database := range read.Databases {
		if database.Like != "" {
			base, found := read.Databases[database.Like]
			if !found || base.Like != "" {
				continue // One naming no database of the reading has nothing to be.
			}
			base.Title = database.Title
			database = base
		}
		databases[id] = database
	}
	views := map[string]notionMigrationViewFormat{}
	for id, view := range read.Views {
		if view.Like != "" {
			base, found := read.Views[view.Like]
			if !found || base.Like != "" {
				continue
			}
			base.Database = view.Database
			view = base
		}
		if index, found := orders.Of[id]; taken && found && index >= 0 && index < len(orders.Lists) {
			view.Rows = orders.Lists[index]
		}
		// A view put in order by hand whose order wasn't taken is listed as not read.
		view.RowsNotTaken, view.Ordered = view.Ordered && view.Rows == nil, false
		views[id] = view
	}
	read.Databases, read.Views = databases, views
	return read, left, nil
}

// notionMigrationFormatsPartText is a part as sent, unpacked; nil when it
// didn't come.
func notionMigrationFormatsPartText(form url.Values, name string) ([]byte, error) {
	if packed, found := form[name+".gz"]; found && len(packed) > 0 {
		zipped, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(packed[0], "="))
		if err != nil {
			return nil, err
		}
		reader, err := gzip.NewReader(bytes.NewReader(zipped))
		if err != nil {
			return nil, err
		}
		text, err := io.ReadAll(io.LimitReader(reader, notionMigrationFormatsMostUnpacked+1))
		if err == nil && len(text) > notionMigrationFormatsMostUnpacked {
			err = errNotionMigrationFormatsPartTooBig
		}
		return text, err
	}
	if plain, found := form[name]; found && len(plain) > 0 {
		return []byte(plain[0]), nil
	}
	return nil, nil
}

// notionMigrationFormatsSent reads the parts' sizes the reader says it sent:
// "formats.gz:<read>:<sent>,orders.gz:…", a part's name without ".gz".
func notionMigrationFormatsSent(sizes string) []notionMigrationFormatsPart {
	parts := []notionMigrationFormatsPart{}
	for _, entry := range strings.Split(sizes, ",") {
		fields := strings.Split(entry, ":")
		if len(fields) != 3 || len(parts) >= 8 {
			continue
		}
		read, readErr := strconv.ParseInt(fields[1], 10, 64)
		sent, sentErr := strconv.ParseInt(fields[2], 10, 64)
		if readErr == nil && sentErr == nil && read >= 0 && sent >= 0 {
			parts = append(parts, notionMigrationFormatsPart{Name: strings.TrimSuffix(fields[0], ".gz"), Read: read, Sent: sent})
		}
	}
	return parts
}

// notionMigrationFormatsSizeWords says a size in MiB.
func notionMigrationFormatsSizeWords(bytes int64) string {
	return fmt.Sprintf("%.1f MiB", float64(bytes)/(1<<20))
}

// notionMigrationFormatsOrderID names a rows' order by what it holds.
func notionMigrationFormatsOrderID(rows []string) string {
	sum := sha256.Sum256([]byte(strings.Join(rows, ",")))
	return hex.EncodeToString(sum[:12])
}
