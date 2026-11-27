import { useState } from "react";
import { i18next } from "@/i18n";
import type { Page } from "@/types";

/**
 * A page's plain text, kept from before its body was blocks, in a field of
 * its own while the page has any. Once shown it stays until another page
 * opens, emptied too, so a text cleared by mistake comes back with ⌘Z
 * (2026-11-13: the field went the moment it was empty, and its undo with it).
 * Key it by the page.
 */
export function PageContentField({ page, readOnly, onChange }: {
  page: Page;
  readOnly: boolean;
  onChange: (content: string) => void;
}) {
  const [shown, setShown] = useState(Boolean(page.content));
  if (page.content && !shown) setShown(true);
  if (!shown && !page.content) return null;
  return (
    <div className="scalar-block">
      <textarea aria-label={i18next.t("syncStatusBadge:status.pageContent")} readOnly={readOnly}
        value={page.content ?? ""} onChange={(event) => onChange(event.target.value)}
        placeholder={i18next.t("syncStatusBadge:status.pageContentPlaceholder")} rows={2} />
    </div>
  );
}
