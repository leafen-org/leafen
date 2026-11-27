import { personLabel, withPersonDistinction } from "@/lib/peopleDirectory";
import { readAllWorkspacePeople } from "@/lib/workspacePeopleDirectory";
import type { Session } from "@/types";

/** Someone an automation or a button may notify. */
export type WorkspacePerson = { id: string; name: string; me: boolean };

/**
 * The workspace's owner and members, every page of them, whom an action's
 * notification may go to; apps and guests, whom the server doesn't notify,
 * aren't among them. The reader comes first, the others after in name order:
 * Notion's automation lists of people — a person condition's, a
 * notification's recipients — put the reader at the head, as its person
 * cell's and person filter's lists do (seen 2026-11-18 in Chrome at 1440;
 * PropertyPeopleCell and DatabaseFilterValueInput put the reader first in
 * their own lists).
 */
export async function loadWorkspacePeople(session: Session, workspaceId: string, signal: AbortSignal): Promise<WorkspacePerson[]> {
  const { people } = await readAllWorkspacePeople(session, workspaceId, signal);
  const listed = people.map((person) => ({
    id: person.userId, name: withPersonDistinction(person.userId, person.displayName.trim() || personLabel(person.userId)),
    me: person.userId === session.user.id,
  }));
  return [...listed.filter((person) => person.me), ...listed.filter((person) => !person.me)];
}
