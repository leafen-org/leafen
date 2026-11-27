import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { PublicContentCheckResult, PublicContentSnapshotResult } from "@/publicContentWire.generated";
import { LeafenApiError } from "@/apiTransport";
import { errorStatus } from "./errorReport";
import { isDiagnosticReference } from "./diagnosticTrace";
import { markAppInteractiveForOfflineWarm } from "./appInteractive";
import {
  activePublicContentPage,
  createPublicContentDocument,
  mergePublicContentDocument,
  nextPublicContentRequest,
  type PublicContentDocument,
  type PublicContentWindowRequest,
} from "./publicContentDocument";
import { logProductError } from "./safeErrorLog";
import {
  productErrorReceiptPresentation,
  type ProductErrorContext,
  type ProductErrorPresentation,
} from "./productErrorPresentation";
import { sharedPageErrorKind, type SharedPageErrorKind } from "./sharedPageErrors";

type RequestOptions = { signal?: AbortSignal };

export type PublicContentSnapshotService = {
  snapshot: (
    input?: PublicContentWindowRequest,
    options?: RequestOptions,
  ) => Promise<PublicContentSnapshotResult>;
  /** Whether the page is still public: refused once the link or site is taken away. */
  check?: (pageId: string, options?: RequestOptions) => Promise<PublicContentCheckResult>;
};

/** How often a visitor's open tab asks again whether its page is still public, while it is seen. */
const PUBLIC_CONTENT_CHECK_MS = 30_000;

export type PublicContentFailure = {
  kind: SharedPageErrorKind;
  status: number | null;
  /** The server's error code, as a view link's secret refusal names it. */
  code?: string;
  reference: string;
  phase: "initial" | "page" | "continuation" | "search";
  pageId?: string;
};

export type PublicContentErrorContext = Extract<
  ProductErrorContext,
  `publicContent.${string}`
>;

export function publicContentFailurePresentation(
  failure: PublicContentFailure,
  context: PublicContentErrorContext,
): ProductErrorPresentation {
  const identity = failure.kind === "not-found"
    ? { status: 404 }
    : failure.kind === "rate-limited"
      ? { code: "LEAFEN_RATE_LIMITED", status: 429 }
      : failure.kind === "offline"
        ? { code: "LEAFEN_TRANSPORT_UNAVAILABLE" }
        : { status: failure.status };
  return productErrorReceiptPresentation({
    ...identity,
    reference: failure.reference,
  }, context);
}

export type PublicContentDocumentState =
  | { status: "loading" }
  | { status: "ready"; document: PublicContentDocument }
  | { status: "error"; failure: PublicContentFailure; document?: PublicContentDocument };

function errorReference(error: unknown) {
  let reference: unknown;
  try {
    reference = error && (typeof error === "object" || typeof error === "function")
      ? Reflect.get(error, "reference")
      : undefined;
  } catch {
    reference = undefined;
  }
  if (isDiagnosticReference(reference)) return reference;
  return logProductError("warn", "api.request", error, { causeStage: "public-content" });
}

export function publicContentFailure(
  error: unknown,
  phase: PublicContentFailure["phase"],
  pageId?: string,
): PublicContentFailure {
  return {
    kind: sharedPageErrorKind(error),
    status: errorStatus(error),
    ...(error instanceof LeafenApiError ? { code: error.code } : {}),
    reference: errorReference(error),
    phase,
    pageId,
  };
}

function isAbort(error: unknown) {
  if (!error || (typeof error !== "object" && typeof error !== "function")) return false;
  try {
    return Reflect.get(error, "name") === "AbortError";
  } catch {
    return false;
  }
}

