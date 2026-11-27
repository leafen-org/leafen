import { useEffect, useMemo, useState } from "react";
import { TemplateEditingBanner } from "@/components/database/TemplateEditingBanner";
import { inlineDatabasePlaceholderTitle } from "@/components/editor/databaseTitles";
import type { DatabaseToolbarConfigMetadataResponse, DatabaseToolbarTemplateRepeatWire } from "@/databaseToolbarConfigWire.generated";
import { createDatabaseToolbarConfigService } from "@/lib/databaseToolbarConfigService";
import { newId } from "@/lib/ids";
import { pageLookup } from "@/lib/pageArchive";
import { knownPathTemplate, pathLeavesOwnPages } from "@/lib/pagePathPieces";
import { usePagePathsAbove } from "@/lib/usePagePathsAbove";
import type { Page, Session } from "@/types";
import { ButtonTemplateBanner, inButtonTemplate } from "./ButtonTemplateBanner";

/**
 * The band over a page that is, or sits under, a draft rather than a page of
 * its own: a button's (ButtonTemplateBanner), or a database template's — a
 * page under a template, opened whole, draws the template's own band,
 * "다음에서 템플릿을 편집 중입니다. 〈DB〉 ⓘ 복제 주기 ⌄", as Notion's sub-page
 * of a template and its sub-page do (seen 2026-11-16, Chrome 1440). The
 * template is known from the path the server reads above the page
 * (page_path_route.go), which the page's bar reads too.
 */
export function PageTemplateBands({ page, pages, session, viewOnly }: {
  page: Page;
  pages: readonly Page[];
  session?: Session | null;
  /** The reader may only read the page: "다음에서 템플릿을 보고 있습니다.", without the repeat. */
  viewOnly: boolean;
}) {
  const lookup = useMemo(() => pageLookup(pages), [pages]);
  const inButton = inButtonTemplate(page, lookup);
  // Only a page whose path leaves the reader's pages can be under a template, which no page list holds.
  const leaves = !inButton && Boolean(session) && pathLeavesOwnPages(page, lookup);
  const pathsRead = usePagePathsAbove(leaves ? [page] : []);
  // The paths read so far name it.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const template = useMemo(() => leaves ? knownPathTemplate(page.id) : undefined, [leaves, page.id, pathsRead]);
  const repeat = useTemplateRepeat(viewOnly ? null : session ?? null, template);
  if (inButton) return <ButtonTemplateBanner />;
  if (!template) return null;
  const database = template.database;
  // An untitled one by its kind's name, as the template's own band names it (templateBandDatabase).
  return <TemplateEditingBanner databaseTitle={database?.title.trim() || inlineDatabasePlaceholderTitle()} viewing={viewOnly} repeat={repeat}
    databaseIcon={database ? { icon: database.icon, iconType: database.iconType, workspaceId: page.workspaceId } : undefined} />;
}

/**
 * The template's repeat as its database's settings hold it, and its change,
 * as the template's own band offers them: read once the template is known,
 * none while unread or for one who may only read it.
 */
function useTemplateRepeat(session: Session | null, template: ReturnType<typeof knownPathTemplate>) {
  const [configuration, setConfiguration] = useState<DatabaseToolbarConfigMetadataResponse | null>(null);
  const service = useMemo(() => session ? createDatabaseToolbarConfigService(session, setConfiguration) : null, [session]);
  const pageId = template?.database?.pageId, dataSourceId = template?.dataSourceId;
  useEffect(() => {
    if (!service || !pageId || !dataSourceId) return undefined;
    void service.refresh(pageId, dataSourceId).catch(() => undefined);
    return () => service.dispose();
  }, [dataSourceId, pageId, service]);
  const shown = configuration?.templates.find(({ id, deleted }) => id === template?.templateId && !deleted);
  if (!service || !pageId || !dataSourceId || !shown) return undefined;
  return {
    template: shown,
    onSave: (repeat: DatabaseToolbarTemplateRepeatWire | null) => {
      const current = service.snapshot();
      if (!current) return;
      void service.mutate(pageId, dataSourceId, {
        requestId: newId(), expectedWorkspaceContentRevision: current.workspaceContentRevision,
        operations: [{ kind: "setTemplateRepeat", templateId: shown.id, expectedRevision: shown.templateAuthorityRevision, repeat }],
      }).catch(() => undefined).then(() => service.refresh(pageId, dataSourceId)).catch(() => undefined);
    },
  };
}
