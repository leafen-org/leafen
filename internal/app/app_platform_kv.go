//go:build !leafen_noapps

package app

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// An app's volatile store (docs/app-platform.md 9): for each app and
// organization, a small in-memory store taking a fixed set of commands on
// strings, hashes, lists and sorted sets. A write that makes a key names how
// long it lives, within the app's declared maximum; a write past the app's
// declared size is refused; nothing survives a restart. This is the store's
// gateway: what sits behind it can change without the apps noticing.

var (
	errAppKVRefused = errors.New(errorCodeAppKVRefused)
	errAppKVFull    = errors.New(errorCodeAppKVFull)
)

type appKVEntry struct {
	text    string
	hash    map[string]string
	list    []string
	scores  map[string]float64
	expires time.Time
	size    int64
}

type appKVSpace struct {
	mu      sync.Mutex
	entries map[string]*appKVEntry
	size    int64
}

type appKVStore struct {
	mu     sync.Mutex
	spaces map[string]*appKVSpace
}

var appKV = &appKVStore{spaces: map[string]*appKVSpace{}}

func (store *appKVStore) space(installID, tenancy string) *appKVSpace {
	store.mu.Lock()
	defer store.mu.Unlock()
	key := installID + "\x00" + tenancy
	space := store.spaces[key]
	if space == nil {
		space = &appKVSpace{entries: map[string]*appKVEntry{}}
		store.spaces[key] = space
	}
	return space
}

// forget drops what an app keeps, as when it is taken off.
func (store *appKVStore) forget(installID string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for key := range store.spaces {
		if strings.HasPrefix(key, installID+"\x00") {
			delete(store.spaces, key)
		}
	}
}

// sweep drops entries that ran out.
func (store *appKVStore) sweep(now time.Time) {
	store.mu.Lock()
	spaces := make([]*appKVSpace, 0, len(store.spaces))
	for _, space := range store.spaces {
		spaces = append(spaces, space)
	}
	store.mu.Unlock()
	for _, space := range spaces {
		space.mu.Lock()
		for key, entry := range space.entries {
			if !entry.expires.After(now) {
				space.size -= entry.size
				delete(space.entries, key)
			}
		}
		space.mu.Unlock()
	}
}

func appKVEntrySize(key string, entry *appKVEntry) int64 {
	size := int64(len(key) + len(entry.text) + 48)
	for field, value := range entry.hash {
		size += int64(len(field) + len(value) + 16)
	}
	for _, value := range entry.list {
		size += int64(len(value) + 16)
	}
	for member := range entry.scores {
		size += int64(len(member) + 24)
	}
	return size
}

