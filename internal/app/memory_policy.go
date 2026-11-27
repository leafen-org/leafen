package app

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const (
	memoryMiB                  = int64(1024 * 1024)
	memoryGiB                  = int64(1024) * memoryMiB
	memoryAutomaticHostFloor   = int64(4)
	memoryDefaultLimitFloorMiB = int64(2_048)
	memoryOverrideFloorMiB     = int64(1_536)
	memoryOverrideCeilingMiB   = int64(1024 * 1024)
)

var memoryOverridePattern = regexp.MustCompile(`^([1-9][0-9]*)([mMgG])$`)

type selfHostMemoryPolicy struct {
	DockerValue    string `json:"dockerValue"`
	LimitBytes     int64  `json:"limitBytes"`
	NominalHostGiB int64  `json:"nominalHostGiB,omitzero"`
	Source         string `json:"source"`
}

func totalMemoryBytesFromMeminfo(source string) (int64, bool) {
	for _, line := range strings.Split(source, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || !strings.EqualFold(fields[0], "MemTotal:") || !strings.EqualFold(fields[2], "kB") {
			continue
		}
		kibibytes, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || kibibytes < 1 || kibibytes > math.MaxInt64/1024 {
			return 0, false
		}
		return kibibytes * 1024, true
	}
	return 0, false
}

func resolveSelfHostMemoryPolicy(totalMemoryBytes int64, override string) (selfHostMemoryPolicy, error) {
	nominalHostGiB := nominalMemoryGiB(totalMemoryBytes)
	trimmedOverride := strings.TrimSpace(override)
	if trimmedOverride != "" {
		limitMiB, err := parseMemoryOverrideMiB(trimmedOverride)
		if err != nil {
			return selfHostMemoryPolicy{}, err
		}
		return selfHostMemoryPolicy{
			DockerValue:    fmt.Sprintf("%dm", limitMiB),
			LimitBytes:     limitMiB * memoryMiB,
			NominalHostGiB: nominalHostGiB,
			Source:         "override",
		}, nil
	}

	if nominalHostGiB == 0 {
		return selfHostMemoryPolicy{}, errors.New("could not detect host memory; set LEAFEN_MEMORY_LIMIT to one finite value such as 2048m")
	}
	if nominalHostGiB < memoryAutomaticHostFloor {
		return selfHostMemoryPolicy{}, fmt.Errorf(
			"automatic sizing requires at least 4 GiB of host RAM; detected about %d GiB; use a larger host or an explicit 1536m override after confirming system headroom",
			nominalHostGiB,
		)
	}
	limitMiB := max(memoryDefaultLimitFloorMiB, nominalHostGiB*1024/2)
	return selfHostMemoryPolicy{
		DockerValue:    fmt.Sprintf("%dm", limitMiB),
		LimitBytes:     limitMiB * memoryMiB,
		NominalHostGiB: nominalHostGiB,
		Source:         "automatic",
	}, nil
}

func nominalMemoryGiB(totalMemoryBytes int64) int64 {
	if totalMemoryBytes <= 0 {
		return 0
	}
	return (totalMemoryBytes + memoryGiB - 1) / memoryGiB
}

func parseMemoryOverrideMiB(value string) (int64, error) {
	match := memoryOverridePattern.FindStringSubmatch(value)
	if match == nil {
		return 0, errors.New("LEAFEN_MEMORY_LIMIT must use whole MiB or GiB, for example 1536m or 8g")
	}
	quantity, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return 0, errors.New("LEAFEN_MEMORY_LIMIT is outside the supported numeric range")
	}
	limitMiB := quantity
	if strings.EqualFold(match[2], "g") {
		if quantity > math.MaxInt64/1024 {
			return 0, errors.New("LEAFEN_MEMORY_LIMIT is outside the supported numeric range")
		}
		limitMiB = quantity * 1024
	}
	if limitMiB < memoryOverrideFloorMiB || limitMiB > memoryOverrideCeilingMiB {
		return 0, fmt.Errorf(
			"LEAFEN_MEMORY_LIMIT must be between %d MiB and 1 TiB",
			memoryOverrideFloorMiB,
		)
	}
	return limitMiB, nil
}
