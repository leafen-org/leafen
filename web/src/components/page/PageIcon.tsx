import { DatabaseCylinderIcon, FileBlank, FileText, TableIcon } from "@/icons/leafen";
import { createContext, useContext, useId, type CSSProperties, type ReactNode } from "react";
import type { Page } from "@/types";
import { useWorkspaceCustomEmojiFileId } from "@/lib/customEmojiIcon";
import { filePreviewWidth, useWorkspaceFile } from "@/lib/fileUrls";
import { useImageLoadFailure } from "@/lib/imageLoadFailure";
import { isPeopleDatabasePage, PEOPLE_DATABASE_ICON } from "@/lib/peopleDatabasePage";
import { personAvatarValue } from "@/lib/peopleDirectory";
import { serverPath } from "@/lib/serverOrigin";
import { usePeopleDirectoryRevision } from "@/lib/workspacePeopleDirectory";
import { useNamedPage } from "@/lib/pageNames";
import { nativeWorkspaceFileIdFromLocator, nativeWorkspaceFileLocator } from "@/lib/workspaceFileTransport";
import {
  builtInIcon,
  nativeIconColorVars,
  nativeIconLabel,
  parseNativeIconValue,
  storedIconType,
  type NativeIcon,
} from "./nativeIcons";
import { NativeIconBody, nativeIconDrawing } from "./nativeIconDrawing";
import { safePresentationImage } from "./pagePresentation";
import { isPagePictureIcon, PERSON_ICON_PREFIX } from "./pagePictureIcon";

export { isPagePictureIcon, PERSON_ICON_PREFIX };

function imageStyle(size: number): CSSProperties {
  return {
    display: "block",
    width: size,
    height: size,
    borderRadius: Math.max(3, Math.round(size * 0.18)),
    objectFit: "cover",
  };
}

// A page's picture icon is 0.888 of its icon's box and a workspace emoji
// fills the box, both rounded 4, as Notion's (measured 2026-11-01: sidebar
// 17.76 and 20 in 20, breadcrumb 15.98 and 18 in 18, page block 19.53 and 22
// in 22, search 17.76 in 20 and 42.62 in 48, header 124.3 in 140 and 78 in 78).
// The box is a place's glyph size and 2 more unless the place gives its own;
// the picture keeps the glyph's room, standing over or within it.
const PICTURE_SHARE = 0.888;
function pageImageStyle(size: number, box: number, fill: boolean): CSSProperties {
  const side = fill ? box : box * PICTURE_SHARE;
  return { display: "block", width: side, height: side, margin: (size - side) / 2, borderRadius: 4, objectFit: "cover" };
}

/**
 * An icon's picture; one that couldn't be loaded leaves its place blank, as
 * Notion's page, sidebar, breadcrumb and table icons do (seen 2026-10-21).
 * A page's icon gives its box and whether it fills it (a workspace emoji).
 */
export function IconImage({ src, size, refused = 0, page }: {
  src: string; size?: number; refused?: number; page?: { box: number; fill: boolean };
}) {
  const failure = useImageLoadFailure(src, refused);
  const style = size === undefined ? undefined : page ? pageImageStyle(size, page.box, page.fill) : imageStyle(size);
  if (failure.failed) return <span style={style ?? { display: "inline-block", width: "1em", height: "1em" }} data-icon-failed="true" />;
  return <img src={src} alt="" style={style} loading="lazy" onError={failure.fail} />;
}

/** Draws a People row's person (PERSON_ICON_PREFIX) as their picture where the list of people provides it. */
export const PersonIconContext = createContext<((userId: string, size: number) => ReactNode) | null>(null);

export type PageIconSource = Pick<Page, "icon" | "iconType"> & {
  /** The page's ID: its icon follows the page's as it is changed anywhere (lib/pageNames.ts). */
  id?: string;
  kind?: string;
  /** Who made it: a database Leafen made is People's own (isPeopleDatabasePage). */
  createdBy?: string | null;
  /** A wiki whose pages are its database's is that database, as Notion draws it. */
  isWiki?: boolean;
  wikiDataSourceId?: string | null;
  /** Whether the page's body holds anything; false shows the empty document. */
  hasContent?: boolean;
  notionIcon?: unknown;
  workspaceId?: string;
};

