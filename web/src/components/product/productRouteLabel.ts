import { i18next } from "@/i18n";
import type { routeInfoFromPath } from "@/lib/router";
import { titleWithClosedMentions } from "@/lib/closedMentions";
import type { TextSpan } from "@/lib/types";

/**
 * What the screen is, for its landmark's name and the "moved to" line a
 * screen reader hears: the page the address names, or the screen itself —
 * an app, the apps, a page not found — never the page opened before
 * (2026-10-16: an app's screen and a missing page were announced as the
 * page left behind). A page still loading is no screen yet: nothing is said
 * until it is shown or known missing (2026-10-27: "찾을 수 없는 페이지로
 * 이동했습니다" came before the page that was loading), and a page that is
 * there but closed to the person is named as that, not as missing.
 */
export function productRouteLabel({ route, trashOpen, pageTitle, pageTitleSpans, workspaceName, listSettled, pageProblem }: {
  route: ReturnType<typeof routeInfoFromPath>;
  /** The trash's address; the settings and trash windows a press opens name themselves (shell/RouteLabel.tsx). */
  trashOpen: boolean;
  /** The title of the page the address names, once it is shown. */
  pageTitle: string | null;
  /** Its spans, which say its mentions as a plain title does (closedMentions.ts). */
  pageTitleSpans?: readonly TextSpan[];
  workspaceName: string | undefined;
  /** Whether the page list has loaded, so a page not in it is missing rather than loading. */
  listSettled: boolean;
  /** What a page not in the list turned out to be, once read. */
  pageProblem: "no-access" | "missing" | undefined;
}) {
  if (trashOpen) return i18next.t("appShell:route.trash");
  // Its mentions said as a plain title says them, one closed to its reader too.
  if (pageTitle) return titleWithClosedMentions(pageTitle, pageTitleSpans);
  switch (route.kind) {
    case "app":
    case "apps":
      return i18next.t("appShell:route.apps");
    case "people":
      return i18next.t("appShell:route.people");
    case "page":
    case "database":
      if (!listSettled || !pageProblem) return "";
      return i18next.t(pageProblem === "no-access" ? "appShell:route.noAccess" : "appShell:route.notFound");
  }
  return workspaceName || i18next.t("sidebar:workspace");
}
