import { useMemo, type DependencyList } from "react";
import { useCommittedResourceDisposal } from "./useCommittedResourceDisposal";

/** An operation scope survives an effect replay and expires with its view. */
export function useCommittedOperationScope(dependencies: DependencyList) {
  const scope = useMemo(() => {
    const controller = new AbortController();
    return { signal: controller.signal, dispose: () => controller.abort() };
  }, dependencies);
  useCommittedResourceDisposal(scope);
  return scope;
}
