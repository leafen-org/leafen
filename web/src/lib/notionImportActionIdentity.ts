export type NotionImportActionKind = "connection" | "job";

export type NotionImportActionAttempt = {
  requestId: string;
  signal: AbortSignal;
  isCurrent: () => boolean;
};

type ActionSlot = {
  controller: AbortController | null;
  generation: number;
  requestId: string;
};

export function createNotionImportActionIdentityOwner(
  createRequestId: () => string,
) {
  const slots: Record<NotionImportActionKind, ActionSlot> = {
    connection: { controller: null, generation: 0, requestId: "" },
    job: { controller: null, generation: 0, requestId: "" },
  };

  function invalidate(kind: NotionImportActionKind) {
    const slot = slots[kind];
    slot.controller?.abort();
    slot.controller = null;
    slot.generation += 1;
    slot.requestId = "";
  }

  function begin(kind: NotionImportActionKind): NotionImportActionAttempt {
    const slot = slots[kind];
    slot.controller?.abort();
    const controller = new AbortController();
    slot.controller = controller;
    slot.generation += 1;
    slot.requestId ||= createRequestId();
    const generation = slot.generation;
    return {
      requestId: slot.requestId,
      signal: controller.signal,
      isCurrent: () => (
        slot.controller === controller
        && slot.generation === generation
        && !controller.signal.aborted
      ),
    };
  }

  return {
    begin,
    invalidate,
    reset: () => {
      invalidate("connection");
      invalidate("job");
    },
  };
}
