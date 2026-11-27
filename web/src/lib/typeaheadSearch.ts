import type { Organization, OrganizationMember, OrganizationProfile } from "@/lib/types";

export interface SearchOrganizationPeopleInput {
  organizationId: string;
  query?: string;
  limit?: number;
  includeInvited?: boolean;
  includeDeactivated?: boolean;
}

export interface SearchOrganizationPeopleResult {
  organization?: Organization;
  currentOrganizationMember?: OrganizationMember | null;
  query?: string;
  limit?: number;
  people: OrganizationProfile[];
}

export interface SearchOrganizationPeopleRemoteOptions {
  signal?: AbortSignal;
}

export type OrganizationPeopleSearchTransport = (
  input: SearchOrganizationPeopleInput,
  options: SearchOrganizationPeopleRemoteOptions,
) => Promise<SearchOrganizationPeopleResult>;

interface OrganizationPeopleTypeaheadHandlers {
  delayMs?: number;
  search: OrganizationPeopleSearchTransport;
  onError: () => void;
  onResult: (result: SearchOrganizationPeopleResult) => void;
}

export function scheduleOrganizationPeopleTypeahead(
  input: SearchOrganizationPeopleInput,
  handlers: OrganizationPeopleTypeaheadHandlers,
) {
  let active = true;
  const controller = new AbortController();
  const timer = window.setTimeout(() => {
    void handlers.search(input, { signal: controller.signal }).then(
      (result) => {
        if (active) handlers.onResult(result);
      },
      () => {
        if (active) handlers.onError();
      },
    );
  }, handlers.delayMs ?? 120);

  return () => {
    active = false;
    window.clearTimeout(timer);
    controller.abort();
  };
}
