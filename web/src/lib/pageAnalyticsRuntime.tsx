import {
  createContext,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";
import { api } from "@/api";
import { loadSession } from "@/lib/authSessionStore";
import { isPageBusyRefusal } from "@/lib/pageBusy";
import { afterPageShown } from "@/lib/pageSideWork";
import {
  createPageAnalyticsBrowserRuntime,
  type PageAnalyticsBrowserRuntime,
} from "./pageAnalyticsBrowserRuntime";
import {
  publishSessionGenerationReset,
  subscribeSessionGenerationReset,
} from "./sessionGeneration";

const PageAnalyticsRuntimeContext = createContext<PageAnalyticsBrowserRuntime | null>(null);

export function PageAnalyticsRuntimeProvider({ children }: { children: ReactNode }) {
  const [runtime, setRuntime] = useState<PageAnalyticsBrowserRuntime | null>(null);

  useEffect(() => {
    const next = createPageAnalyticsBrowserRuntime({
      // A view is counted once the page is drawn and has read what it shows
      // (pageSideWork.ts); a tab closed before then sends it as it goes, the
      // request outliving the tab (lib/pageLeaving.ts).
      schedule: (send) => { afterPageShown(send); },
      subscribeMemberGenerationReset: subscribeSessionGenerationReset,
      // A visit to a page an operation holds goes again once it's done.
      sendsAgainLater: isPageBusyRefusal,
      transport: (request, options) => api.recordPageAnalytics(
        loadSession(),
        request,
        options,
      ),
    });
    setRuntime(next);
    if (!loadSession()) publishSessionGenerationReset();
    const leaving = () => { void next?.flush().catch(() => undefined); };
    window.addEventListener("pagehide", leaving);
    return () => {
      window.removeEventListener("pagehide", leaving);
      next?.dispose();
    };
  }, []);

  return (
    <PageAnalyticsRuntimeContext.Provider value={runtime}>
      {children}
    </PageAnalyticsRuntimeContext.Provider>
  );
}

export function usePageAnalyticsRuntime() {
  return useContext(PageAnalyticsRuntimeContext);
}
