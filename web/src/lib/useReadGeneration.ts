import { useCallback, useEffect, useMemo, useRef } from "react";

import {
  createRequestGenerationOwner,
  type RequestGenerationContext,
} from "./readGeneration";

export function useRequestGeneration(context: RequestGenerationContext) {
  const contextRef = useRef(context);
  contextRef.current = context;
  const owner = useMemo(createRequestGenerationOwner, []);

  useEffect(() => {
    owner.observe(context);
    return () => owner.retire();
  }, [context.principalId, context.route, context.sessionToken, owner]);

  return useCallback(
    () => owner.begin(contextRef.current, () => contextRef.current),
    [owner],
  );
}

export const useReadGeneration = useRequestGeneration;