/**
 * A page a server list names, drawn as the sidebar draws it (2026-11-13: a
 * guest's pages, an MCP token's and a connection's drew a database as a
 * document): the reader's own record of it when they have one — its kind,
 * wiki and whether it holds anything — and else what the list says, a
 * database by its kind.
 */
export function listedPageIcon(
  known: Page | undefined,
  listed: { icon: string; iconType: string; pageKind?: string },
  workspaceId?: string,
): PageIconSource {
  if (known) return known;
  const iconType = ["emoji", "image", "icon", "custom_emoji"].includes(listed.iconType) ? listed.iconType as Page["iconType"] : "none";
  return { icon: listed.icon, iconType, kind: listed.pageKind, workspaceId };
}


export function hasExplicitPageIcon(page: PageIconSource) {
  return (page.iconType !== "none" && Boolean(page.icon)) || Boolean(builtInIcon(page));
}

// Leafen draws its own built-in glyphs, painted solid as Notion's set is
// (nativeIconDrawing.tsx); until one exists for a name, a neutral mark in
// the icon's color names the icon for readers and assistive tech.
export function BuiltInIconGlyph({ icon, size, color, decorative = false }: {
  icon: NativeIcon;
  size: number;
  /** Draws it in this color instead of its own, as a property's icon in the place's icon color. */
  color?: string;
  /** Beside the name it marks, so nothing reads it out. */
  decorative?: boolean;
}) {
  const label = nativeIconLabel(icon.name);
  const drawing = nativeIconDrawing(icon.name);
  const maskId = `native-icon-cut-${useId().replace(/:/g, "")}`;
  return (
    <svg width={size} height={size} viewBox="0 0 20 20" {...(decorative ? { "aria-hidden": true } : { role: "img", "aria-label": label })}
      style={{ display: "block", color: color ?? nativeIconColorVars[icon.color] }}
      data-notion-native-icon-name={icon.name} data-notion-native-icon-color={icon.color}
      data-native-icon-glyph={drawing ? "leafen" : "neutral"} data-native-icon-solid={drawing?.solid ? "true" : undefined}>
      {decorative ? null : <title>{label}</title>}
      <NativeIconBody name={icon.name} id={maskId} />
    </svg>
  );
}

const peopleIcon = parseNativeIconValue(PEOPLE_DATABASE_ICON)!;

/** People's own icon (PEOPLE_DATABASE_ICON), `pixels` wide, beside its name. */
export function PeopleDatabaseIcon({ pixels }: { pixels: number }) {
  return <BuiltInIconGlyph icon={peopleIcon} size={pixels} decorative />;
}

export function pageIconText(page: PageIconSource, fallback = "") {
  if (page.iconType === "emoji" && page.icon) return page.icon;
  if (page.kind === "database") return "DB";
  return fallback;
}

/** The address a page's image or custom emoji icon shows from, or "" for other icons. */
export function usePageIconImageUrl(page: PageIconSource) {
  return usePageIconImage(page).url;
}

/** That address, and the server's error when it gave none for the icon's file. */
function usePageIconImage(page: PageIconSource) {
  const customEmojiFileId = useWorkspaceCustomEmojiFileId(
    page.workspaceId,
    page.iconType === "custom_emoji" ? page.icon : undefined,
  );
  const managedFileId = page.iconType === "image" && /^[A-Za-z0-9_-]{15}$/u.test(page.icon ?? "")
    ? page.icon
    : customEmojiFileId;
  const iconSource = managedFileId
    ? nativeWorkspaceFileLocator(managedFileId)
    : page.iconType === "custom_emoji" ? "" : page.icon;
  // An icon shows at most a page header's size, so a photo set as one comes small.
  const icon = useWorkspaceFile(iconSource, ["data:image/"], { width: filePreviewWidth(128) });
  return page.iconType === "image" || page.iconType === "custom_emoji" ? icon : { url: "", failedStatus: 0 };
}


