//go:build darwin && !ios

package main

// localGraphics: a Mac's engine runs on its Apple chip's shared memory
// (localGPU) or, on an Intel Mac, its CPU, so a card isn't asked about.
func localGraphics() (memoryMB int, vulkan bool) { return 0, false }
