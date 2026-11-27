import { useSyncExternalStore } from "react";
import { request } from "@/apiTransport";
import { pageHref } from "@/lib/navigation";
import { personName } from "@/lib/peopleDirectory";
import { openPersonInPeek } from "@/lib/pagePeek";
import { pushRoute } from "@/lib/router";
import { bundledCoverGallery } from "@/coverImageWire.generated";
import type { Session } from "@/types";
import type { DatabaseSourceTitle } from "@/components/editor/databaseTitles";

/**
 * A workspace's people profiles, as Notion's (seen 2026-10-12): the card a
 * member's name shows, their profile page and the people list, and the
 * owner's three switches for them under 설정 › 일반 › 사람.
 */

export type PeopleProfileSettings = { directory: boolean; activity: boolean; hoverCards: boolean };

export type PersonMembership = "owner" | "member" | "restricted" | "guest" | "deactivated";

export type PersonTeamspace = { id: string; name: string; icon: string };

export type PersonCardData = {
  userId: string;
  displayName: string;
  avatar: string;
  email: string;
  self: boolean;
  membership: PersonMembership;
  timeZone: string;
  teamspaces: PersonTeamspace[];
  /** The person's first groups here, the smallest first, and how many in all (an older server sends none). */
  groups?: { id: string; name: string }[];
  groupCount?: number;
};

type PersonPageLabel = { title: string; icon: string; iconType: string; pageKind: string; viewBlock?: boolean; sourceTitles?: DatabaseSourceTitle[] };

export type PersonActivity = PersonPageLabel & { pageId: string; kind: "created" | "edited" | "commented"; at: number };

export type PersonPage = PersonPageLabel & { pageId: string; editedAt: string };

export type PersonProfileData = PersonCardData & {
  /** The cover chosen for the profile; empty until one is, when the person's gallery picture stands in. */
  cover: string;
  coverPosition: number;
  /** Whether the reader changes the cover: the person, or the workspace's owner. */
  coverEditable: boolean;
  about: string;
  activityShown: boolean;
  activity: PersonActivity[];
  pages: PersonPage[];
  collaborators: string[];
  /** The person's People row, their page, whose bar the profile shows (an older server sends none). */
  rowPageId?: string;
};

// The open workspace's switches, as its people list brought them; none for a
// guest, who has no profiles.
let current: { workspaceId: string; settings: PeopleProfileSettings } | null = null;
const listeners = new Set<() => void>();

export function setPeopleProfileSettings(workspaceId: string | null, settings: PeopleProfileSettings | null) {
  current = workspaceId && settings ? { workspaceId, settings } : null;
  cards.clear();
  listeners.forEach((listener) => listener());
}

