export type RequestGenerationContext = {
  principalId: string;
  sessionToken: string;
  route: string;
};

export type CurrentRequestGeneration = {
  signal: AbortSignal;
  isCurrent: () => boolean;
};

export type ReadGenerationContext = RequestGenerationContext;

function sameContext(left: RequestGenerationContext | null, right: RequestGenerationContext): boolean {
  return left?.principalId === right.principalId
    && left.sessionToken === right.sessionToken
    && left.route === right.route;
}

export function createReadGenerationOwner() {
  let context: RequestGenerationContext | null = null;
  let generation = 0;
  let controller: AbortController | null = null;

  const retire = () => {
    generation += 1;
    controller?.abort();
    controller = null;
  };
  const observe = (next: RequestGenerationContext) => {
    if (sameContext(context, next)) return;
    retire();
    context = next;
  };
  const begin = (
    next: RequestGenerationContext,
    currentContext: () => RequestGenerationContext | null = () => context,
  ): CurrentRequestGeneration => {
    observe(next);
    retire();
    controller = new AbortController();
    const current = generation;
    const signal = controller.signal;
    return {
      signal,
      isCurrent: () => current === generation && !signal.aborted && sameContext(currentContext(), next),
    };
  };
  return { begin, observe, retire };
}

export const createRequestGenerationOwner = createReadGenerationOwner;
