import type { ReactNode } from "react";
import {
  PageLayoutTabActionsContext, PageLayoutTabEnvironmentContext, PageLayoutTabHolderContext, PageLayoutTabModeContext,
  type PageLayoutTabEnvironment,
} from "./pageLayoutTabEnvironment";
import { PageLayoutTabPillContext } from "./pageLayoutTabPill";
import { FilterThisPageContext } from "./filterThisPage";
import { NewPageExtraContext } from "./tableNewRowExtra";

/**
 * The database's own pages and layout builder show their tabs with its graph
 * and writers, outside any tab's mode and its "이 페이지"; a page around it,
 * whose own database this is not, holds none of its tabs.
 */
export function PageLayoutTabProviders({ environment, children }: { environment: PageLayoutTabEnvironment; children: ReactNode }) {
  return (
    <PageLayoutTabEnvironmentContext.Provider value={environment}>
      <PageLayoutTabHolderContext.Provider value={null}>
        <PageLayoutTabModeContext.Provider value={null}>
          <NewPageExtraContext.Provider value={null}>
            <PageLayoutTabActionsContext.Provider value={null}>
              <PageLayoutTabPillContext.Provider value={null}>
                <FilterThisPageContext.Provider value={null}>{children}</FilterThisPageContext.Provider>
              </PageLayoutTabPillContext.Provider>
            </PageLayoutTabActionsContext.Provider>
          </NewPageExtraContext.Provider>
        </PageLayoutTabModeContext.Provider>
      </PageLayoutTabHolderContext.Provider>
    </PageLayoutTabEnvironmentContext.Provider>
  );
}
