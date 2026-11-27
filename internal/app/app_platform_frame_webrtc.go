//go:build !leafen_noapps

package app

import (
	"bytes"
	"regexp"
)

// WebRTC for apps that didn't declare it (docs/app-platform.md 9): browsers
// have no policy that turns peer connections off in a frame, and a peer
// connection reaches past the frame's connect-src. So Leafen's own script
// runs first in each of such an app's pages: it removes them, and frames
// of the page's own making (whose code would have them) — frames the app
// makes are of another origin to it in its sandbox. An app that declares
// realtime.webrtc (approved with the app) keeps them and signals through
// its rooms.

const appFrameNoWebRTCPath = "ui/.leafen/no-webrtc.js"

var appFrameNoWebRTCScript = []byte(`// Leafen: this app didn't declare WebRTC, so its pages have none.
(() => {
  const names = ["RTCPeerConnection", "webkitRTCPeerConnection", "RTCDataChannel", "RTCRtpSender", "RTCRtpReceiver",
    "RTCRtpTransceiver", "RTCIceCandidate", "RTCSessionDescription", "RTCDtlsTransport", "RTCIceTransport", "RTCSctpTransport"];
  for (const name of names) {
    try { Object.defineProperty(window, name, { value: undefined, writable: false, configurable: false }); } catch {}
  }
  // A frame of the page's own making (srcdoc, blank, data or blob) would run
  // the app's code with peer connections; frames that load an address stay —
  // the app's own pages among them, which run this script too.
  const local = (frame) => frame.hasAttribute("srcdoc") || !/^https?:/i.test(frame.src || frame.data || "");
  const watch = (node) => {
    if (node instanceof HTMLIFrameElement || node instanceof HTMLObjectElement || node instanceof HTMLEmbedElement) {
      if (local(node)) node.remove();
      return;
    }
    if (node.querySelectorAll) node.querySelectorAll("iframe, object, embed").forEach(watch);
  };
  new MutationObserver((records) => records.forEach((record) => {
    if (record.type === "attributes") watch(record.target);
    else record.addedNodes.forEach(watch);
  })).observe(document, { childList: true, subtree: true, attributes: true, attributeFilter: ["src", "srcdoc", "data"] });
})();
`)

var appFrameHeadTag = regexp.MustCompile(`(?i)<head(\s[^>]*)?>`)
var appFrameDoctype = regexp.MustCompile(`(?i)^\s*<!doctype[^>]*>`)

// appFrameWithoutWebRTC puts Leafen's script first in an app's page.
func appFrameWithoutWebRTC(page []byte, ticket string) []byte {
	tag := []byte(`<script src="` + appFramePrefix + ticket + "/" + appFrameNoWebRTCPath + `"></script>`)
	if place := appFrameHeadTag.FindIndex(page); place != nil {
		return append(append(append([]byte{}, page[:place[1]]...), tag...), page[place[1]:]...)
	}
	if place := appFrameDoctype.FindIndex(page); place != nil {
		return append(append(append([]byte{}, page[:place[1]]...), tag...), page[place[1]:]...)
	}
	return append(append([]byte{}, tag...), page...)
}

// appFrameHTML says the frame file is a page.
func appFrameHTML(contentType string) bool {
	return bytes.HasPrefix(bytes.ToLower([]byte(contentType)), []byte("text/html"))
}
