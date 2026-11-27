"use client";

import { pageHref } from "@/lib/navigation";
import { inlineDatabaseAddressPage } from "@/lib/inlineDatabaseAddress";
import {
  spansToPlainText,
  type Block,
  type ButtonTemplateBlock,
  type DbView,
  type FilterGroup,
  type ViewType,
} from "@/lib/types";
import { FileBlank, FileText, iconPixels, type LeafenIconSize, ViewTypeMark } from "@/icons/leafen";
import { databaseViewLabels } from "./databaseViewLabels";
import { askingLinkedViewHere } from "@/lib/databaseStarterLinked";
import { BuiltInIconGlyph, IconImage, PageIconGlyph } from "@/components/page/PageIcon";
import { parseNativeIconValue, storedIconPresentation, storedIconType } from "@/components/page/nativeIcons";
import { filePreviewWidth, useWorkspaceFile } from "@/lib/fileUrls";
import { serverUrl } from "@/lib/serverOrigin";
import { touchScreen } from "@/lib/screenKind";


export const NOTION_2023_VIEW_TYPES: { type: ViewType }[] = [
  { type: "table" },
  { type: "board" },
  { type: "gallery" },
  { type: "list" },
  { type: "chart" },
  { type: "dashboard" },
  { type: "timeline" },
  { type: "feed" },
  { type: "map" },
  { type: "calendar" },
  { type: "form" },
];

function viewTypeSettingsLabel(type: ViewType) {
  return databaseViewLabels().viewTypes[type] ?? type;
}

/**
 * A view's name as it is shown: its own, spaces and all, or, for a view
 * never named or named "" as Notion leaves one made through Markdown, added
 * from its menus or cleared by its reader (seen 2026-10-10 and 2026-11-11),
 * its kind in the reader's language ("표"). A name of spaces shows as they
 * are, an empty tab, as Notion's (seen 2026-11-11).
 */
export function viewDisplayName(view: { id?: string; name: string; type: string; config?: object }) {
  // A linked view waiting for its view is "새 보기" only where it asks for it
  // (askingLinkedViewHere); anywhere else the table it waits as, whatever
  // name an older one was stored with.
  if ((view.config as { linkedStarterPending?: unknown } | undefined)?.linkedStarterPending === true) {
    return askingLinkedViewHere(view) ? databaseViewLabels().newView : viewTypeSettingsLabel(view.type as ViewType);
  }
  return view.name || viewTypeSettingsLabel(view.type as ViewType);
}

/**
 * The name a person gave a view, "" for one they never named. A view never
 * named is stored as "" and any other name is its own, its layout's name
 * too, the one rule the server reads it by (viewNeverNamed in
 * database_view_name.go): Notion keeps a view still called its layout's name
 * apart from one named so, whose name field, tab title and layout switch keep
 * it (seen 2026-11-06: an unnamed "캘린더" showed an empty field, one named
 * "갤러리" showed "갤러리").
 */
export function viewOwnName(view: { name: string }) {
  return view.name;
}

/**
 * A view renamed, marked when it is named "": Notion keeps a name cleared
 * apart from one never given, its API reading "" for the first and
 * "Untitled" for the second (seen 2026-11-11; database_view_name.go on the
 * server).
 */
export function renamedView<View extends { name: string; type: string; config: Record<string, unknown> }>(view: View, name: string): View {
  const { namedBlank: _blank, ...config } = view.config;
  return { ...view, name, config: name === "" ? { ...config, namedBlank: true } : config };
}

export function effectiveOpenPageIn(view?: Pick<DbView, "type" | "config">): "side" | "center" | "full" {
  if (!view) return "side";
  if (view.config?.openPageIn) return view.config.openPageIn;
  return defaultOpenPageIn(view.type);
}

/** Where a layout opens its pages until its view says, which its choices mark (seen 2026-11-04). */
export function defaultOpenPageIn(type: DbView["type"]): "side" | "center" {
  // A gallery, calendar, feed or map opens its pages in the middle, as
  // Notion's new ones do (seen 2026-11-02 for a feed and a map).
  return type === "gallery" || type === "calendar" || type === "feed" || type === "map"
    ? "center"
    : "side";
}

/**
 * A view's icon: the one it was given, as Notion's views can have (seen
 * 2026-10-05, from Notion's icons), or else its type's.
 */
