import { useContext, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import type { ProductPages } from "@/components/product/productPagesContext";
import { useProductPagesSelect } from "@/components/product/productPagesStore";
import { isServerUnreachable, readUntilServerAnswers } from "@/lib/serverReach";
import type { Session } from "@/types";
import { relationSession } from "./relationPageOrder";
import { DatabaseRowTemplateNameContext, DatabaseTaskRowsContext } from "./untitledRowName";

type TargetRowName = { template: string; tasks: boolean };

const known = new Map<string, Promise<TargetRowName>>();

/**
 * What a relation's target database calls its rows without a name, which its
 * picker and chips show: its default template's name, as Notion's picker
 * shows a task tracker's "신규 작업" (seen 2026-11-07), else "새 작업" in a
 * task database (seen 2026-11-03), else nothing of its own (pageDisplayTitle).
 * Read once a row without a name is to be shown, and only by someone who can
 * edit that database; a relation to its own database names them as its view does.
 * Read while the server doesn't answer, it is read again once the server may
 * (readUntilServerAnswers): kept as no name, a chip read offline named its row
 * plainly till the app was opened again (2026-11-25).
 */
export function useRelationTargetRowName(targetId: string, ownId: string, needed: boolean): string {
  const { t } = useTranslation("databaseView");
  const ownTasks = useContext(DatabaseTaskRowsContext), ownTemplate = useContext(DatabaseRowTemplateNameContext);
  const session = useProductPagesSelect(relationSession), workspaceId = useProductPagesSelect(productWorkspaceId);
  const [target, setTarget] = useState<TargetRowName>({ template: "", tasks: false });
  useEffect(() => {
    if (!needed || !session || !workspaceId || !targetId || targetId === ownId) return undefined;
    return readUntilServerAnswers(() => targetRowName(session, workspaceId, targetId), setTarget);
  }, [needed, ownId, session, targetId, workspaceId]);
  const { template, tasks } = targetId === ownId ? { template: ownTemplate, tasks: ownTasks } : target;
  return template || (tasks ? t("newTask") : "");
}

/** A target database's name for its rows, read once for the app; one the server didn't answer isn't kept. */
function targetRowName(session: Session, workspaceId: string, targetId: string) {
  const key = `${workspaceId}:${targetId}`;
  let read = known.get(key);
  if (!read) {
    read = api.readAutomationTargetDatabase(session, workspaceId, targetId).then(
      (database) => ({ template: database.untitledRowName?.trim() ?? "", tasks: database.tasks === true }),
      (error: unknown) => {
        if (!isServerUnreachable(error)) return { template: "", tasks: false };
        known.delete(key);
        throw error;
      },
    );
    known.set(key, read);
  }
  return read;
}

function productWorkspaceId(pages: ProductPages | null) {
  return pages?.workspaceId;
}
