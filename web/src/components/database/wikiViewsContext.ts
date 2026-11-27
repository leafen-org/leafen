import { createContext } from "react";
import type { GraphViewType } from "@/contentGraph";
import { NOTION_2023_VIEW_TYPES } from "./databaseViewShared";

// A wiki's "새 보기" offers no form (Notion's, seen 2026-11-11).
export const WIKI_ADD_VIEW_TYPES: GraphViewType[] = NOTION_2023_VIEW_TYPES.map(({ type }) => type).filter((type) => type !== "form");

/** What the wiki's Home asked of its database's views before they showed. */
export type WikiViewsRequest = { create: GraphViewType } | { viewId: string; action: string };

/**
 * A wiki's database block shows the wiki's views, as Notion's (seen
 * 2026-11-11): one picker on its toolbar's row, the wiki's Home in it at its place.
 */
export type WikiViews = {
  homeLabel: string;
  /** How many views stand before Home. */
  homeIndex: number;
  chooseHome(): void;
  /** Moves a row of the views list before or after another, Home's (`home`) among them. */
  reorder(sourceId: string, targetId: string, side: "before" | "after"): void;
  /** Opens Home's settings, where its name changes. */
  renameHome(): void;
  /** Takes the picker's press first; true keeps the views closed. */
  onTrigger?(): boolean;
  /** The Home's request that brought the views up, once. */
  takeRequest(): WikiViewsRequest | null;
};

export const WikiViewsContext = createContext<WikiViews | null>(null);
