// What a scanned QR means on a sign-in screen: a signed-in device's
// "<server>/device/<code>" (account_device_codes.go) or the bare code. A
// link to another server is that server's to open.

// Twelve digits in threes of four; a server from before them gave eight
// letters and digits in two.
const CODE = /^(?:\d{4}-?\d{4}-?\d{4}|[0-9A-Za-z]{4}-?[0-9A-Za-z]{4})$/u;
const DEVICE_PATH = /^\/device\/([0-9A-Za-z-]{8,14})\/?$/u;

export type ScannedDeviceCode =
  | { kind: "code"; code: string }
  | { kind: "elsewhere"; url: string }
  | { kind: "none" };

export function deviceCodeFromScan(text: string, serverOrigin: string): ScannedDeviceCode {
  const scanned = text.trim();
  if (CODE.test(scanned)) return { kind: "code", code: scanned.toUpperCase() };
  let url: URL;
  try {
    url = new URL(scanned);
  } catch {
    return { kind: "none" };
  }
  const path = DEVICE_PATH.exec(url.pathname);
  if (!path || (url.protocol !== "https:" && url.protocol !== "http:")) return { kind: "none" };
  if (url.origin === serverOrigin) return { kind: "code", code: (path[1] ?? "").toUpperCase() };
  return { kind: "elsewhere", url: url.href };
}
