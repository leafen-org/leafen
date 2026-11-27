import {
  pagePermanentDeleteErrorCodes,
  type PagePermanentDeleteResponse,
} from "@/pagePermanentDeleteWire.generated";

export const nativeTrashLifecycleErrorCodes = {
  responseInvalid: "LEAFEN_TRASH_LIFECYCLE_RESPONSE_INVALID",
} as const;

/** Whether a permanent delete was refused because an active legal hold keeps it. */
export function isLegalHoldPermanentDeleteError(error: unknown) {
  if (!error || typeof error !== "object") return false;
  const record = error as Record<string, unknown>;
  return record.code === pagePermanentDeleteErrorCodes.legalHold || record.status === 412;
}

type RequestInput = { requestId: string };
type LifecycleRequest<T> = (input: RequestInput, signal?: AbortSignal) => Promise<T>;

function abortError() {
  return new DOMException("Trash lifecycle aborted.", "AbortError");
}

function responseError() {
  return Object.assign(new Error("Trash lifecycle returned an invalid response."), {
    code: nativeTrashLifecycleErrorCodes.responseInvalid,
  });
}

function nonnegativeInteger(value: number) {
  return Number.isSafeInteger(value) && value >= 0;
}

function assertNotAborted(signal?: AbortSignal) {
  if (signal?.aborted) throw abortError();
}

const deletePhases = ["discovering", "authorizing", "cleaning", "publishing", "completed"];

export async function drainPagePermanentDelete({
  pageId,
  requestId,
  request,
  signal,
}: {
  pageId: string;
  requestId: string;
  request: LifecycleRequest<PagePermanentDeleteResponse>;
  signal?: AbortSignal;
}) {
  let jobId = "";
  let previousRevision = 0;
  let previousDiscovered = 0;
  let previousAuthorized = 0;
  let previousCleaned = 0;
  let previousPhase = -1;
  for (;;) {
    assertNotAborted(signal);
    const response = await request({ requestId }, signal);
    assertNotAborted(signal);
    const phase = deletePhases.indexOf(response.phase);
    const progress = response.progress;
    if (
      !response.jobId
      || (jobId && response.jobId !== jobId)
      || response.rootPageId !== pageId
      || (response.status !== "pending" && response.status !== "completed")
      || phase < previousPhase
      || phase < 0
      || !nonnegativeInteger(progress.revision)
      || progress.revision <= previousRevision
      || !nonnegativeInteger(progress.discovered)
      || !nonnegativeInteger(progress.authorized)
      || !nonnegativeInteger(progress.cleaned)
      || progress.discovered < previousDiscovered
      || progress.authorized < previousAuthorized
      || progress.cleaned < previousCleaned
      || progress.authorized > progress.discovered
      || progress.cleaned > progress.authorized
      || response.counts.pages !== progress.discovered
      || (response.status === "completed") !== (response.phase === "completed")
      || typeof response.duplicate !== "boolean"
      || typeof response.alreadyDeleted !== "boolean"
    ) throw responseError();
    jobId = response.jobId;
    previousRevision = progress.revision;
    previousDiscovered = progress.discovered;
    previousAuthorized = progress.authorized;
    previousCleaned = progress.cleaned;
    previousPhase = phase;
    if (response.status === "completed") return response;
  }
}