// run carries out one command for the app, bounded by its declared size and
// longest life.
func (space *appKVSpace) run(request appKVRequest, limits appManifestKV, now time.Time) (any, error) {
	command := strings.ToUpper(strings.TrimSpace(request.Command))
	if request.Key == "" || len(request.Key) > 512 {
		return nil, fmt.Errorf("%w: a key up to 512 bytes", errAppKVRefused)
	}
	if request.TTL < 0 || request.TTL > limits.MaxTTLSeconds {
		return nil, fmt.Errorf("%w: ttl is 1 to %d seconds", errAppKVRefused, limits.MaxTTLSeconds)
	}
	space.mu.Lock()
	defer space.mu.Unlock()
	entry := space.entries[request.Key]
	if entry != nil && !entry.expires.After(now) {
		space.size -= entry.size
		delete(space.entries, request.Key)
		entry = nil
	}
	// A write that makes a key names how long it lives; one on a key keeps its time unless it names a new one.
	fresh := false
	create := func(kind string) (*appKVEntry, error) {
		if entry != nil {
			if !appKVKind(entry, kind) {
				return nil, fmt.Errorf("%w: %s holds another kind of value", errAppKVRefused, request.Key)
			}
			if request.TTL > 0 {
				entry.expires = now.Add(time.Duration(request.TTL) * time.Second)
			}
			return entry, nil
		}
		if request.TTL == 0 {
			return nil, fmt.Errorf("%w: a new key needs a ttl", errAppKVRefused)
		}
		made := &appKVEntry{expires: now.Add(time.Duration(request.TTL) * time.Second)}
		switch kind {
		case "hash":
			made.hash = map[string]string{}
		case "list":
			made.list = []string{}
		case "zset":
			made.scores = map[string]float64{}
		}
		space.entries[request.Key] = made
		entry, fresh = made, true
		return made, nil
	}
	settle := func() error {
		if entry == nil {
			return nil
		}
		next := appKVEntrySize(request.Key, entry)
		if space.size-entry.size+next > limits.MaxBytes {
			// A key the write made goes again; the caller puts back what it changed in one that was there.
			if fresh {
				delete(space.entries, request.Key)
			}
			return errAppKVFull
		}
		space.size += next - entry.size
		entry.size = next
		if entry.hash != nil && len(entry.hash) == 0 || entry.list != nil && len(entry.list) == 0 || entry.scores != nil && len(entry.scores) == 0 {
			space.size -= entry.size
			delete(space.entries, request.Key)
		}
		return nil
	}
	value := request.Value
	switch command {
	case "GET":
		if entry == nil || entry.hash != nil || entry.list != nil || entry.scores != nil {
			return nil, nil
		}
		return entry.text, nil
	case "SET":
		if _, err := create("string"); err != nil {
			return nil, err
		}
		previous := *entry
		entry.text = value
		if err := settle(); err != nil {
			*entry = previous
			return nil, err
		}
		return "OK", nil
	case "INCR":
		if _, err := create("string"); err != nil {
			return nil, err
		}
		current, err := strconv.ParseInt(valueOr(entry.text, "0"), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: %s isn't a number", errAppKVRefused, request.Key)
		}
		by := int64(1)
		if request.Score != 0 {
			by = int64(request.Score)
		}
		entry.text = strconv.FormatInt(current+by, 10)
		return current + by, settle()
	case "DEL":
		if entry == nil {
			return 0, nil
		}
		space.size -= entry.size
		delete(space.entries, request.Key)
		return 1, nil
	case "EXPIRE":
		if entry == nil || request.TTL == 0 {
			return 0, nil
		}
		entry.expires = now.Add(time.Duration(request.TTL) * time.Second)
		return 1, nil
	case "TTL":
		if entry == nil {
			return -2, nil
		}
		return int64(math.Ceil(entry.expires.Sub(now).Seconds())), nil
	case "HGET", "HGETALL":
		if entry == nil || entry.hash == nil {
			if command == "HGETALL" {
				return map[string]string{}, nil
			}
			return nil, nil
		}
		if command == "HGETALL" {
			return entry.hash, nil
		}
		found, ok := entry.hash[request.Field]
		if !ok {
			return nil, nil
		}
		return found, nil
	case "HSET":
		if _, err := create("hash"); err != nil {
			return nil, err
		}
		previous, had := entry.hash[request.Field]
		entry.hash[request.Field] = value
		if err := settle(); err != nil {
			if had {
				entry.hash[request.Field] = previous
			} else {
				delete(entry.hash, request.Field)
			}
			return nil, err
		}
		if had {
			return 0, nil
		}
		return 1, nil
	case "HDEL":
		if entry == nil || entry.hash == nil {
			return 0, nil
		}
		if _, ok := entry.hash[request.Field]; !ok {
			return 0, nil
		}
		delete(entry.hash, request.Field)
		return 1, settle()
	case "LPUSH", "RPUSH":
		if _, err := create("list"); err != nil {
			return nil, err
		}
		if command == "LPUSH" {
			entry.list = append([]string{value}, entry.list...)
		} else {
			entry.list = append(entry.list, value)
		}
		if err := settle(); err != nil {
			if command == "LPUSH" {
				entry.list = entry.list[1:]
			} else {
				entry.list = entry.list[:len(entry.list)-1]
			}
			return nil, err
		}
		return len(entry.list), nil
	case "LPOP", "RPOP":
		if entry == nil || len(entry.list) == 0 {
			return nil, nil
		}
		var popped string
		if command == "LPOP" {
			popped, entry.list = entry.list[0], entry.list[1:]
		} else {
			popped, entry.list = entry.list[len(entry.list)-1], entry.list[:len(entry.list)-1]
		}
		return popped, settle()
	case "LRANGE":
		if entry == nil || entry.list == nil {
			return []string{}, nil
		}
		start, stop := appKVRange(request.Start, request.Stop, len(entry.list))
		return append([]string{}, entry.list[start:stop]...), nil
	case "ZADD":
		if _, err := create("zset"); err != nil {
			return nil, err
		}
		_, had := entry.scores[request.Member]
		previous := entry.scores[request.Member]
		entry.scores[request.Member] = request.Score
		if err := settle(); err != nil {
			if had {
				entry.scores[request.Member] = previous
			} else {
				delete(entry.scores, request.Member)
			}
			return nil, err
		}
		if had {
			return 0, nil
		}
		return 1, nil
	case "ZSCORE":
		if entry == nil || entry.scores == nil {
			return nil, nil
		}
		score, ok := entry.scores[request.Member]
		if !ok {
			return nil, nil
		}
		return score, nil
	case "ZREM":
		if entry == nil || entry.scores == nil {
			return 0, nil
		}
		if _, ok := entry.scores[request.Member]; !ok {
			return 0, nil
		}
		delete(entry.scores, request.Member)
		return 1, settle()
	case "ZRANGE":
		if entry == nil || entry.scores == nil {
			return []map[string]any{}, nil
		}
		members := make([]string, 0, len(entry.scores))
		for member := range entry.scores {
			members = append(members, member)
		}
		sort.Slice(members, func(left, right int) bool {
			if entry.scores[members[left]] != entry.scores[members[right]] {
				return entry.scores[members[left]] < entry.scores[members[right]]
			}
			return members[left] < members[right]
		})
		start, stop := appKVRange(request.Start, request.Stop, len(members))
		ranked := []map[string]any{}
		for _, member := range members[start:stop] {
			ranked = append(ranked, map[string]any{"member": member, "score": entry.scores[member]})
		}
		return ranked, nil
	}
	return nil, fmt.Errorf("%w: %s isn't a command the store takes", errAppKVRefused, command)
}

