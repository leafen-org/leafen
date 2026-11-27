package main

// CodeScanner reads a QR code with the phone's camera for the window: the
// server address another device shows ("Open on your phone"), to connect to
// it at the app's first start. The camera is asked for only when the person
// chooses to scan. A computer has no scanner here (it shows the code), and
// Android's window reaches its own (LeafenCodeScanner.java).
type CodeScanner struct{}

// CodeScan is what came of a scan: "scanned" with the code's text, or
// "cancelled", "denied" (the camera refused) or "unavailable" (no camera).
type CodeScan struct {
	Outcome string `json:"outcome"`
	Text    string `json:"text"`
}

func newCodeScanner() *CodeScanner { return &CodeScanner{} }

// Scan shows the camera until it reads a QR code or the person closes it.
func (*CodeScanner) Scan() CodeScan { return scanCode() }