export function usePublicContentDocument(
  service: PublicContentSnapshotService,
  requestedPageId?: string,
  supplementalPageId?: string,
) {
  const [state, setState] = useState<PublicContentDocumentState>({ status: "loading" });
  const [reloadKey, setReloadKey] = useState(0);
  const pendingRequest = useRef("");
  const automaticRevisionReloads = useRef(0);

  useEffect(() => {
    automaticRevisionReloads.current = 0;
  }, [service]);

  useEffect(() => {
    const controller = new AbortController();
    pendingRequest.current = "";
    setState({ status: "loading" });
    service.snapshot({}, { signal: controller.signal }).then((snapshot) => {
      if (controller.signal.aborted) return;
      setState({ status: "ready", document: createPublicContentDocument(snapshot) });
      markAppInteractiveForOfflineWarm();
    }).catch((error) => {
      if (controller.signal.aborted || isAbort(error)) return;
      setState({ status: "error", failure: publicContentFailure(error, "initial") });
    });
    return () => controller.abort();
  }, [reloadKey, service]);

  useEffect(() => {
    if (state.status !== "ready") return;
    const request = nextPublicContentRequest(
      state.document,
      requestedPageId,
      supplementalPageId ? [supplementalPageId] : [],
    );
    if (!request) return;
    const requestKey = JSON.stringify(request);
    if (pendingRequest.current === requestKey) return;
    pendingRequest.current = requestKey;
    const controller = new AbortController();
    const phase = state.document.documents[request.pageId ?? ""] ? "continuation" : "page";
    service.snapshot(request, { signal: controller.signal }).then((snapshot) => {
      if (controller.signal.aborted) return;
      pendingRequest.current = "";
      setState((current) => current.status === "ready"
        ? {
            status: "ready",
            document: mergePublicContentDocument(current.document, snapshot, request),
          }
        : current);
    }).catch((error) => {
      if (controller.signal.aborted || isAbort(error)) return;
      pendingRequest.current = "";
      if (errorStatus(error) === 409 && automaticRevisionReloads.current < 1) {
        automaticRevisionReloads.current += 1;
        setReloadKey((key) => key + 1);
        return;
      }
      setState({
        status: "error",
        document: state.document,
        failure: publicContentFailure(error, phase, request.pageId),
      });
    });
    return () => controller.abort();
  }, [requestedPageId, service, state, supplementalPageId]);

  // A visitor's open tab asks again whether what it shows is still public —
  // every PUBLIC_CONTENT_CHECK_MS while it is seen, and as it comes back into
  // view — so a link removed or expired, or a site unpublished, takes the page
  // away as Notion's does, instead of showing it on (2026-11-06). The tab
  // then shows what opening the address again would.
  const shownPageId = state.status === "ready" ? requestedPageId || state.document.rootPageId : "";
  useEffect(() => {
    const check = service.check;
    if (!shownPageId || !check) return undefined;
    let asking: AbortController | null = null;
    const ask = () => {
      if (document.visibilityState !== "visible" || asking) return;
      const current = new AbortController();
      asking = current;
      check(shownPageId, { signal: current.signal }).catch((error) => {
        if (current.signal.aborted || isAbort(error)) return;
        const status = errorStatus(error);
        if (status === 401 || status === 403 || status === 404) {
          setState({ status: "error", failure: publicContentFailure(error, "initial") });
        }
      }).finally(() => {
        if (asking === current) asking = null;
      });
    };
    const onSeen = () => {
      if (document.visibilityState === "visible") ask();
    };
    const timer = window.setInterval(ask, PUBLIC_CONTENT_CHECK_MS);
    document.addEventListener("visibilitychange", onSeen);
    window.addEventListener("focus", onSeen);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", onSeen);
      window.removeEventListener("focus", onSeen);
      asking?.abort();
    };
  }, [service, shownPageId]);

  const reload = useCallback(() => {
    automaticRevisionReloads.current = 0;
    setReloadKey((key) => key + 1);
  }, []);
  const page = useMemo(
    () => state.status === "ready"
      ? activePublicContentPage(state.document, requestedPageId)
      : undefined,
    [requestedPageId, state],
  );
  return { state, page, reload };
}
