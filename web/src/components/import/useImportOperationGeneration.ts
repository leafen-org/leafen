import { useCallback, useEffect, useMemo } from "react";

type ImportOperationGenerationContext = {
  principalId: string;
  sessionToken: string;
  workspaceId: string;
};

type OperationGenerationOwner = {
  controller: AbortController | null;
  generation: number;
};

type BeginOperationOptions = {
  controller?: AbortController;
  isContextCurrent?: () => boolean;
};

function retire(owner: OperationGenerationOwner) {
  owner.generation += 1;
  owner.controller?.abort();
  owner.controller = null;
}

export function useImportOperationGeneration(
  context: ImportOperationGenerationContext,
) {
  const owner = useMemo<OperationGenerationOwner>(() => ({
    controller: null,
    generation: 0,
  }), []);

  useEffect(() => () => retire(owner), [
    context.principalId,
    context.sessionToken,
    context.workspaceId,
    owner,
  ]);

  const begin = useCallback((options: BeginOperationOptions = {}) => {
    owner.controller?.abort();
    owner.controller = options.controller ?? null;
    const generation = ++owner.generation;
    const isCurrent = () => (
      owner.generation === generation
      && owner.controller === (options.controller ?? null)
      && !options.controller?.signal.aborted
      && (options.isContextCurrent?.() ?? true)
    );
    return {
      isCurrent,
      release() {
        if (isCurrent()) owner.controller = null;
      },
    };
  }, [owner]);

  const supersede = useCallback((options: Omit<BeginOperationOptions, "controller"> = {}) => {
    retire(owner);
    return begin(options);
  }, [begin, owner]);

  return { begin, supersede };
}
