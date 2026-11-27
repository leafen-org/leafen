import { useEffect, useSyncExternalStore, type CSSProperties } from "react";
import { AVATAR_ICONS, type AvatarIconName } from "@/avatarIcons.generated";
import type { AVATAR_ICON_FINE_WEIGHTS } from "@/avatarIconFineWeights.generated";
import { SuspendedPersonGlyph } from "@/components/SuspendedPersonGlyph";
import { safePresentationImage } from "@/components/page/pagePresentation";
import { personAvatarValue, personIsBot, personName, personSuspended } from "@/lib/peopleDirectory";
import { serverPath } from "@/lib/serverOrigin";
import { usePeopleDirectoryRevision } from "@/lib/workspacePeopleDirectory";
import styles from "./PersonAvatar.module.css";

export const AVATAR_ICON_PREFIX = "avatar:";

/** The line icon an avatar value shows, if it shows one. */
export function avatarIconName(value?: string | null): AvatarIconName | null {
  if (!value?.startsWith(AVATAR_ICON_PREFIX)) return null;
  const name = value.slice(AVATAR_ICON_PREFIX.length);
  return Object.hasOwn(AVATAR_ICONS, name) ? (name as AvatarIconName) : null;
}

/**
 * The line's width on screen for an icon shown this large: one weight in the
 * drawing grew with the picture, heavy in the picker and on a profile
 * (2026-10-15: "선이 너무 굵어서 세련되지 못한"). Small places keep enough to
 * read; Notion's own icons run about 9% of their size, 2.25px at 24px.
 */
function avatarIconLineWidth(size: number) {
  if (size >= 48) return 2;
  if (size >= 24) return 1.5;
  return 1.25;
}

type AvatarIconWeight = "thin" | "light" | "regular" | "bold";
// Phosphor's weights draw lines 8, 12, 16 and 24 of the glyph's 256, and the
// glyph takes 86% of the circle (PersonAvatar.module.css).
const AVATAR_ICON_WEIGHT_UNITS: Record<AvatarIconWeight, number> = { thin: 8, light: 12, regular: 16, bold: 24 };
const AVATAR_ICON_SHARE = 0.86;

/**
 * The Phosphor weight whose lines come nearest that width at a size: bold in
 * the smallest places, as the app's own icons below 18px, regular around
 * 20–30px, lighter above.
 */
function avatarIconWeight(size: number): AvatarIconWeight {
  const line = (weight: AvatarIconWeight) => AVATAR_ICON_WEIGHT_UNITS[weight] / 256 * size * AVATAR_ICON_SHARE;
  const width = avatarIconLineWidth(size);
  let nearest: AvatarIconWeight = "regular";
  for (const weight of Object.keys(AVATAR_ICON_WEIGHT_UNITS) as AvatarIconWeight[]) {
    if (Math.abs(line(weight) - width) < Math.abs(line(nearest) - width)) nearest = weight;
  }
  return nearest;
}

// The lighter weights only large avatars show come when one first shows.
let fineWeights: typeof AVATAR_ICON_FINE_WEIGHTS | null = null;
let fineWeightsLoading: Promise<void> | null = null;
const fineWeightsListeners = new Set<() => void>();

function loadFineWeights() {
  fineWeightsLoading ??= import("@/avatarIconFineWeights.generated").then((module) => {
    fineWeights = module.AVATAR_ICON_FINE_WEIGHTS;
    for (const listener of fineWeightsListeners) listener();
  }, () => {
    fineWeightsLoading = null;
  });
}

function useFineWeights(wanted: boolean) {
  useEffect(() => {
    if (wanted && !fineWeights) loadFineWeights();
  }, [wanted]);
  return useSyncExternalStore((listener) => {
    fineWeightsListeners.add(listener);
    return () => { fineWeightsListeners.delete(listener); };
  }, () => fineWeights, () => null);
}