export function ViewIcon({ view, size = "small" }: { view: { type: string; config?: Record<string, unknown> | null }; size?: LeafenIconSize }) {
  const given = typeof view.config?.icon === "string" ? view.config.icon.trim() : "";
  const own = given ? parseNativeIconValue(given) : undefined;
  if (own) return <span aria-hidden="true" style={{ display: "inline-flex" }} data-view-icon={given}><BuiltInIconGlyph icon={own} size={iconPixels(size)} /></span>;
  // An emoji given, drawn at the icon's size (2026-11-05: the tab showed its type's instead).
  if (given && !isImageIcon(given)) {
    return <span aria-hidden="true" data-view-icon={given} style={{ display: "inline-flex", alignItems: "center", justifyContent: "center",
      width: iconPixels(size), height: iconPixels(size), fontSize: iconPixels(size) * 0.9, lineHeight: 1 }}>{given}</span>;
  }
  // A chart view keeps the chart's own mark whatever kind it draws, as Notion's (seen 2026-11-03).
  return <ViewTypeIcon type={view.type as ViewType} size={size} />;
}

/** Whether a view draws an icon of its own rather than its layout's mark. */
export function viewHasOwnIcon(view: { config?: Record<string, unknown> | null }) {
  const given = typeof view.config?.icon === "string" ? view.config.icon.trim() : "";
  return Boolean(given && (parseNativeIconValue(given) || !isImageIcon(given)));
}

/**
 * A view's mark in its tab, as Notion's (seen 2026-11-14 at 1440 and 768):
 * its layout's 20px; an icon of its own 16px drawn 1.2 times over, 19.2px
 * standing 1.6px out of its 16px place, which the name follows.
 */
export function ViewTabGlyph({ view, className }: { view: { type: string; config?: Record<string, unknown> | null }; className?: string }) {
  const own = viewHasOwnIcon(view);
  return <span className={className} data-own-icon={own || undefined}><ViewIcon view={view} size={own ? "small" : "regular"} /></span>;
}

/** A layout's mark, from the one place every view screen takes it (ViewTypeMark). */
export function ViewTypeIcon({ type, size = "small" }: { type: ViewType; size?: LeafenIconSize }) {
  return <ViewTypeMark type={type} size={size} />;
}


function isImageIcon(icon: string) {
  return /^(https?:\/\/|data:image\/|blob:|\/)/i.test(icon);
}

/** A template's icon, at a menu's size or at `pixels` (a card's 18). */
export function TemplateIconGlyph({ icon, empty = false, size = "small", pixels }: { icon?: string; empty?: boolean; size?: LeafenIconSize; pixels?: number }) {
  const cleanIcon = icon?.trim();
  const px = pixels ?? iconPixels(size);
  // A template with an empty body is an empty document, as a page is (PageIcon).
  if (!cleanIcon) return empty ? <FileBlank pixels={px} aria-hidden="true" /> : <FileText pixels={px} aria-hidden="true" />;
  const kind = storedIconType(cleanIcon);
  if (kind === "image") return <TemplatePictureIcon icon={cleanIcon} size={px} />;
  // A workspace emoji as a page shows one, and its document once the emoji is deleted, as Notion's (seen 2026-11-11).
  if (kind === "custom_emoji") return <PageIconGlyph page={{ ...storedIconPresentation(cleanIcon), hasContent: !empty }} size={px} />;
  const own = parseNativeIconValue(cleanIcon);
  return own ? <BuiltInIconGlyph icon={own} size={px} /> : cleanIcon;
}

/**
 * A template's picture, as a page made from it shows it (PageIconGlyph): an
 * uploaded one, kept as its file's address, is read as the workspace's files
 * are, signed for the reader; a link as it is. Its place is empty until the
 * address is known, and a picture that can't be loaded leaves it blank.
 */
function TemplatePictureIcon({ icon, size }: { icon: string; size: number }) {
  const file = useWorkspaceFile(icon, ["data:image/"], { width: filePreviewWidth(128) });
  if (!file.url && !file.failedStatus) return <span style={{ display: "block", width: size, height: size }} />;
  return <IconImage src={file.url} size={size} refused={file.failedStatus} page={{ box: size + 2, fill: false }} />;
}

