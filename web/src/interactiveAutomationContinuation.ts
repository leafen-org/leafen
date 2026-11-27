import type { InteractiveAutomationExecuteInput, InteractiveAutomationExecuteResponse } from "./interactiveAutomationWire.generated";

function waitForTurn(signal?: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    if (signal?.aborted) { reject(signal.reason); return; }
    const abort = () => { clearTimeout(timer); reject(signal?.reason); };
    const timer = setTimeout(() => { signal?.removeEventListener("abort", abort); resolve(); }, 200);
    signal?.addEventListener("abort", abort, { once: true });
  });
}

/** Server turns are bounded; a run has no arbitrary total row/time limit. */
export async function continueInteractiveAutomation(
  request: (input: InteractiveAutomationExecuteInput) => Promise<InteractiveAutomationExecuteResponse>,
  input: InteractiveAutomationExecuteInput, signal?: AbortSignal,
  onProgress?: (response: InteractiveAutomationExecuteResponse) => Promise<void>,
) {
  let next = input;
  const blocks = new Map<string, { id: string }>(), pages = new Map<string, { id: string }>(), edited = new Map<string, { id: string }>();
  // Whether every row the turns made or changed is a task, for the run's notice.
  let tasks = true, touched = false;
  const outcomes: NonNullable<InteractiveAutomationExecuteResponse["outcomes"]> = [];
  let canUndo = false;
  for (;;) {
    const response = await request(next);
    if (!response.pending) {
      if (onProgress) return response;
      const lastTouches = Boolean(response.effects?.pages?.length || response.effects?.edited?.length);
      return {
        ...response, canUndo: canUndo || response.canUndo,
        effects: {
          blocks: [...blocks.values(), ...(response.effects?.blocks ?? [])], pages: [...pages.values(), ...(response.effects?.pages ?? [])],
          edited: [...edited.values(), ...(response.effects?.edited ?? []).filter(({ id }) => !edited.has(id))],
          tasks: (touched || lastTouches) && tasks && (!lastTouches || response.effects?.tasks === true),
        },
        outcomes: [...outcomes, ...(response.outcomes ?? [])],
      };
    }
    if (!response.continuationToken || response.confirmation) throw new Error("Invalid automation continuation.");
    if (onProgress) await onProgress(response);
    else {
      for (const block of response.effects?.blocks ?? []) blocks.set(block.id, block);
      for (const page of response.effects?.pages ?? []) pages.set(page.id, page);
      for (const page of response.effects?.edited ?? []) edited.set(page.id, page);
      if (response.effects?.pages?.length || response.effects?.edited?.length) {
        touched = true;
        tasks &&= response.effects.tasks === true;
      }
      if (!response.receipt?.duplicate) outcomes.push(...(response.outcomes ?? []));
      canUndo ||= response.canUndo === true;
    }
    next = { executionId: input.executionId, continuationToken: response.continuationToken };
    await waitForTurn(signal);
  }
}
