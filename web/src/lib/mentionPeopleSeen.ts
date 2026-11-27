import type { OrganizationProfile } from "@/lib/types";
import type { OrganizationPeopleSearchTransport } from "./typeaheadSearch";

/**
 * The organization's people this tab has found by searching, for an "@" menu
 * to show while its own search is on its way: a session just begun used to
 * show the reader alone until the first search answered, seconds in a
 * starting app (2026-11-07). A page being opened looks once for the people an
 * empty "@" lists, so the first menu has them at once; each menu's answer is
 * kept too.
 */

const KEPT_PEOPLE = 200;
// Each signed-in person's own: a tab that changes account doesn't show one's finds to another.
const keyOf = (userId: string, scope: string) => `${userId}\u0000${scope}`;
const seen = new Map<string, Map<string, OrganizationProfile>>();
const warmed = new Set<string>();

/** Keeps the people a search found, the latest last. */
export function rememberMentionPeople(userId: string, scope: string, people: readonly OrganizationProfile[]) {
  if (!userId || !scope || people.length === 0) return;
  const key = keyOf(userId, scope);
  const known = seen.get(key) ?? new Map<string, OrganizationProfile>();
  for (const person of people) {
    if (!person.userId) continue;
    known.delete(person.userId);
    known.set(person.userId, person);
  }
  while (known.size > KEPT_PEOPLE) known.delete(known.keys().next().value as string);
  seen.set(key, known);
}

/** The people found so far in the organization. */
export function mentionPeopleSeen(userId: string, scope: string): OrganizationProfile[] {
  return userId && scope ? [...(seen.get(keyOf(userId, scope))?.values() ?? [])] : [];
}

/** Looks once a session for the people an empty "@" lists in the organization. */
export function warmMentionPeople(userId: string, organizationId: string | undefined, search: OrganizationPeopleSearchTransport, workspaceId?: string) {
  if (!userId || !organizationId) return;
  const scope = mentionPeopleScope(organizationId, workspaceId);
  const key = keyOf(userId, scope);
  if (warmed.has(key)) return;
  warmed.add(key);
  void search({ organizationId, query: "", limit: 8 }, {}).then(
    (result) => rememberMentionPeople(userId, scope, result.people ?? []),
    () => warmed.delete(key),
  );
}

/**
 * What the people found are kept under: the organization, and the workspace
 * a menu offers the people of, so one workspace's people never stand in for
 * another's (organization_people_workspace.go).
 */
export function mentionPeopleScope(organizationId: string | undefined, workspaceId?: string) {
  return organizationId ? `${organizationId}${workspaceId ? `\u0000${workspaceId}` : ""}` : "";
}
