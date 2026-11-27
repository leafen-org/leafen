//go:build ios || android

package main

import "github.com/pocketbase/pocketbase/core"

// A phone's window shows one page, which opens afresh where it can after a
// restore (web/src/lib/restoredPlace.ts).
func forgetGoneTabPages(core.App) {}

func forgetGoneTabPagesWhenUp(*localServer) {}
