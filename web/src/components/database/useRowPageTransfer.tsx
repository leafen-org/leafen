"use client";

import { Suspense, useState, type ReactNode } from "react";
import { lazyScreen } from "@/lib/lazyScreen";
import { useTranslation } from "react-i18next";
import { useProductPages } from "@/components/product/productPagesContext";
import { useProductToast } from "@/lib/productToast";
import type { Session } from "@/types";
import type { PageExportChoice, PageExportFormat } from "../PageExportDialog";
import { withLoadDeadline } from "@/lib/lazyScreen";

const PageExportDialog = lazyScreen(() => (
  import("../PageExportDialog").then(({ PageExportDialog: Dialog }) => ({ default: Dialog }))
));

// A database page exports in Notion's formats; its database restores it.
const ROW_PAGE_EXPORT_FORMATS: readonly PageExportFormat[] = ["pdf", "html", "markdown_csv"];

/**
 * A database page's export and import, from its own … menu, as a page's: the
 * export shows the page as its database does, with its title, properties and
 * body, and the import screen leaves files for the person's agent, naming
 * the page as where they go.
 */
export function useRowPageTransfer({ rowId, title, workspaceId, session, readOnly }: {
  rowId: string;
  title: string;
  workspaceId?: string;
  session?: Session;
  /** The page takes no imported pages. */
  readOnly: boolean;
}): { onExport?: () => void; onImport?: () => void; surface: ReactNode } {
  const { t } = useTranslation(["pageExport"]);
  const { notify } = useProductToast();
  const openImport = useProductPages()?.openImport;
  const [exportOpen, setExportOpen] = useState(false);
  if (!session || !workspaceId) return { surface: null };
  const target = { id: rowId, title, workspaceId };

  async function exportPage(choice: PageExportChoice, signal: AbortSignal) {
    const adapter = await withLoadDeadline(import("../nativeExport"));
    signal.throwIfAborted();
    const context = { session: session!, signal };
    if (choice.format === "pdf") await adapter.printPage(target, choice, context);
    else if (choice.format === "html" || choice.format === "markdown_csv") {
      await adapter.exportPageDocument(target, { ...choice, format: choice.format }, context);
    }
    // A file comes down as Notion's does, its window closing with no notice (seen 2026-11-07); a PDF says its print window opened.
    if (choice.format === "pdf") notify(t("pageExport:printing"), "success");
  }

  return {
    onExport: () => setExportOpen(true),
    onImport: readOnly || !openImport ? undefined : () => openImport({ pageId: rowId, title }),
    surface: exportOpen ? (
      <Suspense fallback={null}>
        <PageExportDialog formats={ROW_PAGE_EXPORT_FORMATS} policyFor={{ session, workspaceId, pageId: rowId }}
          onClose={() => setExportOpen(false)} onExport={exportPage} />
      </Suspense>
    ) : null,
  };
}
