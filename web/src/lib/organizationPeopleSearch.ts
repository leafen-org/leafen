import type { OrganizationProfile } from "@/lib/types";

/** How a people search marks an address invited that no account proves yet (people_value_invitees.go). */
export const INVITED_PERSON_STATUS = "invited";

export interface OrganizationPeopleSearchInput {
  organizationId: string;
  query?: string;
  limit?: number;
  /** Only people, leaving out the accounts installed apps act as. */
  peopleOnly?: boolean;
  /** Only the people of this workspace of the organization, whom its person pickers offer. */
  workspaceId?: string;
  /**
   * With `workspaceId`, after its people the addresses it invited that no
   * account proves yet, as a person picker and an "@" menu offer them
   * (status "invited", named by the address).
   */
  invitees?: boolean;
}

export interface OrganizationPeopleSearchResult {
  organizationId: string;
  query: string;
  limit: number;
  people: Array<OrganizationProfile & { revision: number }>;
}

export interface OrganizationPeopleSearchTransportOptions {
  signal?: AbortSignal;
}

export type OrganizationPeopleSearchRemote = (
  input: OrganizationPeopleSearchInput,
  options: OrganizationPeopleSearchTransportOptions,
) => Promise<OrganizationPeopleSearchResult>;

interface OrganizationPeopleSearchFlight {
  consumers: number;
  controller: AbortController;
  promise: Promise<OrganizationPeopleSearchResult>;
  settled: boolean;
}

const maximumResults = 50;

function resultLimit(value: number | undefined) {
  if (typeof value !== "number" || !Number.isFinite(value)) return 8;
  return Math.max(1, Math.min(maximumResults, Math.floor(value)));
}

function abortReason(signal?: AbortSignal) {
  return signal?.reason
    ?? new DOMException("Organization people search was aborted.", "AbortError");
}

export function createOrganizationPeopleSearchTransport(
  remote: OrganizationPeopleSearchRemote,
) {
  const flights = new Map<string, OrganizationPeopleSearchFlight>();

  return function searchOrganizationPeople(
    input: OrganizationPeopleSearchInput,
    options: OrganizationPeopleSearchTransportOptions = {},
  ): Promise<OrganizationPeopleSearchResult> {
    if (options.signal?.aborted) return Promise.reject(abortReason(options.signal));
    const normalizedQuery = input.query?.trim().toLowerCase() ?? "";
    const callerLimit = resultLimit(input.limit);
    const key = JSON.stringify([input.organizationId, normalizedQuery, input.peopleOnly === true, input.workspaceId ?? "", input.invitees === true]);
    let flight = flights.get(key);
    if (!flight) {
      const controller = new AbortController();
      const promise = remote({
        organizationId: input.organizationId,
        query: normalizedQuery,
        limit: maximumResults,
        ...(input.peopleOnly ? { peopleOnly: true } : {}),
        ...(input.workspaceId ? { workspaceId: input.workspaceId } : {}),
        ...(input.invitees ? { invitees: true } : {}),
      }, { signal: controller.signal });
      const created: OrganizationPeopleSearchFlight = {
        consumers: 0,
        controller,
        promise,
        settled: false,
      };
      flight = created;
      flights.set(key, created);
      void promise.then(
        () => {
          created.settled = true;
          if (flights.get(key) === created) flights.delete(key);
        },
        () => {
          created.settled = true;
          if (flights.get(key) === created) flights.delete(key);
        },
      );
    }

    flight.consumers += 1;
    const shared = flight;
    return new Promise((resolve, reject) => {
      let active = true;
      const release = () => {
        if (!active) return;
        active = false;
        options.signal?.removeEventListener("abort", onAbort);
        shared.consumers -= 1;
        if (shared.consumers === 0 && !shared.settled) {
          if (flights.get(key) === shared) flights.delete(key);
          shared.controller.abort();
        }
      };
      const onAbort = () => {
        const reason = abortReason(options.signal);
        release();
        reject(reason);
      };
      options.signal?.addEventListener("abort", onAbort, { once: true });
      shared.promise.then(
        (result) => {
          if (!active) return;
          release();
          resolve({
            ...result,
            query: normalizedQuery,
            limit: callerLimit,
            people: result.people.slice(0, callerLimit),
          });
        },
        (error) => {
          if (!active) return;
          release();
          reject(error);
        },
      );
    });
  };
}
