//go:build windows

package main

import (
	"encoding/binary"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// localGraphics is the memory of this computer's largest graphics card, as
// its driver records it, and whether Vulkan, which the engine's graphics
// build runs on, came with the drivers.
func localGraphics() (memoryMB int, vulkan bool) {
	if _, err := os.Stat(filepath.Join(os.Getenv("SystemRoot"), "System32", "vulkan-1.dll")); err == nil {
		vulkan = true
	}
	// The display adapters' class.
	adapters, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}`, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return 0, vulkan
	}
	defer adapters.Close()
	names, _ := adapters.ReadSubKeyNames(-1)
	for _, name := range names {
		adapter, err := registry.OpenKey(adapters, name, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		bytes := uint64(0)
		if value, _, err := adapter.GetIntegerValue("HardwareInformation.qwMemorySize"); err == nil {
			bytes = value
		} else if value, _, err := adapter.GetIntegerValue("HardwareInformation.MemorySize"); err == nil {
			bytes = value
		} else if data, _, err := adapter.GetBinaryValue("HardwareInformation.MemorySize"); err == nil && len(data) >= 4 {
			bytes = uint64(binary.LittleEndian.Uint32(data))
		}
		adapter.Close()
		memoryMB = max(memoryMB, int(bytes>>20))
	}
	return memoryMB, vulkan
}
