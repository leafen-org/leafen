"use client";

import type { ReactNode } from "react";
import type { GraphViewType } from "@/contentGraph";

export type DatabaseViewRenderers = Record<GraphViewType, ReactNode> & {
  TableView: ReactNode;
  BoardView: ReactNode;
  GalleryView: ReactNode;
  ListView: ReactNode;
  CalendarView: ReactNode;
  TimelineView: ReactNode;
  ChartView: ReactNode;
  FormView: ReactNode;
  DashboardView: ReactNode;
  FeedView: ReactNode;
  MapView: ReactNode;
};

export function DatabaseViewRenderer({
  activeType,
  renderers,
}: {
  activeType: GraphViewType;
  renderers: DatabaseViewRenderers;
}) {
  if (activeType === "table") return <>{renderers.TableView ?? renderers.table}</>;
  if (activeType === "board") return <>{renderers.BoardView ?? renderers.board}</>;
  if (activeType === "gallery") return <>{renderers.GalleryView ?? renderers.gallery}</>;
  if (activeType === "list") return <>{renderers.ListView ?? renderers.list}</>;
  if (activeType === "calendar") return <>{renderers.CalendarView ?? renderers.calendar}</>;
  if (activeType === "timeline") return <>{renderers.TimelineView ?? renderers.timeline}</>;
  if (activeType === "chart") return <>{renderers.ChartView ?? renderers.chart}</>;
  if (activeType === "form") return <>{renderers.FormView ?? renderers.form}</>;
  if (activeType === "dashboard") return <>{renderers.DashboardView ?? renderers.dashboard}</>;
  if (activeType === "feed") return <>{renderers.FeedView ?? renderers.feed}</>;
  if (activeType === "map") return <>{renderers.MapView ?? renderers.map}</>;
  return null;
}
