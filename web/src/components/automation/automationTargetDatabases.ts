"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { api } from "@/api";
import { useProductPages } from "@/components/product/productPagesContext";
import { createRelationDatabaseSearch } from "@/lib/relationDatabaseSearch";
import { isServerUnreachable, readUntilServerAnswers } from "@/lib/serverReach";
import type { AutomationAction, DbProperty, Page, PropertyConfig } from "@/lib/types";

export interface AutomationTargetDatabase {
  id: string;
  pageId: string;
  title: string;
  properties: DbProperty[];
  tasks?: boolean;
  templates?: readonly { id: string; name: string }[];
}

/** Each database on another page an action names; null where the person setting it up can't change it. */
export type AutomationTargets = Readonly<Record<string, AutomationTargetDatabase | null>>;

function targetDatabaseIds(actions: readonly AutomationAction[]) {
  const ids = new Set<string>();
  for (const action of actions) {
    if (action.type === "add_page" && action.databaseId) ids.add(action.databaseId);
    if ((action.type === "edit_pages" || action.type === "archive_pages") && action.target.type !== "this_page" && action.target.databaseId) ids.add(action.target.databaseId);
  }
  return [...ids].sort();
}

/**
 * The databases on other pages that actions add to or edit, read with their
 * properties for the values an action sets, and a search of the workspace's
 * databases to choose one from, as Notion's button chooses any database one
 * can edit (seen 2026-10-10). One the server doesn't answer for stays unread
 * till it may (readUntilServerAnswers): taken for one the person can't change,
 * an action read offline lost its database's name after the server was back
 * (2026-11-25).
 */
export function useAutomationTargetDatabases(actions: readonly AutomationAction[], local: readonly Page[], workspaceHint?: string) {
  const product = useProductPages();
  const session = product?.session;
  const workspaceId = product?.workspaceId || workspaceHint || local.find(page => page.workspaceId)?.workspaceId;
  const localIds = useMemo(() => new Set(local.map(page => page.id)), [local]);
  // One on this page too, for its templates and whether it holds tasks; its properties are here already.
  const wanted = targetDatabaseIds(actions).join(",");
  const [targets, setTargets] = useState<Record<string, AutomationTargetDatabase | null>>({});
  const requested = useRef(new Set<string>());
  const alive = useRef(true);
  const reading = useRef<(() => void)[]>([]);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      // What was being read is read again if it shows again.
      for (const stop of reading.current) stop();
      reading.current = [];
      requested.current.clear();
    };
  }, []);
  useEffect(() => {
    if (!session || !workspaceId || !wanted) return;
    for (const id of wanted.split(",")) {
      const key = `${workspaceId}:${id}`;
      if (requested.current.has(key)) continue;
      requested.current.add(key);
      const read = () => api.readAutomationTargetDatabase(session, workspaceId, id).then(target => {
        const properties = target.properties.map((property, position): DbProperty => ({
          id: property.id, databaseId: property.dataSourceId, name: property.name, type: property.type, position,
          config: property.config as PropertyConfig,
        }));
        return { id, pageId: target.pageId, title: target.title, properties, tasks: target.tasks, templates: target.templates };
      }, (error: unknown) => {
        if (isServerUnreachable(error)) throw error;
        return null;
      });
      reading.current.push(readUntilServerAnswers(read, (target) => {
        if (alive.current) setTargets(current => ({ ...current, [id]: target }));
      }));
    }
  }, [session, wanted, workspaceId]);
  const search = useMemo(
    () => session && workspaceId ? createRelationDatabaseSearch(session, workspaceId, { addsRows: true }) : undefined,
    [session, workspaceId],
  );
  const properties = useMemo(
    () => Object.values(targets).flatMap(target => target && !localIds.has(target.id) ? target.properties : []),
    [localIds, targets],
  );
  return { targets: targets as AutomationTargets, properties, search };
}
