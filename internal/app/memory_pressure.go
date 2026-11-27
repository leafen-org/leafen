package app

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

const (
	cgroupRootEnvironment            = "LEAFEN_CGROUP_ROOT"
	containerRuntimeEnvironment      = "LEAFEN_CONTAINER_RUNTIME"
	defaultCgroupRoot                = "/sys/fs/cgroup"
	memorySampleInterval             = 250 * time.Millisecond
	memoryPressureRetrySeconds       = 1
	memoryHighPercent                = int64(80)
	memoryRecoveryPercent            = int64(75)
	memoryMinimumHeadroom            = int64(256) * memoryMiB
	memoryCriticalPercent            = int64(95)
	memoryCriticalHeadroom           = int64(64) * memoryMiB
	memoryUnlimitedThreshold         = int64(1) << 60
	errorCodeMemoryPressure          = "LEAFEN_MEMORY_PRESSURE"
	errorCodeMemorySensorUnavailable = "LEAFEN_MEMORY_SENSOR_UNAVAILABLE"
	memoryPressureHandlerID          = "leafenMemoryPressure"
)

var errMemoryPressure = errors.New("native memory pressure admission closed")

type memorySnapshot struct {
	Version         int
	RawBytes        int64
	WorkingSetBytes int64
	LimitBytes      int64
}

type memoryAdmissionDecision struct {
	Allowed           bool
	RetryAfterSeconds int
	denial            memoryAdmissionDenial
	cause             error
}

type memoryAdmissionDenial string

const (
	memoryAdmissionPressure          memoryAdmissionDenial = "pressure"
	memoryAdmissionSensorUnavailable memoryAdmissionDenial = "sensor-unavailable"
)

type memoryPressureController struct {
	mu            sync.Mutex
	sample        func() (memorySnapshot, error)
	now           func() time.Time
	lastSampleAt  time.Time
	cached        memoryAdmissionDecision
	underPressure bool
	// Past the high mark the long work gives way (memory_pressure_long_work.go);
	// past the critical one people's requests wait too, the last guard
	// before the container's limit ends the server.
	critical        bool
	longWorkRunning int
	longWorkEnded   time.Time
	freedAt         time.Time
	sampleFailed    error
}

func registerMemoryPressure(event *core.ServeEvent) error {
	root := strings.TrimSpace(os.Getenv(cgroupRootEnvironment))
	if root == "" {
		root = defaultCgroupRoot
	}
	sample := func() (memorySnapshot, error) {
		return readMemoryCgroupSnapshot(root)
	}
	initial, err := sample()
	if err != nil {
		// A container started without a memory limit (a plain `docker run`)
		// plans as a machine does — LEAFEN_MEMORY_LIMIT, else half of the
		// machine's memory (memory_plan.go) — and holds requests back by its
		// cgroup's use against that plan.
		if planned, source := serverMemoryLimit(); planned > 0 && strings.TrimSpace(os.Getenv(containerRuntimeEnvironment)) == "1" {
			withinPlan := func() (memorySnapshot, error) { return readMemoryCgroupUseWithin(root, planned) }
			if snapshot, useErr := withinPlan(); useErr == nil {
				sample, initial, err = withinPlan, snapshot, nil
				fmt.Fprintf(os.Stderr, "leafen: the container has no memory limit; planning for %d MiB from %s\n", planned/memoryMiB, source)
			}
		}
	}
	if err != nil {
		// Outside a container the Go runtime still keeps to the memory the
		// server plans for (memory_plan.go); there is no reading of the
		// machine's use to hold requests back with.
		if serverMemoryKnown() {
			limit, _ := serverMemoryLimit()
			debug.SetMemoryLimit(memoryHighThreshold(limit))
		}
		return nil
	}

	debug.SetMemoryLimit(memoryHighThreshold(initial.LimitBytes))
	controller := newMemoryPressureController(sample, time.Now)
	serverMemoryPressure.Store(controller)
	event.Router.Bind(&hook.Handler[*core.RequestEvent]{
		Id:       memoryPressureHandlerID,
		Priority: apis.DefaultRateLimitMiddlewarePriority - 20,
		Func: func(request *core.RequestEvent) error {
			decision := controller.admit()
			if decision.Allowed {
				return request.Next()
			}
			request.Response.Header().Set("Retry-After", strconv.Itoa(decision.RetryAfterSeconds))
			if decision.denial == memoryAdmissionSensorUnavailable {
				return apiErrorWithCause(
					request,
					http.StatusServiceUnavailable,
					errorCodeMemorySensorUnavailable,
					"The server couldn't check its memory state, so the request didn't start. Try again shortly.",
					diagnosticOperation(request),
					"admission",
					"MEMORY_SENSOR_UNAVAILABLE",
					decision.cause,
				)
			}
			return apiErrorWithCause(
				request,
				http.StatusTooManyRequests,
				errorCodeMemoryPressure,
				"The server is stabilizing its memory use. Try again shortly.",
				diagnosticOperation(request),
				"admission",
				"MEMORY_PRESSURE",
				decision.cause,
			)
		},
	})
	return nil
}

