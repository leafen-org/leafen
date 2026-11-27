import type {
  PageFavoriteMutationInput,
  PageFavoriteMutationResponse,
  PageFavoriteProjection,
} from "@/workspaceMembershipWire.generated";

type PageFavoriteServiceOptions = {
  identity: string;
  currentIdentity(): string;
  publish(state: PageFavoriteProjection): void;
  read(pageId: string): PageFavoriteProjection | undefined;
  reconcile?(pageId: string, error: unknown): Promise<void> | void;
  requestId(): string;
  send(
    pageId: string,
    input: PageFavoriteMutationInput,
    signal: AbortSignal,
  ): Promise<PageFavoriteMutationResponse>;
};

type PendingFavorite = {
  input: PageFavoriteMutationInput;
  previous: PageFavoriteProjection;
};

function uncertainFailure(error: unknown) {
  return error instanceof Error
    && (error.name === "LeafenTransportError" || error.name === "AbortError");
}

export function createPageFavoriteService(options: PageFavoriteServiceOptions) {
  const ownerIdentity = options.identity;
  const inflightByPage = new Map<string, Promise<PageFavoriteProjection | null>>();
  const acknowledgedByPage = new Map<string, PageFavoriteProjection>();
  const pendingByPage = new Map<string, PendingFavorite>();
  const controllers = new Set<AbortController>();
  let requestGeneration = 0;

  function isCurrent(generation: number, controller: AbortController) {
    return (
      ownerIdentity === options.currentIdentity()
      && generation === requestGeneration
      && !controller.signal.aborted
    );
  }

  function setFavorite(
    pageId: string,
    favorite: boolean,
  ): Promise<PageFavoriteProjection | null> {
    const inflight = inflightByPage.get(pageId);
    if (inflight) {
      const current = pendingByPage.get(pageId);
      if (current?.input.favorite === favorite) return inflight;
      return inflight.then(() => setFavorite(pageId, favorite));
    }

    const ownerState = options.read(pageId);
    const acknowledged = acknowledgedByPage.get(pageId);
    const stored = acknowledged && (!ownerState || acknowledged.revision >= ownerState.revision)
      ? acknowledged
      : ownerState ?? { pageId, favorite: false, revision: 0 };
    const pending = pendingByPage.get(pageId);
    if (!pending && stored.favorite === favorite) return Promise.resolve(stored);
    const reusable = pending
      && pending.input.favorite === favorite
      && pending.input.expectedRevision === stored.revision
      ? pending
      : null;
    const operation = reusable ?? {
      input: {
        requestId: options.requestId(),
        favorite,
        expectedRevision: stored.revision,
      },
      previous: stored,
    };
    pendingByPage.set(pageId, operation);
    options.publish({ ...stored, favorite });

    const generation = requestGeneration;
    const controller = new AbortController();
    controllers.add(controller);
    const promise = options.send(pageId, operation.input, controller.signal)
      .then((response) => {
        pendingByPage.delete(pageId);
        if (!isCurrent(generation, controller)) return null;
        const state = {
          pageId: response.pageId,
          favorite: response.favorite,
          revision: response.revision,
        };
        acknowledgedByPage.set(pageId, state);
        options.publish(state);
        return state;
      })
      .catch(async (error: unknown) => {
        const uncertain = uncertainFailure(error);
        if (!uncertain) pendingByPage.delete(pageId);
        if (isCurrent(generation, controller)) {
          options.publish(operation.previous);
          if (!uncertain) await options.reconcile?.(pageId, error);
        }
        throw error;
      })
      .finally(() => {
        controllers.delete(controller);
        if (inflightByPage.get(pageId) === promise) inflightByPage.delete(pageId);
      });
    inflightByPage.set(pageId, promise);
    return promise;
  }

  return Object.freeze({
    identity: ownerIdentity,
    setFavorite,
    dispose() {
      requestGeneration += 1;
      for (const controller of controllers) controller.abort();
      controllers.clear();
      inflightByPage.clear();
      acknowledgedByPage.clear();
      pendingByPage.clear();
    },
  });
}
