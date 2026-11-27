import { isPackagedClient } from "./packagedClient";
import { activeProfileId, LOCAL_PROFILE_ID } from "./profileRegistry";

// A folder picked with the system's dialog, for the app's own server: in the
// app on its local profile that server runs on this device, so a folder
// picked here is one it can write. A phone's own server ("이 폰에서 써
// 보기") reaches its folders through the phone's document providers, not as
// paths: the app names one by a string of its own ("ios-folder:" or
// "android-folder:", the shown name, then what opens it —
// desktop/backup_folders_*.go), so there a folder is only picked, and shown
// by its name. Another server's folders are typed as that server names them.

/** Whether this window can ask for a folder its server can write. */
export function canChooseServerFolder(): boolean {
  return isPackagedClient() && activeProfileId() === LOCAL_PROFILE_ID;
}

/** Whether this window is a phone's or tablet's app (an iPad's reads as a Mac's but takes touches). */
function phoneApp(): boolean {
  const agent = navigator.userAgent;
  return /Android|iPhone|iPad|iPod/u.test(agent) || (/Macintosh/u.test(agent) && navigator.maxTouchPoints > 1);
}

/** Whether the server is the phone app's own, whose folders can only be picked. */
export function pickedFoldersOnly(): boolean {
  return canChooseServerFolder() && phoneApp();
}

/** Whether the server is the desktop app's own, on this computer. */
export function serverOnThisComputer(): boolean {
  return canChooseServerFolder() && !phoneApp();
}

/** A phone folder's shown name, or null for a path. */
export function deviceFolderName(folder: string): string | null {
  const match = /^(?:ios|android)-folder:([^:]*):/u.exec(folder);
  if (!match) return null;
  try {
    return decodeURIComponent(match[1].replace(/\+/gu, " "));
  } catch {
    return match[1];
  }
}

/** The folder the person picked, or null when they cancelled. */
export async function chooseServerFolder(title: string): Promise<string | null> {
  const { Call } = await import("@wailsio/runtime");
  const folder: unknown = await Call.ByName("main.FolderChooser.Choose", title);
  return typeof folder === "string" && folder !== "" ? folder : null;
}

/** A cloud drive's folder the app found on this computer. */
export type SyncFolder = {
  service: "google-drive" | "icloud-drive" | "dropbox" | "onedrive";
  account: string;
  path: string;
};

const SYNC_SERVICES = new Set<string>(["google-drive", "icloud-drive", "dropbox", "onedrive"]);

/**
 * The cloud drives' folders on this computer, which the backup settings
 * offer first; none in a browser, on a phone (its picker shows its drives)
 * or for another server.
 */
export async function listSyncFolders(): Promise<SyncFolder[]> {
  if (!serverOnThisComputer()) return [];
  const { Call } = await import("@wailsio/runtime");
  const folders: unknown = await Call.ByName("main.FolderChooser.SyncFolders").catch(() => []);
  if (!Array.isArray(folders)) return [];
  return folders.filter((folder): folder is SyncFolder =>
    typeof folder === "object" && folder !== null && SYNC_SERVICES.has(String(folder.service)) &&
    typeof folder.account === "string" && typeof folder.path === "string" && folder.path !== "");
}
