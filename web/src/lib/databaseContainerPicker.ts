import type { GraphViewType } from "@/contentGraph";
import type { DatabaseSourceTitle } from "@/components/editor/databaseTitles";

// The view to copy into a new linked database, which owns its views.
export type DatabaseContainerSource = {
  id: string;
  pageId: string;
  pageTitle: string;
  /** Its page's kind, which names it while untitled (untitledPageDisplayTitle); a source made here has none yet. */
  pageKind?: string;
  viewBlock?: boolean;
  dataSourceId: string;
  viewId: string;
  viewType: GraphViewType;
  viewName: string;
  viewConfig: Record<string, unknown>;
  title: string;
  /** Every source's title, in its database's order, while it holds more than one. */
  sourceTitles?: DatabaseSourceTitle[];
  /** An inline database's own name while it holds several, none while it has none (inlineDatabaseName). */
  databaseTitle?: string;
  previewProperties?: import("@/contentGraph").GraphProperty[];
  /** The source's own icon, none where it has none (DataSourceMark). */
  icon?: string;
  iconType?: import("@/contentGraph").GraphDataSource["iconType"];
};

export type DatabaseContainerPickerResponse = {
  items: DatabaseContainerSource[];
  nextCursor?: string;
};