/** The open workspace and its switches, or null where the reader has no profiles. */
function peopleProfileSettings() {
  return current;
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

export function usePeopleProfileSettings() {
  return useSyncExternalStore(subscribe, peopleProfileSettings, () => null);
}

export function personProfileHref(userId: string) {
  return `/people/${encodeURIComponent(userId)}`;
}

export const PEOPLE_DIRECTORY_HREF = "/people";

/**
 * The address a person's link is copied as: their People row's own page
 * (/p/〈row〉), which opens as their profile for whoever may read it
 * (page_links_people.go) and stands as that page where it is pasted, as
 * Notion's "링크 복사" of a person — among the library's favourites and at
 * the top of their page alike — copies their page's own address
 * (app.notion.com/p/〈이름〉-〈ID〉, seen 2026-11-16 twice), not People's.
 */
export function personPageHref(rowId: string) {
  return pageHref(rowId);
}

/**
 * Opens a member's profile — in a new tab with Cmd or Ctrl, in the peek it
 * was pressed in (openPersonInPeek) — where the open workspace has profiles
 * and knows them; false where it doesn't.
 */
export function openPersonProfile(userId: string, newTab = false, pressed?: Element | null) {
  if (!personName(userId) || !current?.settings.directory) return false;
  const href = personProfileHref(userId);
  if (newTab) window.open(href, "_blank", "noopener,noreferrer");
  else if (!openPersonInPeek(userId, pressed)) pushRoute(href);
  return true;
}

const base = (workspaceId: string) => `/workspaces/${encodeURIComponent(workspaceId)}/people`;

// A card is read once a minute at most while the app is open.
const CARD_FRESH_MS = 60_000;
const cards = new Map<string, { at: number; read: Promise<PersonCardData | null> }>();

export function personCardOnce(session: Session, workspaceId: string, userId: string) {
  const key = `${workspaceId}:${userId}`;
  const known = cards.get(key);
  if (known && Date.now() - known.at < CARD_FRESH_MS) return known.read;
  const read = request<PersonCardData>(`${base(workspaceId)}/${encodeURIComponent(userId)}/card`, {}, session)
    .catch(() => null);
  cards.set(key, { at: Date.now(), read });
  return read;
}

export type PeopleDirectoryPerson = {
  userId: string;
  displayName: string;
  avatar: string;
  email: string;
  about: string;
  membership: PersonMembership;
};

/** The People list, every page of it. */
export async function readPeopleDirectory(session: Session, workspaceId: string, signal?: AbortSignal) {
  const people: PeopleDirectoryPerson[] = [];
  let cursor = "";
  do {
    const page = await request<{ people: PeopleDirectoryPerson[]; nextCursor?: string }>(
      `${base(workspaceId)}/directory${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`, { signal }, session,
    );
    people.push(...page.people);
    cursor = page.nextCursor ?? "";
  } while (cursor);
  return { people };
}

/** A profile with all its activity and pages, as its page scrolls through them (a summary is the default). */
export function readPersonProfile(session: Session, workspaceId: string, userId: string, signal?: AbortSignal) {
  return request<PersonProfileData>(`${base(workspaceId)}/${encodeURIComponent(userId)}/profile?detail=full`, { signal }, session);
}

export function updatePersonAbout(session: Session, workspaceId: string, userId: string, about: string) {
  return request<{ about: string }>(`${base(workspaceId)}/${encodeURIComponent(userId)}/profile`, {
    method: "PATCH", body: JSON.stringify({ about }),
  }, session);
}

/** Sets a profile's cover or its position, as the person or the workspace's owner. */
export function updatePersonCover(session: Session, workspaceId: string, userId: string, change: { cover?: string; coverPosition?: number }) {
  return request<{ cover: string; coverPosition: number }>(`${base(workspaceId)}/${encodeURIComponent(userId)}/profile`, {
    method: "PATCH", body: JSON.stringify(change),
  }, session);
}

export async function updatePeopleProfileSettings(session: Session, workspaceId: string, change: Partial<PeopleProfileSettings>) {
  const settings = await request<PeopleProfileSettings>(`${base(workspaceId)}/settings`, {
    method: "PATCH", body: JSON.stringify(change),
  }, session);
  if (current?.workspaceId === workspaceId) setPeopleProfileSettings(workspaceId, settings);
  return settings;
}

/**
 * The cover a profile shows: one of the gallery's pictures, the same for a
 * person each time, as Notion gives each profile one of its own.
 */
export function personProfileCover(userId: string) {
  const covers = bundledCoverGallery.flatMap((group) => group.covers.map((cover) => cover.value));
  let hash = 0;
  for (const character of userId) hash = (hash * 31 + character.charCodeAt(0)) >>> 0;
  return covers.length ? covers[hash % covers.length] : "";
}

/** The person's local time, as the card shows it, or "" without their time zone. */
export function personLocalTime(timeZone: string, language: string, now = new Date()) {
  if (!timeZone) return "";
  try {
    return new Intl.DateTimeFormat(language, { hour: "2-digit", minute: "2-digit", timeZone }).format(now);
  } catch {
    return "";
  }
}

// A workspace's People changed — someone joined, left or was deactivated,
// or their name, About or membership changed — as the workspace's change
// socket says (useWorkspaceChangeFollow): an open People list reads itself
// again, as Notion's shows them without a reload.
const peopleRevisions = new Map<string, number>();
const peopleListeners = new Set<() => void>();

/** Says a workspace's People may have changed. */
export function announcePeopleChange(workspaceId: string) {
  peopleRevisions.set(workspaceId, (peopleRevisions.get(workspaceId) ?? 0) + 1);
  for (const listener of peopleListeners) listener();
}

const followPeople = (listener: () => void) => {
  peopleListeners.add(listener);
  return () => { peopleListeners.delete(listener); };
};

/** A count that moves each time a workspace's People may have changed. */
export function usePeopleRevision(workspaceId: string): number {
  return useSyncExternalStore(followPeople, () => peopleRevisions.get(workspaceId) ?? 0, () => 0);
}
