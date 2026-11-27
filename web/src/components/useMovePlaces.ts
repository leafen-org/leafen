import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { DialogTeamspace } from "@/components/teamspace/teamspaceDialogModel";
import { createNativeTeamspaceBrowser } from "@/lib/nativeTeamspaceBrowser";
import { canAddTeamspacePages, readWorkspaceTeamspaces } from "@/lib/workspaceTeamspaces";
import type { Session } from "@/types";
import type { MovePlace } from "./pageMoveDialogModel";

/**
 * The tops a page moves to: the reader's private pages and the teamspaces
 * they may add pages to.
 */
export function useMovePlaces(session: Session, workspaceId: string): MovePlace[] {
  return useTeamspacePlaces(session, workspaceId).places;
}

/**
 * The tops a page goes to (useMovePlaces), with whether only the workspace's
 * owners make teamspaces there (null until the teamspaces are read) and a
 * way to read them again, once a teamspace was made where they are listed.
 */
export function useTeamspacePlaces(session: Session, workspaceId: string) {
  const { t } = useTranslation("moveToDialog");
  const [read, setRead] = useState<{ teamspaces: DialogTeamspace[]; ownersOnlyCreate: boolean | null }>({ teamspaces: [], ownersOnlyCreate: null });
  const [generation, setGeneration] = useState(0);
  useEffect(() => {
    if (!workspaceId) return;
    const context = { generation: generation + 1, session, workspaceId };
    const browser = createNativeTeamspaceBrowser({ getContext: () => context });
    let current = true;
    readWorkspaceTeamspaces(browser).then(
      (answer) => {
        if (current && answer) setRead({ teamspaces: answer.teamspaces, ownersOnlyCreate: answer.ownersOnlyCreate });
      },
      () => {
        // The private pages stay a place without the teamspaces.
      },
    );
    return () => {
      current = false;
      browser.dispose();
    };
  }, [generation, session, workspaceId]);
  const places = useMemo<MovePlace[]>(() => [
    { teamspaceId: "", label: t("moveToDialog:private.label"), path: t("moveToDialog:private.path") },
    ...read.teamspaces.filter(canAddTeamspacePages).map((teamspace) => ({
      teamspaceId: teamspace.id,
      label: teamspace.name,
      path: t("moveToDialog:teamspace.path"),
      icon: teamspace.icon,
    })),
  ], [t, read.teamspaces]);
  const readAgain = useCallback(() => setGeneration((value) => value + 1), []);
  return { places, ownersOnlyCreate: read.ownersOnlyCreate, readAgain };
}
