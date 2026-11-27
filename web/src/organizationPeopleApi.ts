import type { Session } from "./types";
import {
  organizationOperations,
  type OrganizationPeopleResponse,
} from "./organizationWire.generated";
import type {
  OrganizationPeopleSearchInput,
  OrganizationPeopleSearchResult,
  OrganizationPeopleSearchTransportOptions,
} from "./lib/organizationPeopleSearch";
import { createOrganizationPeopleSearchTransport, INVITED_PERSON_STATUS } from "./lib/organizationPeopleSearch";
import { addLookedUpPeople, directoryPeopleCount } from "./lib/peopleDirectory";

type Request = <T>(
  path: string,
  init?: RequestInit,
  session?: Session | null,
) => Promise<T>;

export function createOrganizationPeopleApi(request: Request) {
  const transports = new WeakMap<Session, ReturnType<typeof createOrganizationPeopleSearchTransport>>();

  function transportFor(session: Session) {
    const existing = transports.get(session);
    if (existing) return existing;
    const transport = createOrganizationPeopleSearchTransport(async (input, options) => {
      const query = new URLSearchParams({
        q: input.query?.trim().toLowerCase() ?? "",
        limit: String(input.limit ?? 8),
        ...(input.peopleOnly ? { kind: "people" } : {}),
        ...(input.workspaceId ? { workspace: input.workspaceId } : {}),
        ...(input.invitees ? { invitees: "1" } : {}),
      });
      const response = await request<OrganizationPeopleResponse>(
        `${organizationOperations.searchPeople.path.replace(
          "{organizationId}",
          encodeURIComponent(input.organizationId),
        )}?${query}`,
        { method: organizationOperations.searchPeople.method, signal: options.signal },
        session,
      );
      // Someone invited is named and pictured by the address wherever a value
      // or mention picked from the list names them.
      const invited = response.people.filter((person) => (person.status as string) === INVITED_PERSON_STATUS);
      if (invited.length) {
        addLookedUpPeople(directoryPeopleCount() + invited.length,
          invited.map((person) => ({ userId: person.userId, email: person.email, invitee: true })));
      }
      return {
        organizationId: response.organizationId,
        query: response.query,
        limit: response.limit,
        people: response.people.map((person) => ({
          userId: person.userId,
          displayName: person.displayName,
          email: person.email,
          avatar: person.avatar,
          organizationRole: person.organizationRole,
          status: person.status,
          revision: person.revision,
          workspaceMemberships: [],
          pendingInvitations: [],
        })),
      };
    });
    transports.set(session, transport);
    return transport;
  }

  return {
    searchOrganizationPeople(
      session: Session,
      input: OrganizationPeopleSearchInput,
      options: OrganizationPeopleSearchTransportOptions = {},
    ): Promise<OrganizationPeopleSearchResult> {
      return transportFor(session)(input, options);
    },
  };
}