type PageGlyphFallback = "file" | "database" | "none" | ReactNode;

/**
 * How a database with no icon of its own is drawn: its table, as the sidebar
 * and the move window's suggestions draw it, or the cylinder a move window's
 * search draws it with (Notion's, seen 2026-11-11). A wiki stays a table.
 */
type DatabaseGlyph = "table" | "cylinder";

/** The icon a page is shown with now, wherever it was changed (lib/pageNames.ts): the one given where it stands. */
function useNamedIcon(page: PageIconSource): PageIconSource {
  const named = useNamedPage(page.id ? page : null);
  return named && named !== page && (named.icon !== page.icon || named.iconType !== page.iconType)
    ? { ...page, icon: named.icon, iconType: named.iconType } : page;
}

/**
 * A page's icon. Only a picture or workspace emoji icon reads its address,
 * in a part of its own, so a list's rows of emoji and glyph icons keep no
 * state and queue no updates as they appear.
 */
export function PageIconGlyph({
  page: given,
  size = 16,
  box,
  fallback = "file",
  database = "table",
}: {
  page: PageIconSource;
  size?: number;
  /** The box a picture or workspace emoji icon is drawn against; size and 2 more if not given. */
  box?: number;
  fallback?: PageGlyphFallback;
  database?: DatabaseGlyph;
}) {
  const personIcon = useContext(PersonIconContext);
  const page = useNamedIcon(given);
  if (page.iconType === "image" && page.icon?.startsWith(PERSON_ICON_PREFIX)) {
    const userId = page.icon.slice(PERSON_ICON_PREFIX.length);
    if (personIcon) return personIcon(userId, size);
    return <PersonPicture userId={userId} page={page} size={size} box={box} fallback={fallback} database={database} />;
  }
  if (page.iconType === "image" || page.iconType === "custom_emoji") {
    return <PagePictureIcon page={page} size={size} box={box} fallback={fallback} database={database} />;
  }
  return pageGlyph(page, size, fallback, database);
}

/** A person's photo, read from the list of people, or the page's glyph while they have none. */
function PersonPicture({ userId, page, size, box, fallback, database }: {
  userId: string; page: PageIconSource; size: number; box?: number; fallback: PageGlyphFallback; database: DatabaseGlyph;
}) {
  usePeopleDirectoryRevision();
  const photo = safePresentationImage(personAvatarValue(userId));
  if (!photo) return pageGlyph({ ...page, icon: "", iconType: "none" }, size, fallback, database);
  return <IconImage src={photo.startsWith("/api/") ? serverPath(photo) : photo} size={size} page={{ box: box ?? size + 2, fill: false }} />;
}

/** A picture or workspace emoji icon once its address is known; the page's glyph until then. */
function PagePictureIcon({ page, size, box, fallback, database }: {
  page: PageIconSource; size: number; box?: number; fallback: PageGlyphFallback; database: DatabaseGlyph;
}) {
  const icon = usePageIconImage(page);
  if (icon.url || icon.failedStatus) {
    return <IconImage src={icon.url} size={size} refused={icon.failedStatus}
      page={{ box: box ?? size + 2, fill: page.iconType === "custom_emoji" }} />;
  }
  return pageGlyph(page, size, fallback, database);
}

/**
 * A page's icon drawn without a picture: its emoji, built-in icon or the
 * place's glyph. An emoji is marked as one, for a page in a serif or mono
 * font to draw it from the emoji fonts (typography.css).
 */