func appKVKind(entry *appKVEntry, kind string) bool {
	switch kind {
	case "hash":
		return entry.hash != nil
	case "list":
		return entry.list != nil
	case "zset":
		return entry.scores != nil
	}
	return entry.hash == nil && entry.list == nil && entry.scores == nil
}

// appKVRange turns Redis-style start and stop, negative from the end and
// stop included, into a slice's bounds.
func appKVRange(start, stop, length int) (int, int) {
	if start < 0 {
		start += length
	}
	if stop < 0 {
		stop += length
	}
	start = max(start, 0)
	stop = min(stop+1, length)
	if start >= stop {
		return 0, 0
	}
	return start, stop
}

func valueOr[T comparable](value, fallback T) T {
	var zero T
	if value == zero {
		return fallback
	}
	return value
}

// A person's calls to an app's store: an endpoint's request makes several, and
// an endpoint takes 600 requests a minute by default.
var appKVLimiter = newAppRelayLimits(6000, 64)

func init() {
	appRemovalCleanups = append(appRemovalCleanups, appKV.forget)
}

// runAppKV runs a store command for an app in one of its organizations.
func runAppKV(manifest appManifest, installID, tenancy, personID string, request appKVRequest, now time.Time) (any, error) {
	if manifest.KV == nil {
		return nil, fmt.Errorf("%w: the app declares no store", errAppKVRefused)
	}
	release, admitted := appKVLimiter.admit(installID, personID, now)
	if !admitted {
		return nil, errAppRelayLimited
	}
	defer release()
	return appKV.space(installID, tenancy).run(request, *manifest.KV, now)
}

// appKVRefusalMessage is what a refused store command says to fix, the
// words runAppKV gave after its code.
func appKVRefusalMessage(err error) string {
	message := err.Error()
	if _, reason, found := strings.Cut(message, ": "); found {
		return reason
	}
	return "The store doesn't take that command."
}
