import { useSyncExternalStore } from "react";
import { projectTeamspaceSnapshot, type DialogTeamspace } from "@/components/teamspace/teamspaceDialogModel";
import { parseStored, personValue, storePersonValue } from "@/lib/browserStorage";
import type { createNativeTeamspaceBrowser } from "@/lib/nativeTeamspaceBrowser";
import type { TeamspaceArchivedPlace } from "@/teamspaceWire.generated";

type TeamspaceBrowser = ReturnType<typeof createNativeTeamspaceBrowser>;

/**
 * Reads every teamspace of the workspace, a window at a time; null once the
 * browser's read is no longer current.
 */
export async function readWorkspaceTeamspaces(browser: TeamspaceBrowser): Promise<WorkspaceTeamspaces | null> {
  const teamspaces: DialogTeamspace[] = [];
  const seenCursors = new Set<string>();
  let cursor: string | undefined;
  let archived: TeamspaceArchivedPlace[] = [];
  for (;;) {
    const result = await browser.readSnapshot(cursor);
    if (!result) return null;
    teamspaces.push(...projectTeamspaceSnapshot(result));
    // The first window names the archived teamspaces whose pages are still open to the reader.
    if (!cursor) archived = result.archivedPlaces ?? [];
    if (!result.hasMore) return { teamspaces, ownersOnlyCreate: result.settings.ownersOnlyCreate, archived, revision: result.snapshotRevision };
    const nextCursor = result.nextCursor;
    if (!nextCursor || seenCursors.has(nextCursor)) {
      throw new Error("The teamspace list did not return its next position.");
    }
    seenCursors.add(nextCursor);
    cursor = nextCursor;
  }
}

const readsUnderway = new Map<string, Promise<WorkspaceTeamspaces | null>>();

/**
 * One read of a workspace's teamspaces at a revision for every screen that
 * shows them at once (the sidebar and the search window both read them as a
 * workspace's teamspaces moved); null when the screen that started it went.
 */
export function readTeamspacesShared(key: string, read: () => Promise<WorkspaceTeamspaces | null>) {
  const underway = readsUnderway.get(key);
  if (underway) return underway;
  const promise = read().finally(() => { if (readsUnderway.get(key) === promise) readsUnderway.delete(key); });
  readsUnderway.set(key, promise);
  return promise;
}

export type WorkspaceTeamspaces = {
  teamspaces: DialogTeamspace[];
  ownersOnlyCreate: boolean;
  /** Archived teamspaces whose pages the reader still opens, under their band (PageTeamspaceArchiveBanner). */
  archived?: TeamspaceArchivedPlace[];
  /** The workspace's teamspace revision the list was read at (its bootstrap's teamspaceRevision). */
  revision?: number;
};

const KEPT_TEAMSPACES_KEY = "leafen:teamspaces";

/**
 * The teamspaces this device last read in a workspace, which the sidebar
 * shows while the server doesn't answer, as it shows the pages it kept.
 */
export function readKeptTeamspaces(userId: string, workspaceId: string): WorkspaceTeamspaces | null {
  const kept = parseStored(personValue(KEPT_TEAMSPACES_KEY, { userId, part: encodeURIComponent(workspaceId) }));
  if (!kept || typeof kept !== "object") return null;
  const { teamspaces, ownersOnlyCreate, archived, revision } = kept as Partial<WorkspaceTeamspaces>;
  return Array.isArray(teamspaces) && typeof ownersOnlyCreate === "boolean"
    ? { teamspaces, ownersOnlyCreate, archived: Array.isArray(archived) ? archived : [], revision: typeof revision === "number" ? revision : undefined } : null;
}

/**
 * The teamspaces this device last read in a workspace while they are still
 * the workspace's — read at the teamspace revision its bootstrap gives — or
 * null: a screen that shows them needn't read them again (2026-11-13: every
 * bootstrap read them twice, other people's page changes too).
 */
export function currentKeptTeamspaces(userId: string, workspaceId: string, revision: number | undefined) {
  const key = `${userId}\u0000${workspaceId}`;
  if (!latestTeamspaces.has(key)) latestTeamspaces.set(key, readKeptTeamspaces(userId, workspaceId));
  const kept = latestTeamspaces.get(key);
  return kept && revision !== undefined && kept.revision === revision ? kept : null;
}

export function keepTeamspaces(userId: string, workspaceId: string, read: WorkspaceTeamspaces) {
  storePersonValue(KEPT_TEAMSPACES_KEY, JSON.stringify(read), { userId, part: encodeURIComponent(workspaceId) });
  latestTeamspaces.set(`${userId}\u0000${workspaceId}`, read);
  for (const listener of teamspaceListeners) listener();
}

// The teamspaces last read in each workspace, which the top bar's teamspace
// piece follows as the sidebar reads them again, without reading them itself.
const latestTeamspaces = new Map<string, WorkspaceTeamspaces | null>();
const teamspaceListeners = new Set<() => void>();

function listenForTeamspaces(listener: () => void) {
  teamspaceListeners.add(listener);
  return () => { teamspaceListeners.delete(listener); };
}

// Asks every sidebar showing a workspace's teamspaces to read them again, as
// after an archived one is brought back from its page's band.
const readRequests = new Set<(workspaceId: string) => void>();

export function onTeamspacesReadRequest(listener: (workspaceId: string) => void) {
  readRequests.add(listener);
  return () => { readRequests.delete(listener); };
}

export function requestTeamspacesRead(workspaceId: string) {
  for (const listener of readRequests) listener(workspaceId);
}

/** A workspace's teamspaces as this device last read them; null before any read. */
export function useKeptTeamspaces(userId: string, workspaceId: string): WorkspaceTeamspaces | null {
  const key = `${userId}\u0000${workspaceId}`;
  return useSyncExternalStore(listenForTeamspaces, () => {
    if (!latestTeamspaces.has(key)) latestTeamspaces.set(key, userId && workspaceId ? readKeptTeamspaces(userId, workspaceId) : null);
    return latestTeamspaces.get(key) ?? null;
  });
}

/** Whether the reader may add pages at the top of the teamspace. */
export function canAddTeamspacePages(teamspace: DialogTeamspace) {
  return teamspace.joined && !teamspace.archivedAt
    && (teamspace.pageRole === "edit" || teamspace.pageRole === "full_access");
}
