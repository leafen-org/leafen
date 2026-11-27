import type { ReactNode } from "react";
import { BuiltInIconGlyph, IconImage } from "@/components/page/PageIcon";
import { parseNativeIconValue, storedIconType } from "@/components/page/nativeIcons";
import { safeExternalPresentationImage } from "@/components/page/pagePresentation";
import { filePreviewWidth, useWorkspaceFile } from "@/lib/fileUrls";
import type { WorkspaceFileTarget } from "@/lib/storage";
import { managedFileValueLocator, nativeWorkspaceFileIdFromLocator } from "@/lib/workspaceFileTransport";
import type { Teamspace } from "@/teamspaceWire.generated";
import styles from "./KeptIconGlyph.module.css";

/**
 * An icon kept as a bare string — a teamspace's or a group's, as a
 * template's (nativeIcons `storedIconType`) — as Notion draws them from its
 * picker's three tabs (seen 2026-11-11): an emoji as it is, a built-in icon's
 * "name:color" and a picture's address each as large as the emoji would be
 * in its place; without one, its fallback. A picture uploaded there is the
 * workspace file's address, read as the workspace's files are.
 */
export function KeptIconGlyph({ icon, fallback }: { icon?: string; fallback: ReactNode }) {
  const clean = icon?.trim() ?? "";
  const kind = storedIconType(clean);
  if (kind === "icon") {
    const native = parseNativeIconValue(clean);
    if (native) return <span className={styles.picture} data-kept-icon="icon"><BuiltInIconGlyph icon={native} size={20} /></span>;
  }
  if (kind === "image" && nativeWorkspaceFileIdFromLocator(clean)) {
    return <span className={styles.picture} data-kept-icon="file"><KeptFileImage locator={clean} /></span>;
  }
  if (kind === "image") {
    const src = safeExternalPresentationImage(clean);
    return src ? <span className={styles.picture} data-kept-icon="image"><IconImage src={src} /></span> : <>{fallback}</>;
  }
  return <>{clean || fallback}</>;
}

// An uploaded picture's address, signed for the reader; nothing until it is.
function KeptFileImage({ locator }: { locator: string }) {
  const file = useWorkspaceFile(locator, [], { width: filePreviewWidth(64) });
  return file.url ? <IconImage src={file.url} refused={file.failedStatus} /> : <span />;
}

/**
 * What a workspace's, a teamspace's or a group's icon picker sends a picture
 * with: the workspace, it or the teamspace or group keeping the file as its
 * icon until it changes (the server's held_icon_files.go).
 */
export function keptIconUploadTarget(workspaceId: string, holder: NonNullable<WorkspaceFileTarget["iconHolder"]>): WorkspaceFileTarget {
  return { workspaceId, iconHolder: holder };
}

/** What a workspace, a teamspace or a group keeps for a picked picture — an uploaded file's address, or a link — or "" for anything else. */
export function keptIconPicture(value: string) {
  const icon = managedFileValueLocator(value);
  return storedIconType(icon) === "image" ? icon : "";
}

/** What a teamspace shows for an icon: its own, else its name's first letter. */
export function teamspaceGlyph(teamspace: Pick<Teamspace, "icon" | "name">) {
  return <KeptIconGlyph icon={teamspace.icon} fallback={teamspaceLetter(teamspace.name)} />;
}

export function teamspaceLetter(name: string | undefined) {
  return name?.trim().slice(0, 1).toUpperCase() || "?";
}
