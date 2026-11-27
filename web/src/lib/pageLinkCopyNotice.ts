import { i18next } from "@/i18n";
import type { ToastAction } from "./toastStackView";
import { publicMutationService } from "./publicMutationService";
import type { Session } from "@/types";

type Notify = (message: string, tone?: "default" | "success" | "error", action?: ToastAction, icon?: "lock") => void;

// Top-bar and share-window copies explain native access. Menus and keyboard
// copies keep the plain confirmation (Notion, 2026-10-09). The share window's
// own copy offers no button, the window being open already (Notion,
// 2026-11-07): `openShare` is left out there. Never infer sole access from a
// paginated roster, or offer management after its authorization fails.
export async function notifyPageLinkCopied(
  session: Session | undefined,
  pageId: string,
  notify: Notify,
  openShare: (() => void) | undefined,
  signal: AbortSignal,
) {
  let onlyReader: boolean | undefined;
  let webLinkOn = false;
  const controller = new AbortController();
  const abort = () => controller.abort();
  if (signal.aborted) return;
  signal.addEventListener("abort", abort, { once: true });
  const timer = setTimeout(abort, 4_000);
  try {
    if (session) {
      const { state } = await publicMutationService.summary(session, pageId, controller.signal);
      if (!controller.signal.aborted) {
        onlyReader = state.onlyReaderOpens === true;
        webLinkOn = state.webSharingEnabled === true;
      }
    }
  } catch {
    // Failed or forbidden reads do not assert an audience or invite access.
  } finally {
    clearTimeout(timer);
    signal.removeEventListener("abort", abort);
  }
  if (signal.aborted) return;
  if (onlyReader !== undefined) {
    // A page open to the web is reached from its web link, not this address:
    // the notice says where that link is, so its owner doesn't think the page
    // went private (2026-11-06).
    const key = webLinkOn ? "topBar:copiedPageLinkWebLinkOn"
      : onlyReader ? "topBar:copiedPrivatePageLink" : "topBar:copiedRestrictedPageLink";
    notify(String(i18next.t(key)), "default", openShare ? {
      label: String(i18next.t(webLinkOn ? "topBar:share" : "topBar:grantPageAccess")),
      dismissible: false,
      onClick: () => { if (!signal.aborted) openShare(); },
    } : undefined, "lock");
  } else {
    notify(String(i18next.t("topBar:copiedPageLink")), "success");
  }
}
