import { useEffect, useRef } from "react";

interface DisposableResource {
  dispose(): Promise<void> | void;
}

interface PendingDisposal<Resource extends DisposableResource> {
  cancelled: boolean;
  resource: Resource;
}

export function useCommittedResourceDisposal<Resource extends DisposableResource>(
  resource: Resource,
): void {
  const pendingRef = useRef<PendingDisposal<Resource> | null>(null);

  useEffect(() => {
    const pending = pendingRef.current;
    if (pending?.resource === resource) {
      pending.cancelled = true;
      pendingRef.current = null;
    }
    return () => {
      const request = { cancelled: false, resource };
      pendingRef.current = request;
      queueMicrotask(() => {
        if (pendingRef.current === request) pendingRef.current = null;
        if (!request.cancelled) void resource.dispose();
      });
    };
  }, [resource]);
}
