import { useEffect, useSyncExternalStore } from "react";
import { request } from "@/apiTransport";
import {
  addLookedUpPeople,
  askAgainForPeopleShown,
  directoryPeopleCount,
  peopleDirectoryRevision,
  appDirectoryRevision,
  setPeopleLookup,
  setSignedInPerson,
  setWorkspaceDirectoryPeople,
  subscribePeopleDirectory,
  type PersonDirectoryEntry,
} from "@/lib/peopleDirectory";
import { setPeopleProfileSettings, type PeopleProfileSettings } from "@/lib/peopleProfiles";
import { onWindowReturn } from "@/lib/windowReturn";
import type { Session } from "@/types";

// The server names at most this many people asked for at once; it lists a
// workspace's people a page at a time, each page's cursor reading on.
const WORKSPACE_PEOPLE_LOOKUP_LIMIT = 100;
// The server takes at most this many invited addresses in one ask
// (people_value_invitees.go maxPeopleValueInvitees); more go in turns.
const PEOPLE_INVITEES_PER_ASK = 20;

type WorkspacePeopleResponse = {
  people: Array<{ userId: string; displayName: string; avatar: string; suspended: boolean; joinedAt?: string; bot?: boolean; invitee?: boolean; email?: string; deactivated?: boolean }>;
  hasMore: boolean;
  nextCursor?: string;
  profiles?: PeopleProfileSettings;
};

/**
 * Whom a person value names for each address just invited to the workspace:
 * the account that proved it, or the one invited, named by the address until
 * an account proves it — as Notion's invite puts the person into the cell at
 * once (seen 2026-11-12). The directory learns them.
 */
export async function invitedPeopleValues(session: Session, workspaceId: string, emails: readonly string[], signal?: AbortSignal) {
  const people: WorkspacePeopleResponse["people"] = [];
  // However many were invited at once, every one is named (an invite of 21
  // or more was refused whole, and none without an account went in).
  for (let start = 0; start < emails.length; start += PEOPLE_INVITEES_PER_ASK) {
    const response = await request<{ people: WorkspacePeopleResponse["people"] }>(
      `/workspaces/${encodeURIComponent(workspaceId)}/people/invitees`,
      { method: "POST", body: JSON.stringify({ emails: emails.slice(start, start + PEOPLE_INVITEES_PER_ASK) }), signal }, session,
    );
    people.push(...response.people);
  }
  addLookedUpPeople(directoryPeopleCount() + people.length, people);
  return people.map((person) => person.userId);
}

/** Every page of a workspace's people (owner and members, not apps), in name order. */
export async function readAllWorkspacePeople(session: Session, workspaceId: string, signal: AbortSignal) {
  const people: WorkspacePeopleResponse["people"] = [];
  let profiles: PeopleProfileSettings | null = null;
  let cursor = "";
  do {
    const response = await request<WorkspacePeopleResponse>(
      `/workspaces/${encodeURIComponent(workspaceId)}/people${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`, { signal }, session,
    );
    people.push(...response.people);
    profiles ??= response.profiles ?? null;
    cursor = response.nextCursor ?? "";
  } while (cursor);
  return { people, profiles };
}

// Whether a workspace's people were listed since the app started, told with
// the directory (subscribePeopleDirectory): an ID none of them has is then
// no person's.
let peopleListed = false;

export function workspacePeopleListed() {
  return peopleListed;
}

/**
 * Fills the people directory with the open workspace's people — names,
 * pictures and suspensions — when the workspace opens and when the app comes
 * back into view, so person properties, mentions and bylines name them; in
 * a workspace where the reader is a guest, the people its pages show. A
 * person the directory doesn't know yet, such as a member just added, is
 * asked for as they show.
 */
export function useWorkspacePeopleDirectory(session: Session | null, workspaceId: string | null | undefined, guest = false) {
  const userId = session?.user.id ?? "", userName = session?.user.name ?? "";
  useEffect(() => setSignedInPerson(userId, userName), [userId, userName]);
  useEffect(() => {
    if (!session || !workspaceId) return;
    const lookups = new AbortController();
    setPeopleLookup((ids) => {
      for (let start = 0; start < ids.length; start += WORKSPACE_PEOPLE_LOOKUP_LIMIT) {
        request<{ people: WorkspacePeopleResponse["people"] }>(
          `/workspaces/${encodeURIComponent(workspaceId)}/people/lookup`,
          { method: "POST", body: JSON.stringify({ userIds: ids.slice(start, start + WORKSPACE_PEOPLE_LOOKUP_LIMIT) }), signal: lookups.signal },
          session,
        ).then((response) => {
          // The directory holds the workspace's people and those looked up.
          if (!lookups.signal.aborted) addLookedUpPeople(directoryPeopleCount() + response.people.length, response.people);
        }).catch(() => undefined);
      }
    });
    // A guest knows only the people its shared pages show, as they show,
    // and has no profiles; coming back, it asks again for them all, as a
    // member's reads its list again: a name or a picture changed while it
    // was away shows, and one invited may have an account by then.
    if (guest) {
      peopleListed = true;
      setWorkspaceDirectoryPeople(0, []);
      setPeopleProfileSettings(null, null);
      const stopReturn = onWindowReturn(askAgainForPeopleShown);
      return () => {
        lookups.abort();
        setPeopleLookup(null);
        stopReturn();
      };
    }
    let controller = new AbortController();
    const load = () => {
      controller.abort();
      controller = new AbortController();
      const { signal } = controller;
      readAllWorkspacePeople(session, workspaceId, signal)
        .then((response) => {
          if (signal.aborted) return;
          const people: PersonDirectoryEntry[] = response.people.map((person) => ({
            userId: person.userId, displayName: person.displayName, avatar: person.avatar, suspended: person.suspended,
            joinedAt: person.joinedAt,
          }));
          peopleListed = true;
          setWorkspaceDirectoryPeople(people.length, people);
          setPeopleProfileSettings(workspaceId, response.profiles);
        })
        .catch(() => undefined);
    };
    load();
    const stopReturn = onWindowReturn(load);
    return () => {
      controller.abort();
      lookups.abort();
      setPeopleLookup(null);
      stopReturn();
    };
  }, [guest, session, workspaceId]);
}

const noRevision = () => 0;
const noSubscription = () => () => undefined;

/** Redraws the app as the directory changes, but for the page titles it keeps as it draws (useMentionPageTitles). */
export function useAppDirectoryRevision() {
  return useSyncExternalStore(subscribePeopleDirectory, appDirectoryRevision, appDirectoryRevision);
}

/** Redraws a component naming people when the directory changes; one naming no one now, not. */
export function usePeopleDirectoryRevision(names = true) {
  const read = names ? peopleDirectoryRevision : noRevision;
  return useSyncExternalStore(names ? subscribePeopleDirectory : noSubscription, read, read);
}
