import type { ProductStatusSink } from "@/lib/productStatus";
import { i18next } from "@/i18n";
import { api } from "@/api";
import { upsertBootstrapPage } from "@/lib/bootstrapPageProjection";
import { newId } from "@/lib/ids";
import { createNativePageMutationOutboxBrowser } from "@/lib/nativePageMutationBrowser";
import { nativePageMutationFailure } from "@/lib/nativePageMutationFailure";
import type { Bootstrap, Page, Session } from "@/types";
import { keptChangeStatus } from "@/lib/serverAnswers";

type PageMutationOutbox = ReturnType<
  typeof createNativePageMutationOutboxBrowser<Awaited<ReturnType<typeof api.updatePage>>>
>;

/**
 * The signed-in product's page changes: a page the server hands back goes
 * into the page list, and a page's plain text goes through the page mutation
 * outbox, kept for later when the server can't be reached. This device's copy
 * of the list takes both from the list itself, which keeps itself once it
 * changed (useAuthenticatedBootstrap): read again and written whole here
 * besides, a 47,000-page copy (37 MB) was decrypted, parsed and sealed again
 * for each page a menu changed (2026-11-15).
 */
export function authenticatedPageChanges({
  session,
  setData,
  pageMutationOutbox,
  setMessage,
}: {
  session: Session;
  setData: (update: (current: Bootstrap) => Bootstrap) => void;
  pageMutationOutbox: PageMutationOutbox;
  setMessage: ProductStatusSink;
}) {
  function applyPageProjection(page: Page): void {
    setData((current) => upsertBootstrapPage(current, page));
  }

  async function changePage(page: Page, content: string) {
    const mutationId = newId();
    setData((current) => ({ ...current, pages: current.pages.map((item) => item.id === page.id ? { ...item, content } : item) }));
    try {
      const input = {
        baseCursor: page.cursor,
        content,
        createdAt: Date.now(),
        mutationId,
        pageId: page.id,
      };
      const delivery = pageMutationOutbox
        ? await pageMutationOutbox.admitAndSend(input)
        : { result: await api.updatePage(
            session,
            page.id,
            content,
            mutationId,
            page.cursor,
          ), state: "committed" as const };
      // A newer text written meanwhile replaced it: that one's answer tells.
      if (delivery.state === "superseded") return;
      if (delivery.state === "deferred") {
        setMessage(i18next.t(keptChangeStatus()));
        return;
      }
      const result = delivery.result;
      // What was typed after this text stays on the screen, and goes next on
      // the cursor this answer gave (lib/nativePageTextChain.ts).
      const answered = (item: Page): Page => ({
        ...result.page, hasContent: result.page.hasContent ?? item.hasContent,
        ...(item.content === content ? {} : { content: item.content }),
      });
      setData((current) => ({ ...current, cursor: result.receipt.cursor, pages: current.pages.map((item) => item.id === page.id ? answered(item) : item) }));
      setMessage(i18next.t("syncStatusBadge:status.saved"));
    } catch (error) {
      setMessage(nativePageMutationFailure(error, "pageMutation.admission"), "error");
    }
  }

  return { applyPageProjection, changePage };
}
