import { useState } from "react";
import { authWords, type AuthText } from "@/lib/authLine";
import { scanCode } from "@/lib/codeScanner";
import { deviceCodeFromScan } from "@/lib/deviceCodeFromScan";
import { isPackagedClient } from "@/lib/packagedClient";
import { openFoundLink } from "@/lib/packagedLinks";
import { serverOrigin } from "@/lib/serverOrigin";
import type { BrowserQrScanEnd } from "./BrowserQrScanner";

/**
 * "QR 찍기" on the code step: the phone app's own scanner, else the browser's
 * camera, asked for only now; what can't scan leaves typing.
 */
export function useDeviceCodeScan({ setError, onCode }: {
  setError(message: AuthText): void;
  onCode(code: string): void;
}) {
  const [browserScanning, setBrowserScanning] = useState(false);

  async function startScan() {
    setError("");
    if (!isPackagedClient()) {
      setBrowserScanning(true);
      return;
    }
    const scanned = await scanCode();
    if (scanned.outcome === "scanned") takeScanned(scanned.text);
    else if (scanned.outcome !== "cancelled") setError(authWords(scanned.outcome === "denied" ? "authGate:scanDenied" : "authGate:scanUnavailable"));
  }

  function endBrowserScan(reason: BrowserQrScanEnd) {
    setBrowserScanning(false);
    if (reason === "denied") setError(authWords("authGate:scanDenied"));
    else if (reason === "insecure") setError(authWords("authGate:scanNeedsHttps"));
    else if (reason === "unavailable") setError(authWords("authGate:scanUnavailable"));
  }

  function takeScanned(text: string) {
    setBrowserScanning(false);
    const found = deviceCodeFromScan(text, serverOrigin());
    if (found.kind === "code") {
      onCode(found.code);
    } else if (found.kind === "elsewhere") {
      // Another server's code is that server's to open: a browser goes to its
      // link, the app opens it as a link it was opened with — a server it
      // doesn't know yet once the person says so (PackagedLinkOpener).
      if (isPackagedClient()) openFoundLink(found.url);
      else window.location.assign(found.url);
    } else {
      setError(authWords("authGate:scanNoCode"));
    }
  }

  return { browserScanning, startScan, endBrowserScan, takeScanned };
}
