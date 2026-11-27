import { LeafenTransportError } from "@/apiTransport";
import { newId } from "@/lib/ids";
import { sendAfterLostWrite } from "@/lib/settleLostWrite";
import {
  organizationErrorCodes,
  type Organization,
  type OrganizationStorageLimitUpdateInput,
  type OrganizationStorageLimitUpdateResponse,
} from "@/organizationWire.generated";
import type { Session } from "@/types";

type StoragePolicyTransport = (
  session: Session,
  organizationId: string,
  input: OrganizationStorageLimitUpdateInput,
  signal?: AbortSignal,
) => Promise<OrganizationStorageLimitUpdateResponse>;

export type OrganizationStoragePolicyMutationRequest = {
  session: Session;
  sessionIdentity: string;
  organizationId: string;
  policyVersion: number;
  storageLimitBytes: number | null;
  signal?: AbortSignal;
  isCurrent: () => boolean;
};

type RetryIntent = OrganizationStorageLimitUpdateInput & {
  signature: string;
};

function storageLimitSignature(storageLimitBytes: number | null) {
  return JSON.stringify([storageLimitBytes]);
}

function isPolicyConflict(error: unknown) {
  return (error as { code?: unknown } | null)?.code === organizationErrorCodes.storagePolicyConflict;
}

function obsoleteStoragePolicyMutation() {
  return new DOMException("Organization storage-policy context changed.", "AbortError");
}

export function createOrganizationStoragePolicyMutationDriver({
  updateOrganizationStorageLimit,
  createRequestId = newId,
}: {
  updateOrganizationStorageLimit: StoragePolicyTransport;
  createRequestId?: () => string;
}) {
  const revisions = new Map<string, number>();
  const retryIntents = new Map<string, RetryIntent>();
  const sameKeyTails = new Map<string, Promise<void>>();

  function keyFor(input: Pick<
    OrganizationStoragePolicyMutationRequest,
    "sessionIdentity" | "organizationId"
  >) {
    return `${input.sessionIdentity}\u0000${input.organizationId}`;
  }

  function seed(
    sessionIdentity: string,
    organization: Pick<Organization, "id" | "policyVersion">,
  ) {
    if (!Number.isSafeInteger(organization.policyVersion) || organization.policyVersion < 0) return;
    const key = `${sessionIdentity}\u0000${organization.id}`;
    revisions.set(key, Math.max(revisions.get(key) ?? 0, organization.policyVersion));
  }

  async function mutate(input: OrganizationStoragePolicyMutationRequest) {
    const key = keyFor(input);
    const signature = storageLimitSignature(input.storageLimitBytes);
    const prior = sameKeyTails.get(key) ?? Promise.resolve();
    let settleTail: Promise<void>;
    const run = prior.catch(() => undefined).then(async () => {
      if (!input.isCurrent() || input.signal?.aborted) throw obsoleteStoragePolicyMutation();
      const send = (sent: RetryIntent) => updateOrganizationStorageLimit(
        input.session,
        input.organizationId,
        {
          requestId: sent.requestId,
          expectedPolicyVersion: sent.expectedPolicyVersion,
          storageLimitBytes: sent.storageLimitBytes,
        },
        input.signal,
      );
      const fresh = (): RetryIntent => ({
        requestId: createRequestId(),
        expectedPolicyVersion: revisions.get(key) ?? input.policyVersion,
        storageLimitBytes: input.storageLimitBytes,
        signature,
      });
      const retry = retryIntents.get(key);
      let intent: RetryIntent = retry?.signature === signature ? retry : fresh();
      try {
        // One sent just before whose answer was lost settles first (lib/settleLostWrite.ts).
        const response = await sendAfterLostWrite({
          send: () => send(intent),
          lost: retry && retry !== intent ? () => send(retry) : null,
          conflicted: isPolicyConflict,
          forget: () => retryIntents.delete(key),
          again: (settled) => {
            if (!input.isCurrent() || input.signal?.aborted) throw obsoleteStoragePolicyMutation();
            retryIntents.delete(key);
            revisions.set(key, Math.max(revisions.get(key) ?? 0, settled.receipt.policyVersion));
            intent = fresh();
            return send(intent);
          },
        });
        if (!input.isCurrent() || input.signal?.aborted) throw obsoleteStoragePolicyMutation();
        revisions.set(key, Math.max(revisions.get(key) ?? 0, response.receipt.policyVersion));
        retryIntents.delete(key);
        return response;
      } catch (error) {
        if (error instanceof LeafenTransportError && input.isCurrent() && !input.signal?.aborted) {
          retryIntents.set(key, intent);
        } else if (retryIntents.get(key)?.requestId === intent.requestId) {
          retryIntents.delete(key);
        }
        throw error;
      }
    });
    // One policy version owns all storage-limit writes for an organization.
    settleTail = run.then(() => undefined, () => undefined).finally(() => {
      if (sameKeyTails.get(key) === settleTail) sameKeyTails.delete(key);
    });
    sameKeyTails.set(key, settleTail);
    return run;
  }

  return { mutate, seed };
}
