// Reading a QR code with the phone app's camera, for connecting to a server
// from the code another device shows (its "Open on your phone"): iOS through
// the app's own scanner (desktop/code_scanner_ios.go), Android through
// LeafenCodeScanner.java, reached by messages only the app's page can send.
// The camera is asked for only when the person chooses to scan.

export type CodeScan =
  | { outcome: "scanned"; text: string }
  // Closed without a code; the camera refused or not there.
  | { outcome: "cancelled" | "denied" | "unavailable" };

type AndroidScanner = {
  postMessage(message: string): void;
  onmessage: ((event: { data: string }) => void) | null;
};

function scanOf(value: unknown): CodeScan {
  const reply = value && typeof value === "object" ? value as { outcome?: unknown; text?: unknown } : {};
  if (reply.outcome === "scanned" && typeof reply.text === "string" && reply.text.trim()) {
    return { outcome: "scanned", text: reply.text.trim() };
  }
  if (reply.outcome === "cancelled" || reply.outcome === "denied") return { outcome: reply.outcome };
  return { outcome: "unavailable" };
}

function androidScan(bridge: AndroidScanner): Promise<CodeScan> {
  const id = `${Date.now()}-${Math.random().toString(36).slice(2)}`;
  return new Promise((resolve) => {
    bridge.onmessage = (event) => {
      try {
        const reply = JSON.parse(event.data) as { id?: string };
        if (reply.id !== id) return;
        bridge.onmessage = null;
        resolve(scanOf(reply));
      } catch {
        // Not an answer to this scan.
      }
    };
    bridge.postMessage(JSON.stringify({ id, op: "scan" }));
  });
}

/** Reads one QR code with the camera; what came of it when there is none. */
export async function scanCode(): Promise<CodeScan> {
  const android = (window as unknown as { leafenCodeScanner?: AndroidScanner }).leafenCodeScanner;
  if (android && typeof android.postMessage === "function") return androidScan(android);
  try {
    const { Call } = await import("@wailsio/runtime");
    return scanOf(await Call.ByName("main.CodeScanner.Scan"));
  } catch {
    return { outcome: "unavailable" };
  }
}
