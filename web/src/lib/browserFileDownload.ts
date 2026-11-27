const MAX_BROWSER_FILE_REVOKE_DELAY_MS = 5 * 60 * 1_000;

// Where a packaged app's page saves instead: its window can't download, so
// the app saves the file onto the device itself.
type DeviceSaves = {
  /** Whether the file was saved: false when the person closed the save window. */
  saveFile(file: Blob, name: string): Promise<boolean>;
  saveUrl(url: string, name: string): Promise<boolean>;
};

/**
 * What became of a download: given to the browser, or saved on the device by
 * the app; the save window closed without saving; or the save failed. The
 * caller says it downloaded only when it did (2026-11-07: the app said an
 * audit log was exported while no save window had come and no file existed).
 */
export type FileDownloadResult = "saved" | "cancelled" | "failed";

async function deviceSaveResult(save: Promise<boolean>): Promise<FileDownloadResult> {
  try {
    return await save ? "saved" : "cancelled";
  } catch {
    return "failed";
  }
}

let deviceSaves: DeviceSaves | null = null;

/** Whether this page's downloads are saved by the app that shows it, whose window opens no tab. */
export function savesOnDevice() {
  return deviceSaves !== null;
}

/** Sends this page's downloads to the app that shows it. */
export function saveDownloadsOnDevice(saves: DeviceSaves) {
  deviceSaves = saves;
}

export type BrowserFileDownloadOptions = {
  revokeAfterMs: number;
  cleanup?: () => void | Promise<void>;
};

export function triggerBrowserFileDownload(
  file: Blob,
  name: string,
  options: BrowserFileDownloadOptions,
): Promise<FileDownloadResult> {
  if (
    !Number.isSafeInteger(options.revokeAfterMs)
    || options.revokeAfterMs < 0
    || options.revokeAfterMs > MAX_BROWSER_FILE_REVOKE_DELAY_MS
  ) {
    throw new RangeError("Browser file-download revoke delay is invalid.");
  }
  if (deviceSaves) {
    return deviceSaveResult(deviceSaves.saveFile(file, name)).finally(() => options.cleanup?.());
  }

  const url = URL.createObjectURL(file);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = name;
  anchor.rel = "noopener";
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  window.setTimeout(() => {
    URL.revokeObjectURL(url);
    void options.cleanup?.();
  }, options.revokeAfterMs);
  return Promise.resolve("saved");
}

/**
 * Downloads what an address holds — a file's stored or signed address — the
 * one way every link download goes: the browser's own download, or in the
 * app, whose window can't download, saved on the device.
 */
export function triggerBrowserLinkDownload(url: string, name: string): Promise<FileDownloadResult> {
  if (deviceSaves && /^(https?:|\/)/u.test(url)) return deviceSaveResult(deviceSaves.saveUrl(new URL(url, window.location.href).href, name));
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = name;
  anchor.rel = "noreferrer noopener";
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  return Promise.resolve("saved");
}

/** The server's attachment filename takes precedence over the anchor's
 * download attribute. Send the current display name without buffering bytes. */
export function withFileDownloadName(url: string, name: string) {
  return name ? `${url}${url.includes("?") ? "&" : "?"}filename=${encodeURIComponent(name)}` : url;
}

export function triggerBrowserURLDownload(url: string, name: string): Promise<FileDownloadResult> {
  // A packaged app's page has the capability at its server's address.
  if (deviceSaves && /^https?:\/\/[^/?#]+\/api\/leafen\/v1\/file-capabilities\/[A-Za-z0-9_-]+$/u.test(url)) {
    return deviceSaveResult(deviceSaves.saveUrl(url, name));
  }
  if (!/^\/api\/leafen\/v1\/file-capabilities\/[A-Za-z0-9_-]+$/u.test(url)) {
    throw new RangeError("Browser file-download capability URL is invalid.");
  }
  const anchor = document.createElement("a");
  anchor.href = withFileDownloadName(url, name);
  anchor.download = name;
  anchor.rel = "noopener";
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  return Promise.resolve("saved");
}
