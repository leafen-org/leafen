import type { ProductStatusSink } from "@/lib/productStatus";
import { useEffect, useMemo, useState } from "react";
import i18next from "i18next";
import { api } from "@/api";
import { userErrorText } from "@/lib/settingsErrors";
import type { SuggestingMode } from "@/components/editor/suggestingMode";
import { commentServiceFor } from "@/lib/commentService";
import type { Page, Session } from "@/types";

/**
 * A page's suggesting mode, turned on from its menu by someone who can
 * comment on it. It turns off when the person opens another page, and it
 * isn't offered on a locked page, a page in the trash or a full-page
 * database.
 */
export function usePageSuggesting(input: {
  canSuggest: boolean;
  lockedForMe: boolean;
  inTrash: boolean;
  onStatus: ProductStatusSink;
  page: Page | null;
  session: Session | null;
}) {
  const { canSuggest, lockedForMe, inTrash, onStatus, page, session } = input;
  const [suggestingPageId, setSuggestingPageId] = useState<string | null>(null);
  const available = Boolean(page && session && canSuggest && !lockedForMe && !inTrash && page.kind !== "database");
  const active = available && suggestingPageId === page?.id;
  const pageId = page?.id ?? null;

  useEffect(() => {
    setSuggestingPageId((current) => (current === pageId ? current : null));
  }, [pageId]);

  const offered = available && pageId !== null;
  const mode = useMemo<SuggestingMode>(() => ({
    active,
    onChange: offered ? (on: boolean) => setSuggestingPageId(on ? pageId : null) : undefined,
    async suggest(edit) {
      if (!session || !pageId) return;
      try {
        return await commentServiceFor(session, api).suggest(pageId, edit);
      } catch (error) {
        // A suggestion isn't kept to send later, so even offline it says it failed.
        onStatus(userErrorText(error, i18next.t("commentsPanel:errors.suggestFailed")), "error");
        throw error;
      }
    },
  }), [active, offered, onStatus, pageId, session]);

  const control = available && pageId
    ? { active, onChange: (on: boolean) => setSuggestingPageId(on ? pageId : null) }
    : undefined;
  return { active, control, mode };
}
