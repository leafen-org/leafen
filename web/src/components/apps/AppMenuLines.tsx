import { useTranslation } from "react-i18next";
import { appText } from "@/components/admin/administratorAppsApi";
import { useProductPages } from "@/components/product/productPagesContext";
import { ServerAppsIcon } from "@/icons/leafen";
import { appLineScore } from "@/lib/menuSearch";
import { NativeActionMenuItem, NativeMenuSeparator } from "@/lib/nativeActionMenu";
import { useOptionalProductToast } from "@/lib/productToast";
import { runAppMenuAction } from "./appMenuAction";
import { useUsableAppList } from "./useUsableApps";

/**
 * Which of an app's lines a menu's search finds, best first and in the
 * menu's order among equals: each by its name and its app's (appLineScore),
 * as the "/" menu finds an app's item. Untyped, all of them.
 */
export function appLinesFound<T>(query: string, lines: readonly T[], words: (line: T) => string): T[] {
  if (!query.trim().replace(/^\//u, "")) return [...lines];
  return lines.map((line, index) => ({ line, index, score: appLineScore(query, words(line)) }))
    .filter((found) => found.score > 0).sort((a, b) => b.score - a.score || a.index - b.index).map((found) => found.line);
}

/**
 * The lines apps add to a database row's or a page's "…" menu
 * (docs/app-platform.md 10-1): each named as the app names it,
 * with Leafen's "앱" mark and the app's name beside it so no app passes as
 * Leafen or another, running the app's action on that row or page. The
 * menu's search finds them as it finds its own (appLinesFound), after its
 * own rows and with no divider before them, as its own rows stand.
 */
export function AppMenuLines({ menu, targetId, query = "", className, dividerClassName, hintClassName, onDone }: {
  menu: "row" | "page";
  targetId: string;
  /** What the menu's search holds. */
  query?: string;
  className: string;
  dividerClassName: string;
  hintClassName: string;
  /** Closes the menu once a line is chosen. */
  onDone(): void;
}) {
  const { t, i18n } = useTranslation("common");
  const pages = useProductPages();
  const toast = useOptionalProductToast();
  const organizationId = pages?.organizationId;
  const { apps } = useUsableAppList(pages?.session, Boolean(organizationId));
  const lines = appLinesFound(query, apps.filter((app) => app.tenancyKey === `org:${organizationId}`)
    .flatMap((app) => (app.menuActions ?? []).filter((line) => line.on.includes(menu))
      .map((line) => ({ app, line, label: appText(line.name, i18n.language), appName: appText(app.name, i18n.language) }))),
  (line) => `${line.label} ${line.appName}`);
  if (lines.length === 0 || !pages?.session) return null;
  const session = pages.session;
  return (
    <>
      {query.trim() ? null : <NativeMenuSeparator className={dividerClassName} />}
      {lines.map(({ app, line, label, appName }) => (
        <NativeActionMenuItem key={`${app.installId}:${line.key}`} type="button" className={className} data-menu-item role="menuitem"
          data-app-menu-line={`${app.appId}:${line.key}`}
          onClick={() => {
            onDone();
            void runAppMenuAction({
              session, usable: app, action: line.action, label, t, language: i18n.language,
              input: { place: menu === "row" ? "rowMenu" : "pageMenu", pageId: targetId },
              notice: (message, tone) => pages.onStatus?.(message, tone),
              running: (message) => {
                toast?.notify(message, "default", { label: t("appPlace.actionRunningDismiss"), onClick: () => {},
                  autoDismissMs: null, dismissible: false }, "progress");
                return () => toast?.dismissMessage(message);
              },
            });
          }}>
          <ServerAppsIcon size="regular" aria-hidden="true" />
          <span>{label}</span>
          <span className={hintClassName} data-app-menu-mark>{t("appPlace.markNamed", { app: appName })}</span>
        </NativeActionMenuItem>
      ))}
    </>
  );
}
