import { useMemo } from "react";
import { appText } from "@/components/admin/administratorAppsApi";
import { useUsableAppList } from "@/components/apps/useUsableApps";
import { useProductPages } from "@/components/product/productPagesContext";
import { i18next } from "@/i18n";
import type { BlockDef } from "./blockDefinitions";

/**
 * The "/" menu's items for the apps the person may use in the page's
 * organization (docs/app-platform.md 2·10-1), under "앱" with the app's name
 * beside each: its blocks, each making an embed whose service is "app", and
 * the items it declares — one of its blocks under the item's name, or one of
 * its actions, run where the item was chosen. A block an item puts in is
 * listed by that item alone, its keywords with it, so each block is one line,
 * as Notion's "/" menu (2026-11-09: 결재's "결재 현황" stood twice). It also
 * says whether the list is still being read.
 */
export function useAppBlockDefs(enabled: boolean): { defs: BlockDef[]; reading: boolean } {
  const pages = useProductPages();
  const organizationId = pages?.organizationId;
  const asked = enabled && Boolean(organizationId) && Boolean(pages?.session);
  const { apps, loaded } = useUsableAppList(pages?.session, asked);
  const defs = useMemo(() => {
    const language = i18next.language;
    const seen = new Set<string>();
    const defs: BlockDef[] = [];
    for (const app of apps) {
      if (app.tenancyKey !== `org:${organizationId}` || seen.has(app.appId)) continue;
      seen.add(app.appId);
      const name = appText(app.name, language);
      const blockContent = (key: string, title: string) => ({ service: "app", app: app.appId, block: key, name: title });
      const itemBlocks = new Set((app.slashItems ?? []).map((item) => item.block).filter(Boolean));
      for (const block of app.blocks ?? []) {
        if (itemBlocks.has(block.key)) continue;
        const title = appText(block.name, language);
        defs.push({
          id: `app:${app.appId}:${block.key}`, type: "embed", label: title, description: `A block of ${name}.`, glyph: "▣",
          group: "Apps", keywords: ["app", name.toLowerCase()], koKeywords: ["앱"],
          content: blockContent(block.key, title), appPlatform: true, app: { name, title },
        });
      }
      for (const item of app.slashItems ?? []) {
        const title = appText(item.name, language);
        const block = item.block ? app.blocks?.find((candidate) => candidate.key === item.block) : undefined;
        if (item.block && !block) continue;
        defs.push({
          id: `app-item:${app.appId}:${item.key}`, type: "embed", label: title, description: `${title} from ${name}.`, glyph: "▣",
          group: "Apps", keywords: [...(item.keywords ?? []).map((word) => word.toLowerCase()), "app", name.toLowerCase()], koKeywords: ["앱"],
          appPlatform: true, app: { name, title, keywords: item.keywords ?? [] },
          ...(block ? { content: blockContent(block.key, appText(block.name, language)) } : { appAction: { usable: app, action: item.action ?? "" } }),
        });
      }
    }
    return defs;
  }, [apps, organizationId]);
  // Until the list has been read its rows aren't "none" yet (SlashMenu's close).
  return { defs, reading: asked && !loaded };
}
