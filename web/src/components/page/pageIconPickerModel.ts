import type { WorkspaceFileTarget } from "@/lib/storage";
import { builtInIcon, storedIconType, type NativeIconSource } from "./nativeIcons";

export const RECENT_ICON_STORAGE_KEY = "leafen:recent-icons";

export function hasLifecycleTrackedIconUploadTarget(target?: WorkspaceFileTarget) {
  const hasWorkspace = typeof target?.workspaceId === "string" && target.workspaceId.trim().length > 0;
  const hasPageOrBlock = [target?.pageId, target?.blockId].some(
    (value) => typeof value === "string" && value.trim().length > 0
  );
  return hasWorkspace && (hasPageOrBlock || Boolean(target?.iconHolder));
}

export type IconPickerTab = "emoji" | "icons" | "custom";

/** A page's tabs, as Notion's page icon: "이모지 · 아이콘 · 업로드", opening on the first it offers. */
export const PAGE_ICON_TABS: readonly IconPickerTab[] = ["emoji", "icons", "custom"];

/** A teamspace's tabs, as Notion's new teamspace and teamspace settings (seen 2026-11-16 at 1440 dark,
 *  both, in two tabs): "아이콘 · 이모지 · 업로드", opening on "아이콘". */
export const ICON_FIRST_TABS: readonly IconPickerTab[] = ["icons", "emoji", "custom"];

/** The icon a picker opens on: a page's (its icon and kind) or one kept as a bare string. */
export type PickerCurrentIcon = NativeIconSource | string | undefined;

/** The tab a picker opens on, one rule for every icon picker as Notion's (seen 2026-11-16 at 1440 dark in two tabs
 *  at different times, a page's, a database's, the sidebar's and a teamspace's): "아이콘" when the icon is a built-in
 *  one, else the first tab it offers — a page's emoji, picture or none "이모지", a teamspace's emoji "아이콘". The tab
 *  last used is not remembered (a page's emoji after "아이콘" was used reopens on "이모지"). */
export function openingIconTab(tabs: readonly IconPickerTab[], current: PickerCurrentIcon): IconPickerTab {
  const builtIn = typeof current === "string" ? storedIconType(current) === "icon" : Boolean(current && builtInIcon(current));
  return builtIn && tabs.includes("icons") ? "icons" : tabs[0] ?? "custom";
}