func newMemoryPressureController(
	sample func() (memorySnapshot, error),
	now func() time.Time,
) *memoryPressureController {
	return &memoryPressureController{
		sample: sample,
		now:    now,
		cached: memoryAdmissionDecision{
			Allowed: true,
		},
	}
}

// admit is whether a person's request goes in. Under memory pressure it
// does: the long work gives way first (memory_pressure_long_work.go), and
// people's edits and reads go on (2026-11-23: two Notion moves at once
// took a 2 GiB NAS past its high mark, and for minutes every request but
// theirs was refused — a guest's row sent to the trash came back "couldn't
// move" — while the moves went on). Only past the critical mark, close to
// the container's limit, do people's requests wait too.
func (controller *memoryPressureController) admit() memoryAdmissionDecision {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.refresh()
	return controller.cached
}

// refresh reads the memory again when the last reading is old, and moves
// between the marks: into pressure at the high mark and out of it at the
// recovery mark below, into the critical state at the critical mark and out
// of it a little below (2026-11-23: left only back under the high mark, a
// server whose memory the long work no longer moved stayed refusing people
// for good, the long work waiting too).
func (controller *memoryPressureController) refresh() {
	now := controller.now()
	if !controller.lastSampleAt.IsZero() && now.Sub(controller.lastSampleAt) < memorySampleInterval {
		return
	}
	controller.lastSampleAt = now
	snapshot, err := controller.sample()
	controller.sampleFailed = err
	if err != nil {
		controller.cached = memoryAdmissionDecision{
			Allowed:           false,
			RetryAfterSeconds: memoryPressureRetrySeconds,
			denial:            memoryAdmissionSensorUnavailable,
			cause:             err,
		}
		return
	}
	limit, used := snapshot.LimitBytes, snapshot.WorkingSetBytes
	if controller.underPressure {
		if used <= memoryRecoveryThreshold(limit) {
			controller.underPressure = false
		}
	} else if used >= memoryHighThreshold(limit) {
		controller.underPressure = true
		controller.freeHeld(now)
	}
	if controller.critical {
		controller.critical = used > memoryCriticalRecoveryThreshold(limit)
	} else {
		controller.critical = used >= memoryCriticalThreshold(limit)
	}
	if controller.critical {
		// Everything waits then: what the long work held goes back.
		controller.freeHeld(now)
	}
	controller.cached = memoryAdmissionDecision{
		Allowed:           !controller.critical,
		RetryAfterSeconds: retryAfterForPressure(controller.critical),
		denial:            denialForPressure(controller.critical),
		cause:             causeForPressure(controller.critical),
	}
}

func denialForPressure(underPressure bool) memoryAdmissionDenial {
	if underPressure {
		return memoryAdmissionPressure
	}
	return ""
}

func causeForPressure(underPressure bool) error {
	if underPressure {
		return errMemoryPressure
	}
	return nil
}

func retryAfterForPressure(underPressure bool) int {
	if underPressure {
		return memoryPressureRetrySeconds
	}
	return 0
}

func memoryHighThreshold(limit int64) int64 {
	byRatio := limit * memoryHighPercent / 100
	byHeadroom := limit - memoryMinimumHeadroom
	if byHeadroom < byRatio {
		return max(byHeadroom, int64(1))
	}
	return byRatio
}

// memoryCriticalThreshold is the mark close to the limit past which people's
// requests wait too: 95%, or the limit less 64 MiB on a small one, and never
// under the high mark.
func memoryCriticalThreshold(limit int64) int64 {
	return max(min(limit*memoryCriticalPercent/100, limit-memoryCriticalHeadroom), memoryHighThreshold(limit))
}

// memoryCriticalRecoveryThreshold is the mark people's requests go in again
// below, past the critical one: half the gap between the high and recovery
// marks under it, and never under the high mark.
func memoryCriticalRecoveryThreshold(limit int64) int64 {
	gap := limit * (memoryHighPercent - memoryRecoveryPercent) / 200
	return max(memoryCriticalThreshold(limit)-gap, memoryHighThreshold(limit))
}

