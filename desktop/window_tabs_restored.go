//go:build (darwin && !ios) || windows

package main

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
	"github.com/wailsapp/wails/v3/pkg/application"

	"leafen.org/leafen/internal/app"
)

// After a restore the app's own server holds an earlier history: a tab kept
// on a page the restore took away opens home, where the workspace's start
// page opens what is still there, rather than on a page not found
// (2026-11-21: the window came back on "페이지 찾지 못함"). The tabs whose
// page isn't up — brought back from the last time, or put away — take home
// as their place now; a tab whose page is up learns of the restore itself
// and opens afresh where it can (web/src/lib/restoredPlace.ts). Tabs on
// another server keep theirs. It looks once the app's own server answers
// with data restored as it started — again after "지금 다시 시작", or at the
// app's start after a restore that waited for it.
func forgetGoneTabPages(product core.App) {
	if !app.DataRestoredAtStart(product) {
		return
	}
	type asked struct {
		tab  *appTab
		page string
	}
	var tabs []asked
	application.InvokeSync(func() {
		for _, tab := range desktopTabs.allTabs() {
			if page := pageOfPath(tab.path); page != "" && tab.window == nil && ownServerTab(tab) {
				tabs = append(tabs, asked{tab: tab, page: page})
			}
		}
	})
	if len(tabs) == 0 {
		return
	}
	ids := make([]string, 0, len(tabs))
	for _, each := range tabs {
		ids = append(ids, each.page)
	}
	gone, err := app.PagesGone(product, ids)
	if err != nil || len(gone) == 0 {
		return
	}
	onMain(func() {
		t := desktopTabs
		for _, each := range tabs {
			if gone[each.page] && each.tab.window == nil && pageOfPath(each.tab.path) == each.page {
				each.tab.path, each.tab.title, each.tab.icon, each.tab.said = "/", "", "", tabSaid{}
			}
		}
		for _, frame := range t.frames {
			t.drawStrip(frame)
		}
		t.keep()
	})
}

// forgetGoneTabPagesWhenUp looks once the app's own server first answers.
func forgetGoneTabPagesWhenUp(local *localServer) {
	ready, _, _, _ := local.current()
	<-ready
	local.mu.RLock()
	err, product := local.err, local.product
	local.mu.RUnlock()
	if err == nil && product != nil {
		forgetGoneTabPages(product)
	}
}

// ownServerTab is a tab on the app's own server: one that kept no sign-in,
// as a new tab's, or the own server's ("local", web/src/lib/profileRegistry.ts).
func ownServerTab(tab *appTab) bool {
	if tab.signIn == "" {
		return true
	}
	signIn := struct {
		Profile string `json:"profile"`
	}{}
	return json.Unmarshal([]byte(tab.signIn), &signIn) == nil && signIn.Profile == "local"
}