function pageGlyph(page: PageIconSource, size: number, fallback: PageGlyphFallback, database: DatabaseGlyph = "table"): ReactNode {
  if (page.iconType === "emoji" && page.icon) return <span data-emoji-glyph="">{page.icon}</span>;
  const nativeIcon = builtInIcon(page);
  if (nativeIcon) return <BuiltInIconGlyph icon={nativeIcon} size={size} />;
  // A database with no icon of its own is its table, a size up and centred
  // on the page's (Notion's sidebar and search, seen 2026-10-25: 20px for 18);
  // so is a wiki, Notion's database (seen 2026-11-11).
  if (page.kind === "database" || (page.isWiki && page.wikiDataSourceId) || fallback === "database") {
    const pixels = Math.round((size * 20) / 18);
    // People's own is Notion's People icon (PEOPLE_DATABASE_ICON) where its glyph is, as Notion's
    // sidebar draws a starred People (seen 2026-11-19: 20 in its 20px box).
    if (isPeopleDatabasePage(page)) {
      return (
        <span style={{ display: "block", margin: (size - pixels) / 2 }} data-page-glyph="people">
          <BuiltInIconGlyph icon={peopleIcon} size={pixels} />
        </span>
      );
    }
    const Glyph = database === "cylinder" && page.kind === "database" ? DatabaseCylinderIcon : TableIcon;
    return <Glyph pixels={pixels} style={{ margin: (size - pixels) / 2 }} aria-hidden="true" data-page-glyph="database" />;
  }
  if (fallback === "none") return null;
  if (fallback !== "file") return fallback;
  // A page with nothing in it is an empty document, one with anything a
  // lined one, as Notion's (seen 2026-10-01).
  if (page.hasContent === false) return <FileBlank pixels={size} aria-hidden="true" data-page-glyph="empty" />;
  return <FileText pixels={size} aria-hidden="true" />;
}

export function PageIcon({
  page,
  size = 16,
  fallback = "file",
  className,
}: {
  page: PageIconSource;
  size?: number;
  fallback?: "file" | "database" | "none" | ReactNode;
  className?: string;
}) {
  return (
    <span className={className} aria-hidden="true">
      <PageIconGlyph page={page} size={size} fallback={fallback} />
    </span>
  );
}

/** The letter a workspace without an icon shows, as Notion's sidebar and settings do. */
export function workspaceIconLetter(name: string, unnamed = "W") {
  return Array.from(name.trim())[0]?.toUpperCase() ?? unnamed;
}

/** What a workspace's icon is, for a place that draws each kind its own way:
 * its name's letter on a tint when it has none. */
export function workspaceIconKind(icon: string | undefined): "letter" | "emoji" | "icon" | "image" {
  const clean = icon?.trim() ?? "";
  if (!clean) return "letter";
  if (nativeWorkspaceFileIdFromLocator(clean) || safePresentationImage(clean)) return "image";
  if (storedIconType(clean) === "icon" && parseNativeIconValue(clean)) return "icon";
  return "emoji";
}

/**
 * A workspace's icon: an emoji, a built-in icon, a linked picture, or a
 * picture uploaded as it, read as the workspace's files are — by its
 * members, and outside it by anyone signed in (the server's
 * held_icon_files.go). A built-in icon or a picture fills `size`.
 */
export function WorkspaceIconGlyph({
  icon,
  size = 16,
  fallback,
}: {
  icon?: string;
  size?: number;
  fallback: string;
}) {
  const cleanIcon = icon?.trim();
  if (nativeWorkspaceFileIdFromLocator(cleanIcon)) return <WorkspaceFileIcon locator={cleanIcon ?? ""} size={size} />;
  const iconUrl = safePresentationImage(cleanIcon);
  if (iconUrl) {
    return <IconImage src={iconUrl} size={size} />;
  }
  // A built-in icon from the picker's "아이콘" tab, kept as "name:color".
  const native = storedIconType(cleanIcon ?? "") === "icon" ? parseNativeIconValue(cleanIcon) : undefined;
  if (native) return <BuiltInIconGlyph icon={native} size={size} />;
  return cleanIcon || fallback;
}

// An uploaded workspace icon, signed for the reader; its place stays empty
// until it is.
function WorkspaceFileIcon({ locator, size }: { locator: string; size: number }) {
  const file = useWorkspaceFile(locator, [], { width: filePreviewWidth(128) });
  return file.url ? <IconImage src={file.url} size={size} refused={file.failedStatus} />
    : <span style={{ display: "inline-block", width: size, height: size }} />;
}