/**
 * The ID a database's view link names, as Notion's "보기 링크 복사" makes it
 * (seen 2026-11-24 in Chrome in two tabs: /p/<database>?v=<view>): its own
 * address's. A full-page database's is its page's; one in a page — linked or
 * not, shown there or alone — has its block's, which opens it alone
 * (inlineDatabaseAddress.ts), never the page holding it, whose address with
 * the view Notion's leaves at the view it last showed. `pageId` is the page it
 * is drawn in, `ownPage` whether it is that page's own (DatabaseOwnPageContext).
 */
export function databaseViewLinkId({ pageId, blockId, ownPage }: { pageId: string; blockId: string; ownPage: boolean }) {
  // Shown alone, it is still in the page holding it.
  return !ownPage || inlineDatabaseAddressPage(blockId) === pageId ? blockId : pageId;
}

/** A view's link: the address of the database with that ID (databaseViewLinkId) with the view. */
export function databaseViewLink(databaseId: string, viewId: string) {
  if (typeof window === "undefined") return `${pageHref(databaseId)}?v=${encodeURIComponent(viewId)}`;
  const url = new URL(serverUrl(pageHref(databaseId)));
  url.searchParams.set("v", viewId);
  return url.toString();
}

/** A template's empty body's line: a touch screen's says to tap there, as Notion's iPad (seen 2026-11-14 at 834). */
export function templateBodyPlaceholder() {
  const labels = databaseViewLabels();
  return touchScreen() ? labels.templateBodyTapPlaceholder : labels.templateBodyPlaceholder;
}

function cloneTemplateBlockContent(
  content?: ButtonTemplateBlock["content"]
): NonNullable<ButtonTemplateBlock["content"]> {
  const value = content ?? { rich: [] };
  if (typeof structuredClone === "function") return structuredClone(value);
  return JSON.parse(JSON.stringify(value)) as NonNullable<ButtonTemplateBlock["content"]>;
}

function templateEditorBlockId(pageId: string, path: number[]) {
  return `${pageId}:block:${path.join(".")}`;
}

export function templateBlocksToEditorBlocks(
  pageId: string,
  blocks: ButtonTemplateBlock[],
  now: string,
  parentId: string | null = null,
  path: number[] = []
): Block[] {
  return blocks.flatMap((block, index) => {
    const currentPath = [...path, index];
    const content = cloneTemplateBlockContent(block.content);
    const id = templateEditorBlockId(pageId, currentPath);
    const editorBlock: Block = {
      id,
      pageId,
      parentId,
      type: block.type,
      content,
      plainText: spansToPlainText(content.rich) || content.expression || content.url || content.fileName || "",
      position: index + 1,
      createdAt: now,
      updatedAt: now,
    };
    return [
      editorBlock,
      ...templateBlocksToEditorBlocks(pageId, block.children ?? [], now, id, currentPath),
    ];
  });
}

export function editorBlocksToTemplateBlocks(blocks: Block[], parentId: string | null = null): ButtonTemplateBlock[] {
  return blocks
    .filter((block) => (block.parentId ?? null) === parentId)
    .sort((a, b) => a.position - b.position)
    .map((block) => {
      const next: ButtonTemplateBlock = {
        type: block.type,
        content: cloneTemplateBlockContent(block.content),
      };
      const children = editorBlocksToTemplateBlocks(blocks, block.id);
      if (children.length > 0) next.children = children;
      return next;
    });
}

// --- Nested filter tree helpers (pure, immutable) -------------------------------
// A `path` is the chain of group indices from the root: [] = root group, [0] = its
// first sub-group, [0,2] = the third sub-group of that, and so on. Every transform
// clones the groups it touches so React state is never mutated in place.

/** Count every leaf condition in the tree (used for the toolbar button badge). */
export function countLeaves(group: FilterGroup): number {
  let total = group.filters.length;
  for (const sub of group.groups ?? []) total += countLeaves(sub);
  return total;
}

/**
 * Return a new tree where the group at `path` is replaced by `fn(group)`. The path
 * and every ancestor are rebuilt with fresh objects/arrays; untouched branches are
 * shared by reference.
 */
export function updateGroupAtPath(
  root: FilterGroup,
  path: number[],
  fn: (group: FilterGroup) => FilterGroup
): FilterGroup {
  if (path.length === 0) return fn(root);
  const [index, ...rest] = path;
  const groups = root.groups ?? [];
  const child = groups[index];
  if (!child) return root;
  const nextChild = updateGroupAtPath(child, rest, fn);
  const nextGroups = groups.map((g, i) => (i === index ? nextChild : g));
  return { ...root, groups: nextGroups };
}
