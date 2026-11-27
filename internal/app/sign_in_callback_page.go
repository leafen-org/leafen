package app

import (
	"encoding/json"
	"html"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// signInCallbackPage is the page an outside sign-in comes back to. It says
// how the sign-in went on the channel Leafen's windows of this address hear
// (web/src/lib/signInWindow.ts), and to the window that opened it where the
// browser left one — Leafen's pages keep Cross-Origin-Opener-Policy:
// same-origin, so a window that went to another site comes back with none.
// Answered by a Leafen window waiting for it, or opened by one, it closes.
// Otherwise — a tab, or Leafen's own where a browser or an app's web view
// opens no windows — it stays and links back to Leafen, since closing would
// close Leafen; so it does too when the browser won't let it close.
type signInCallbackPage struct {
	// Done and Failed are the sentence the person reads, English and Korean.
	Done, Failed [2]string
	// Result is what the opening window is told; AnyOrigin tells it whatever
	// origin the opener has, rather than only this server's.
	Result    map[string]any
	AnyOrigin bool
	// Back is the Leafen address the page links to.
	Back string
	// Code and Call are what a program's call, which no sign-in sends back,
	// hears in its place when the sign-in didn't go (api_page_refusals.go).
	Code, Call string
}

func (page signInCallbackPage) write(event *core.RequestEvent, ok bool) error {
	language := 0
	if strings.HasPrefix(strings.ToLower(event.Request.Header.Get("Accept-Language")), "ko") {
		language = 1
	}
	message := page.Done[language]
	if !ok {
		message = page.Failed[language]
	}
	closing := [2]string{"This window closes shortly.", "이 창은 곧 닫힙니다."}[language]
	back := [2]string{"Back to Leafen", "리펜으로 돌아가기"}[language]
	target := "location.origin"
	if page.AnyOrigin {
		target = `"*"`
	}
	result, _ := json.Marshal(page.Result)
	nonce, word := security.RandomString(24), security.RandomString(16)
	body := `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Leafen</title>` +
		`<style nonce="` + nonce + `">body{margin:64px auto;max-width:360px;padding:0 16px;font:14px/1.5 -apple-system,system-ui,sans-serif;color:#37352f;background:#fff}` +
		`a{color:#2383e2}@media (prefers-color-scheme:dark){body{color:#d4d4d4;background:#191919}a{color:#529cca}}</style></head><body>` +
		`<p>` + html.EscapeString(message) + ` <span id="closing" hidden>` + html.EscapeString(closing) + `</span></p>` +
		`<p id="back"><a href="` + html.EscapeString(page.Back) + `">` + html.EscapeString(back) + `</a></p>` +
		`<script nonce="` + nonce + `">(function(){var result=` + string(result) + `,page="` + word + `",closing=document.getElementById("closing"),` +
		`back=document.getElementById("back"),going=false;function closeSoon(){if(going)return;going=true;closing.hidden=false;back.hidden=true;` +
		`setTimeout(function(){window.close();setTimeout(function(){closing.hidden=true;back.hidden=false},500)},1200)}` +
		`if(window.opener){try{window.opener.postMessage(result,` + target + `)}catch(e){}closeSoon()}` +
		`try{var channel=new BroadcastChannel("leafen:sign-in");channel.onmessage=function(event){var seen=event.data;` +
		`if(seen&&seen.type==="leafen:sign-in-seen"&&seen.page===page)closeSoon()};result.page=page;channel.postMessage(result)}catch(e){}})()</script></body></html>`
	answer := func(status int) error {
		header := event.Response.Header()
		header.Set("Content-Type", "text/html; charset=utf-8")
		header.Set("Cache-Control", "no-store")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("Content-Security-Policy", "default-src 'none'; style-src 'nonce-"+nonce+"'; script-src 'nonce-"+nonce+
			"'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		return event.HTML(status, body)
	}
	if ok {
		return answer(http.StatusOK)
	}
	return apiPageRefusal(event, http.StatusBadRequest, page.Code, page.Call, func() error { return answer(http.StatusBadRequest) },
		apiErrorField{Name: "?state", Problem: "not one of a login Leafen started"})
}