func memoryRecoveryThreshold(limit int64) int64 {
	byRatio := limit * memoryRecoveryPercent / 100
	high := memoryHighThreshold(limit)
	gap := limit * (memoryHighPercent - memoryRecoveryPercent) / 100
	if high-gap < byRatio {
		return max(high-gap, int64(0))
	}
	return byRatio
}

func readMemoryCgroupSnapshot(root string) (memorySnapshot, error) {
	if files, ok := readMemoryCgroupFiles(root, true); ok {
		return memorySnapshotFromCgroupFiles(files)
	}
	for _, candidate := range []string{root, filepath.Join(root, "memory")} {
		if files, ok := readMemoryCgroupFiles(candidate, false); ok {
			return memorySnapshotFromCgroupFiles(files)
		}
	}
	return memorySnapshot{}, errors.New("finite cgroup memory files are unavailable")
}

// readMemoryCgroupUseWithin reads a cgroup's use as readMemoryCgroupSnapshot
// does, against the given limit in place of its own unlimited one.
func readMemoryCgroupUseWithin(root string, limit int64) (memorySnapshot, error) {
	within := strconv.FormatInt(limit, 10)
	if files, ok := readMemoryCgroupFiles(root, true); ok {
		files["memory.max"] = within
		return memorySnapshotFromCgroupFiles(files)
	}
	for _, candidate := range []string{root, filepath.Join(root, "memory")} {
		if files, ok := readMemoryCgroupFiles(candidate, false); ok {
			files["memory.limit_in_bytes"] = within
			return memorySnapshotFromCgroupFiles(files)
		}
	}
	return memorySnapshot{}, errors.New("cgroup memory use is unreadable")
}

func readMemoryCgroupFiles(root string, versionTwo bool) (map[string]string, bool) {
	names := []string{"memory.usage_in_bytes", "memory.limit_in_bytes", "memory.stat"}
	if versionTwo {
		names = []string{"memory.current", "memory.max", "memory.stat"}
	}
	files := make(map[string]string, len(names))
	for _, name := range names {
		content, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return nil, false
		}
		files[name] = string(content)
	}
	return files, true
}

func memorySnapshotFromCgroupFiles(files map[string]string) (memorySnapshot, error) {
	if _, ok := files["memory.current"]; ok {
		return parseMemorySnapshot(
			2,
			files["memory.current"],
			files["memory.max"],
			files["memory.stat"],
			"inactive_file",
		)
	}
	if _, ok := files["memory.usage_in_bytes"]; ok {
		return parseMemorySnapshot(
			1,
			files["memory.usage_in_bytes"],
			files["memory.limit_in_bytes"],
			files["memory.stat"],
			"total_inactive_file",
		)
	}
	return memorySnapshot{}, errors.New("unsupported cgroup memory file set")
}

func parseMemorySnapshot(
	version int,
	rawValue string,
	limitValue string,
	statValue string,
	inactiveKey string,
) (memorySnapshot, error) {
	rawBytes, err := parseNonnegativeMemoryValue(rawValue)
	if err != nil {
		return memorySnapshot{}, fmt.Errorf("invalid cgroup memory usage: %w", err)
	}
	limitBytes, err := parseFiniteMemoryLimit(limitValue)
	if err != nil {
		return memorySnapshot{}, err
	}
	inactiveBytes, ok := memoryStatValue(statValue, inactiveKey)
	workingSetBytes := rawBytes
	if ok {
		workingSetBytes = max(rawBytes-inactiveBytes, int64(0))
	}
	return memorySnapshot{
		Version:         version,
		RawBytes:        rawBytes,
		WorkingSetBytes: workingSetBytes,
		LimitBytes:      limitBytes,
	}, nil
}

func parseNonnegativeMemoryValue(value string) (int64, error) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed < 0 {
		return 0, errors.New("memory value must be a nonnegative integer")
	}
	return parsed, nil
}

func parseFiniteMemoryLimit(value string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.EqualFold(trimmed, "max") {
		return 0, errors.New("cgroup memory limit is unlimited")
	}
	parsed, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || parsed < 1 || parsed >= memoryUnlimitedThreshold {
		return 0, errors.New("cgroup memory limit is not finite")
	}
	return parsed, nil
}

func memoryStatValue(source string, key string) (int64, bool) {
	for _, line := range strings.Split(source, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != key {
			continue
		}
		value, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || value < 0 {
			return 0, false
		}
		return value, true
	}
	return 0, false
}
