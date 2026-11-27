import {
  productErrorMessage,
  type ProductErrorContext,
} from "@/lib/productErrorPresentation";
import type {
  PageAnalyticsPreferences,
  PageAnalyticsReadResult,
  PageAnalyticsUpdatePolicyRequest,
  PageAnalyticsUpdatePreferencesRequest,
} from "../pageAnalyticsWire.generated";
import type { PageAnalyticsTarget } from "../pageAnalyticsApi";

export type {
  PageAnalyticsPreferences,
  PageAnalyticsReadResult,
} from "../pageAnalyticsWire.generated";

export interface PageAnalyticsPanelService {
  /** A database row page, or an inline database opened alone, reads its analytics through the page holding it. */
  getAnalytics(pageId: string, rangeDays: number, target?: PageAnalyticsTarget): Promise<PageAnalyticsReadResult>;
  /** A database row page's, or an inline database's, own choice, with the page holding it. */
  getPreferences(
    workspaceId: string,
    pageId: string,
    target?: PageAnalyticsTarget,
  ): Promise<PageAnalyticsPreferences>;
  updatePreferences(
    input: Omit<PageAnalyticsUpdatePreferencesRequest, "action">,
  ): Promise<unknown>;
  updatePolicy(
    input: Omit<PageAnalyticsUpdatePolicyRequest, "action">,
  ): Promise<unknown>;
}

export const PAGE_ANALYTICS_FAILURE_STAGES = [
  "analytics.read",
  "preferences.read",
  "preferences.update",
  "policy.update",
] as const;

export type PageAnalyticsFailureStage =
  typeof PAGE_ANALYTICS_FAILURE_STAGES[number];

const pageAnalyticsFailureContexts = {
  "analytics.read": "pageAnalytics.analyticsRead",
  "preferences.read": "pageAnalytics.preferencesRead",
  "preferences.update": "pageAnalytics.preferencesUpdate",
  "policy.update": "pageAnalytics.policyUpdate",
} as const satisfies Record<PageAnalyticsFailureStage, ProductErrorContext>;

export function pageAnalyticsFailureMessage(
  error: unknown,
  stage: PageAnalyticsFailureStage,
) {
  return productErrorMessage(error, pageAnalyticsFailureContexts[stage]);
}

/**
 * The ranges an analytics screen offers: none past what the organization
 * keeps (0 keeps them all), the shortest always.
 */
export function keptAnalyticsRanges<Range extends number>(ranges: readonly Range[], retentionDays: number): Range[] {
  const kept = ranges.filter((days) => retentionDays <= 0 || days <= retentionDays);
  return kept.length ? kept : ranges.slice(0, 1);
}
