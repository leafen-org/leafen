package app

import (
	"os"
	"strings"
	"sync"
)

// The memory the server plans for (2026-10-18: a long run on a 64 GB
// machine grew to 5.2 GB in caches sized for any machine — a 32 MB page
// cache for each of about fifty connections and a 2 GB memory map), in one
// place: what its container allows, else LEAFEN_MEMORY_LIMIT, else half of
// this machine's memory (at least 2 GiB, as the Docker hint gives), else
// 1.5 GiB. SQLite's page cache, memory map and lookaside, the data
// connections, the Go runtime's soft limit and photo previews are sized
// from it: a large machine keeps what it had, a small NAS takes less.

const (
	// Without any other answer, the memory a self-hosted server is given by
	// default.
	serverMemoryDefaultLimit = 1536 * memoryMiB
	// Under this, the data database keeps half the connections: a server
	// this small serves far fewer people at once than the 300 the pools
	// were sized for (2026-10-14).
	serverMemorySmall = 2 * memoryGiB
	// Each data connection's page cache is a share of a quarter of the
	// memory, between these.
	sqlitePageCacheMinKiB = 2 * 1024
	sqlitePageCacheMaxKiB = 32_000
	// SQLite's largest memory map, its first 2 GB (SQLITE_MAX_MMAP_SIZE in
	// the build).
	sqliteMemoryMapMax = 2147418112
)

// serverMemoryLimit is the memory the server plans for and where that came
// from, settled once.
var serverMemoryLimit = sync.OnceValues(func() (int64, string) {
	root := strings.TrimSpace(os.Getenv(cgroupRootEnvironment))
	if root == "" {
		root = defaultCgroupRoot
	}
	if snapshot, err := readMemoryCgroupSnapshot(root); err == nil && snapshot.LimitBytes > 0 {
		return snapshot.LimitBytes, "the container"
	}
	if raw := strings.TrimSpace(os.Getenv(memoryLimitEnvironment)); raw != "" {
		if mebibytes, err := parseMemoryOverrideMiB(raw); err == nil {
			return mebibytes * memoryMiB, memoryLimitEnvironment
		}
	}
	if total, ok := hostMemoryBytes(); ok && total > 0 {
		return max(memoryDefaultLimitFloorMiB*memoryMiB, total/2), "half of this machine's memory"
	}
	return serverMemoryDefaultLimit, "the default"
})

// serverMemoryKnown says whether the limit came from what the server could
// learn rather than the default.
func serverMemoryKnown() bool {
	_, source := serverMemoryLimit()
	return source != "the default"
}

// serverMemoryPlan is what the plan gives, as the doctor command and the
// start show it.
func serverMemoryPlan() map[string]any {
	limit, source := serverMemoryLimit()
	return map[string]any{"limitMiB": limit / memoryMiB, "from": source, "pageCacheKiB": sqlitePageCacheKiB(),
		"memoryMapMiB": sqliteMemoryMap() / memoryMiB, "dataConnections": dataReadConnections(),
		"snapshotConnections": readSnapshotConnections()}
}

// dataReadConnections is how many read connections the data database
// keeps (app.go); readSnapshotConnections the read snapshots' (read_snapshot.go).
func dataReadConnections() int {
	if limit, _ := serverMemoryLimit(); limit < serverMemorySmall {
		return workspaceDataConnections / 2
	}
	return workspaceDataConnections
}

func readSnapshotConnections() int {
	if limit, _ := serverMemoryLimit(); limit < serverMemorySmall {
		return readSnapshotPoolSize / 2
	}
	return readSnapshotPoolSize
}

// sqliteConnections is every connection the server may open at once: the
// data's read and write connections, the read snapshots, PocketBase's log
// database, and a few more (the replica, imports).
func sqliteConnections() int {
	return dataReadConnections() + 1 + readSnapshotConnections() + 20 + 1 + 4
}

// sqlitePageCacheKiB is each data connection's page cache, in KiB.
func sqlitePageCacheKiB() int64 {
	limit, _ := serverMemoryLimit()
	return min(max(limit/4/int64(sqliteConnections())/1024, sqlitePageCacheMinKiB), sqlitePageCacheMaxKiB)
}

// sqliteMemoryMap is the bytes of the data database read through a memory
// map: as much as SQLite maps, whatever the memory (2026-10-18: a quarter of
// the memory left a 3.4 GB database's reads beyond 384 MiB to a read call
// each, 17.5% of the CPU, and widening it halved the reads' p95). The map
// holds no memory of its own: its pages are the system's file cache, the
// same a read call fills, reclaimed the same way, and on the 64-bit systems
// Leafen runs on the address space is plenty. A 2.5 GB backup in a 1 GiB
// container read with it as without it.
func sqliteMemoryMap() int64 {
	return sqliteMemoryMapMax
}
