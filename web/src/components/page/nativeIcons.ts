import {
  NATIVE_ICONS,
  NATIVE_ICON_COLORS,
  NATIVE_ICON_DEFAULT_COLOR,
  type NativeIconColor,
} from "@/nativeIconCatalog.generated";

// Built-in icons are stored as iconType "icon" with "name:color", where name is
// an internal name from the generated catalog. The first built-in icon release
// stored underscores for hyphens, so those read as the same name.

export type NativeIcon = { name: string; color: NativeIconColor };

const nativeIconNamePattern = /^[a-z0-9]+(?:-[a-z0-9]+)*$/u;
const nativeIconAliases = new Map(
  NATIVE_ICONS.filter((entry) => entry[1] !== undefined).map(([name, alias]) => [name, alias as string]),
);

// Colors use the shared text-color tokens; light gray has none of its own.
export const nativeIconColorVars: Record<NativeIconColor, string> = {
  gray: "var(--icon-c-gray)",
  lightgray: "var(--icon-c-lightgray)",
  brown: "var(--icon-c-brown)",
  yellow: "var(--icon-c-yellow)",
  orange: "var(--icon-c-orange)",
  green: "var(--icon-c-green)",
  blue: "var(--icon-c-blue)",
  purple: "var(--icon-c-purple)",
  pink: "var(--icon-c-pink)",
  red: "var(--icon-c-red)",
};

export function isNativeIconColor(value: unknown): value is NativeIconColor {
  return typeof value === "string" && (NATIVE_ICON_COLORS as readonly string[]).includes(value);
}

function nativeIconName(raw: string) {
  const name = raw.replace(/_/gu, "-");
  return raw === raw.toLowerCase() && nativeIconNamePattern.test(name) ? name : undefined;
}

export function parseNativeIconValue(value: string | undefined): NativeIcon | undefined {
  const [stored = "", color = "", extra] = (value ?? "").split(":");
  const name = nativeIconName(stored);
  if (!name || extra !== undefined || !isNativeIconColor(color)) return undefined;
  return { name, color };
}

/** How a bare icon names a workspace emoji: by its ID after this (the server's databaseTemplateCustomEmojiPrefix). */
const STORED_CUSTOM_EMOJI_PREFIX = "custom_emoji:";
const STORED_CUSTOM_EMOJI = /^custom_emoji:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/u;

/** What an icon kept as a bare string is — a template's, as the server reads
 *  it (database_template_icon.go): an image's address, a built-in icon's
 *  "name:color", a workspace emoji ("custom_emoji:<id>"), or else an emoji. */
export function storedIconType(icon: string): "none" | "image" | "icon" | "custom_emoji" | "emoji" {
  const clean = icon.trim();
  if (!clean) return "none";
  if (/^(https?:|data:image\/|blob:|\/)/iu.test(clean)) return "image";
  if (STORED_CUSTOM_EMOJI.test(clean)) return "custom_emoji";
  return parseNativeIconValue(clean) ? "icon" : "emoji";
}

/** A bare icon as a page keeps one, its kind beside it: a workspace emoji by its ID, any other as it is. */
export function storedIconPresentation(icon: string) {
  const iconType = storedIconType(icon);
  return { icon: iconType === "custom_emoji" ? icon.trim().slice(STORED_CUSTOM_EMOJI_PREFIX.length) : icon, iconType };
}

/** The bare icon a workspace emoji is kept as, by its ID. */
export function storedCustomEmojiIcon(emojiId: string) {
  return `${STORED_CUSTOM_EMOJI_PREFIX}${emojiId}`;
}

// Reads an icon object preserved from a Notion import.
function nativeIconFromObject(name: unknown, color: unknown): NativeIcon | undefined {
  if (typeof name !== "string") return undefined;
  const normalized = nativeIconName(name.trim().toLowerCase().replace(/[\s_-]+/gu, "-"));
  if (!normalized) return undefined;
  return { name: normalized, color: isNativeIconColor(color) ? color : NATIVE_ICON_DEFAULT_COLOR };
}

export function nativeIconValue(icon: NativeIcon) {
  return `${icon.name}:${icon.color}`;
}

// The readable name is the picker tooltip, which differs from the internal
// name for some icons.
export function nativeIconLabel(name: string) {
  return (nativeIconAliases.get(name) ?? name).replace(/-/gu, " ");
}

/**
 * An icon's cell in the picker, named as Notion's (seen 2026-11-17 at 1440
 * in Chrome and on an iPhone: "anchor gray", "vitruvian man circle gray"):
 * its readable name and the key of the color it is drawn in, in every
 * language — the grid's cells and the row of an icon in every color alike.
 */
export function nativeIconCellLabel(name: string, color: NativeIconColor) {
  return `${nativeIconLabel(name)} ${color}`;
}

// The picker filter matches internal names and tooltips with the same
// separator rule as requests.
export function filteredNativeIcons(query: string): string[] {
  const key = query.trim().toLowerCase().replace(/[\s_-]+/gu, "-").replace(/^-+|-+$/gu, "");
  if (!key) return NATIVE_ICONS.map(([name]) => name);
  // Names starting with what is typed come first, as Notion's ("circle" puts
  // circle, circle-dashed… before add-circle…, seen 2026-11-06).
  const found = NATIVE_ICONS.filter(([name, alias]) => name.includes(key) || (alias ?? "").includes(key));
  return [...found.filter(([name]) => name.startsWith(key)), ...found.filter(([name]) => !name.startsWith(key))].map(([name]) => name);
}

/** A page's icon fields a built-in icon is read from. */
export type NativeIconSource = { icon?: string; iconType?: string; notionIcon?: unknown };

function notionNativeIcon(page: NativeIconSource) {
  const value = page.notionIcon;
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
  const record = value as Record<string, unknown>;
  if (record.type !== "icon") return undefined;
  const icon = record.icon;
  if (!icon || typeof icon !== "object" || Array.isArray(icon)) return undefined;
  return nativeIconFromObject((icon as Record<string, unknown>).name, (icon as Record<string, unknown>).color);
}

// A built-in icon is stored as iconType "icon" with "name:color".
export function builtInIcon(page: NativeIconSource) {
  if (page.iconType !== "icon") return notionNativeIcon(page);
  return parseNativeIconValue(page.icon);
}

/**
 * A built-in icon's share of the box a list or a title gives an icon: Notion
 * draws its built-in icons 1.2 times their 16 — 19.2 of a 20px box — and in
 * proportion in a larger one (seen in Chrome at 1440: a source list's 19.19
 * of 20, 2026-11-19; a database title's 21.12 of its 22, own and linked,
 * centred in it, 2026-11-19 and 2026-11-20 at dpr 2).
 */
export const BUILT_IN_ICON_SHARE = 19.2 / 20;