export function AvatarIconGlyph({ name, size }: { name: AvatarIconName; size?: number }) {
  const weight = size ? avatarIconWeight(size) : "regular";
  const fine = weight === "thin" || weight === "light";
  const loaded = useFineWeights(fine);
  // Until a large avatar's lighter lines come, its regular ones.
  const paths = weight === "thin" || weight === "light"
    ? loaded?.[name][weight] ?? AVATAR_ICONS[name].regular
    : AVATAR_ICONS[name][weight];
  return (
    <svg viewBox="0 0 256 256" fill="currentColor" aria-hidden="true">
      {paths.map((d) => <path key={d} d={d} />)}
    </svg>
  );
}

function firstLetter(name: string) {
  const letter = Array.from(name.trim())[0] ?? "";
  return letter.toUpperCase() || "?";
}

/** What an avatar value shows: a line icon, a photo, an emoji kept from before or the first letter. */
export function avatarKind(value?: string | null): "icon" | "image" | "emoji" | "letter" {
  const avatar = (value ?? "").trim();
  if (avatarIconName(avatar)) return "icon";
  if (avatar && safePresentationImage(avatar)) return "image";
  if (avatar && !avatar.startsWith(AVATAR_ICON_PREFIX) && Array.from(avatar).length <= 4) return "emoji";
  return "letter";
}

/**
 * A person's picture, the same everywhere people are shown: their photo, the
 * line icon they picked, or — for everyone else, as Notion shows a person
 * without a photo (the decision of 2026-11-09) — the first letter of their
 * name, or of their email when they have no name, on Notion's circle.
 */
export function PersonAvatar({ userId, avatar, name, size = 20, className, title, bot }: {
  /** Reads the person's picture and name from the people directory. */
  userId?: string | null;
  /**
   * An integration rather than a person: its letter on a rounded square a
   * little inside the slot, as Notion draws an integration that edited or
   * made a page (seen 2026-11-06: 14px at radius 3 in a 16px slot, 25px at
   * radius 4 in 28px).
   */
  bot?: boolean;
  /** The picture to show when the directory doesn't have it. */
  avatar?: string | null;
  name?: string | null;
  size?: number;
  className?: string;
  title?: string;
}) {
  usePeopleDirectoryRevision();
  const id = userId ?? "";
  const value = (avatar ?? "").trim() || personAvatarValue(id);
  const label = (name ?? "").trim() || personName(id);
  const style = { "--avatar-size": `${size}px` } as CSSProperties;
  const classes = className ? `${styles.avatar} ${className}` : styles.avatar;
  // A connection's bot a value names, from the directory, as one is drawn.
  const integration = (bot ?? personIsBot(id)) ? "" : undefined;
  if (personSuspended(id)) {
    return <span className={classes} style={style} data-avatar="suspended" title={title} aria-hidden="true"><SuspendedPersonGlyph /></span>;
  }
  const kind = avatarKind(value);
  const icon = avatarIconName(value);
  if (icon) {
    return <span className={classes} style={style} data-avatar="icon" title={title} aria-hidden="true"><AvatarIconGlyph name={icon} size={size} /></span>;
  }
  if (kind === "image") {
    // A photo kept with the account lives on the server the person is on.
    const image = safePresentationImage(value);
    const src = image.startsWith("/api/") ? serverPath(image) : image;
    return <span className={classes} style={style} data-avatar="image" data-avatar-bot={integration} title={title} aria-hidden="true"><img src={src} alt="" loading="lazy" /></span>;
  }
  // An emoji chosen before line icons existed stays; a connection's own icon.
  if (kind === "emoji") {
    return <span className={classes} style={style} data-avatar="emoji" data-avatar-bot={integration} title={title} aria-hidden="true">{value}</span>;
  }
  return <span className={classes} style={style} data-avatar="letter" data-avatar-bot={integration} title={title} aria-hidden="true">{firstLetter(label)}</span>;
}
