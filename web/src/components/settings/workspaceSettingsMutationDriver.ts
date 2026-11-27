import { newId } from "@/lib/ids";
import { LeafenTransportError } from "@/apiTransport";
import { sendAfterLostWrite } from "@/lib/settleLostWrite";
import type { Session, Workspace } from "@/types";
import {
  workspaceMembershipErrorCodes,
  type WorkspaceSettingsMutationResponse,
  type WorkspaceSettingsPatch,
} from "@/workspaceMembershipWire.generated";

type SettingsTransport = (
  session: Session,
  workspaceId: string,
  input: {
    requestId: string;
    baseRevision: number;
    patch: WorkspaceSettingsPatch;
  },
  signal?: AbortSignal,
) => Promise<WorkspaceSettingsMutationResponse>;

export type WorkspaceSettingsMutationRequest = {
  session: Session;
  sessionIdentity: string;
  workspaceId: string;
  settingsRevision: number;
  patch: WorkspaceSettingsPatch;
  signal?: AbortSignal;
  isCurrent: () => boolean;
};

type RetryIntent = {
  requestId: string;
  baseRevision: number;
  signature: string;
  patch: WorkspaceSettingsPatch;
};

// A retried request carries its ID again only for the same patch: every field
// it names, as the server weighs the whole patch against that ID
// (workspaceSettingsRequestHash), so a field added to the patch can't slip
// out of it (2026-11-14: the landing page did, and another page chosen after
// a lost answer reused the ID the server then refused).
function patchSignature(patch: WorkspaceSettingsPatch) {
  return JSON.stringify(Object.entries(patch)
    .filter(([, value]) => value !== undefined)
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)));
}

function isRevisionConflict(error: unknown) {
  return (error as { code?: unknown } | null)?.code === workspaceMembershipErrorCodes.settingsRevisionConflict;
}

function obsoleteSettingsMutation() {
  return new DOMException("Workspace settings context changed.", "AbortError");
}

export function createWorkspaceSettingsMutationDriver({
  updateWorkspaceSettings,
  createRequestId = newId,
}: {
  updateWorkspaceSettings: SettingsTransport;
  createRequestId?: () => string;
}) {
  const revisions = new Map<string, number>();
  const retryIntents = new Map<string, RetryIntent>();
  const sameKeyTails = new Map<string, Promise<void>>();

  function keyFor(input: Pick<WorkspaceSettingsMutationRequest, "sessionIdentity" | "workspaceId">) {
    return `${input.sessionIdentity}\u0000${input.workspaceId}`;
  }

  function seed(sessionIdentity: string, workspace: Pick<Workspace, "id" | "settingsRevision">) {
    const key = `${sessionIdentity}\u0000${workspace.id}`;
    const revision = workspace.settingsRevision;
    if (Number.isSafeInteger(revision) && (revision ?? -1) >= 0) {
      revisions.set(key, Math.max(revisions.get(key) ?? 0, revision ?? 0));
    }
  }

  async function mutate(input: WorkspaceSettingsMutationRequest) {
    const key = keyFor(input);
    const signature = patchSignature(input.patch);
    const prior = sameKeyTails.get(key) ?? Promise.resolve();
    let settleTail: Promise<void>;
    const run = prior.catch(() => undefined).then(async () => {
      if (!input.isCurrent() || input.signal?.aborted) throw obsoleteSettingsMutation();
      const send = (sent: RetryIntent) => updateWorkspaceSettings(input.session, input.workspaceId, {
        requestId: sent.requestId,
        baseRevision: sent.baseRevision,
        patch: sent.patch,
      }, input.signal);
      const fresh = (): RetryIntent => ({
        requestId: createRequestId(),
        baseRevision: revisions.get(key) ?? input.settingsRevision,
        signature,
        patch: input.patch,
      });
      const retry = retryIntents.get(key);
      let intent = retry?.signature === signature ? retry : fresh();
      try {
        // One sent just before whose answer was lost settles first (lib/settleLostWrite.ts).
        const response = await sendAfterLostWrite({
          send: () => send(intent),
          lost: retry && retry !== intent ? () => send(retry) : null,
          conflicted: isRevisionConflict,
          forget: () => retryIntents.delete(key),
          again: (settled) => {
            if (!input.isCurrent() || input.signal?.aborted) throw obsoleteSettingsMutation();
            retryIntents.delete(key);
            revisions.set(key, settled.receipt.revision);
            intent = fresh();
            return send(intent);
          },
        });
        if (!input.isCurrent() || input.signal?.aborted) throw obsoleteSettingsMutation();
        revisions.set(key, response.receipt.revision);
        // A change taken leaves none to send again: one sent before it, its
        // answer lost, was taken under it or can no longer be.
        retryIntents.delete(key);
        return response;
      } catch (error) {
        if (
          error instanceof LeafenTransportError
          && input.isCurrent()
          && !input.signal?.aborted
        ) {
          retryIntents.set(key, intent);
        } else if (retryIntents.get(key)?.requestId === intent.requestId) {
          retryIntents.delete(key);
        }
        throw error;
      }
    });
    // All settings fields share one revision, so compatible same-key writes serialize here.
    settleTail = run.then(() => undefined, () => undefined).finally(() => {
      if (sameKeyTails.get(key) === settleTail) sameKeyTails.delete(key);
    });
    sameKeyTails.set(key, settleTail);
    return run;
  }

  return { mutate, seed };
}
