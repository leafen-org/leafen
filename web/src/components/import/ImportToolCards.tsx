import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { ServerAppsIcon, Search } from "@/icons/leafen";
import { pushRoute } from "@/lib/router";
import { appText } from "../admin/administratorAppsApi";
import { useUsableAppList } from "../apps/useUsableApps";
import { useProductPages } from "../product/productPagesContext";
import type { UsableApp, UsableAppImportSource } from "../../appPlatformWire.generated";
import type { Session } from "../../types";
import { ImportSourceCard } from "./ImportBrowse";
import { ToolImportWindow } from "./ToolImportWindow";
import { clearLeftBundlePlaces } from "./useToolImport";

// "다른 도구에서 가져오기" (decision "설계 우선 Notion 이사" 7절 4):
// a card for each tool an installed app moves content from, named as the app
// names it with Leafen's "앱 · 〈앱〉" mark, and one more to find such apps.
// As the cards show, the places earlier moves from these tools left behind go.

export function ImportToolCards({ session, workspaceId, onNavigate = pushRoute }: {
  session: Session;
  workspaceId: string;
  /** Leaves the import screen for another place in Leafen. */
  onNavigate?: (path: string) => void;
}) {
  const { t, i18n } = useTranslation(["importDialog", "common"]);
  const pages = useProductPages();
  const organizationId = pages?.organizationId;
  const { apps } = useUsableAppList(session, Boolean(organizationId));
  const [open, setOpen] = useState<{ usable: UsableApp; source: UsableAppImportSource } | null>(null);
  const tools = apps.filter((app) => app.tenancyKey === `org:${organizationId}`)
    .flatMap((usable) => (usable.importSources ?? []).map((source) => ({ usable, source })));
  const toolKeys = tools.map(({ usable, source }) => `${usable.appId}:${source.key}`).join("\n");
  // Once as the cards first show for the workspace, before any card's window
  // takes a bundle: a session renewed later doesn't read the places again
  // while a window's move is under way.
  const sessionRef = useRef(session);
  sessionRef.current = session;
  const cleared = useRef("");
  useEffect(() => {
    if (!toolKeys || cleared.current === workspaceId) return;
    cleared.current = workspaceId;
    void clearLeftBundlePlaces(sessionRef.current, workspaceId, new Set(toolKeys.split("\n"))).catch(() => undefined);
  }, [toolKeys, workspaceId]);
  return (
    <>
      {tools.map(({ usable, source }) => (
        <ImportSourceCard key={`${usable.installId}:${source.key}`} icon={<ServerAppsIcon size="regular" aria-hidden="true" />}
          title={appText(source.name, i18n.language)} text={appText(source.description, i18n.language)}
          mark={t("common:appPlace.markNamed", { app: appText(usable.name, i18n.language) })}
          data={{ "data-import-tool-card": `${usable.appId}:${source.key}` }} onOpen={() => setOpen({ usable, source })} />
      ))}
      <ImportSourceCard icon={<Search size="regular" aria-hidden="true" />} title={t("importDialog:tools.findTitle")}
        text={tools.length ? t("importDialog:tools.findMore") : t("importDialog:tools.findFirst")}
        data={{ "data-import-tool-card": "find" }} onOpen={() => onNavigate("/apps")} />
      {open ? <ToolImportWindow usable={open.usable} source={open.source} session={session} workspaceId={workspaceId}
        onClose={() => setOpen(null)} /> : null}
    </>
  );
}
