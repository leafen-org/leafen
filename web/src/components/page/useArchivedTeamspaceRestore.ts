import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { LeafenApiError } from "@/apiTransport";
import { teamspaceRestoreLock } from "@/components/teamspace/teamspaceDialogModel";
import { newId } from "@/lib/ids";
import { useProductToast } from "@/lib/productToast";
import { requestTeamspacesRead, type WorkspaceTeamspaces } from "@/lib/workspaceTeamspaces";
import { createTeamspaceApi } from "@/teamspaceApi";
import { teamspaceErrorCodes, type TeamspaceArchivedPlace } from "@/teamspaceWire.generated";
import type { Session } from "@/types";

/**
 * "팀스페이스 복원" for an archived teamspace whose pages the reader still
 * opens — the band over such a page and its path's teamspace piece: only its
 * owners bring it back, and while no default teamspace is in use only an
 * open one, as the settings' (teamspaceRestoreLock). Permission changes are
 * the server's to decide first; the press shows at once as busy.
 */
export function useArchivedTeamspaceRestore(session: Session, workspaceId: string,
  place: TeamspaceArchivedPlace | undefined, kept: WorkspaceTeamspaces | null) {
  const { t } = useTranslation("teamspaceDialog");
  const { notify } = useProductToast();
  const [busy, setBusy] = useState(false);
  const operationRef = useRef<AbortController | null>(null);
  const placeId = place?.id ?? "";

  useEffect(() => () => operationRef.current?.abort(), []);
  useEffect(() => {
    operationRef.current?.abort();
    operationRef.current = null;
    setBusy(false);
  }, [placeId]);

  const lock = place ? teamspaceRestoreLock({ access: place.access, membershipRole: place.canRestore ? "owner" : undefined },
    { defaultTeamspaceIds: (kept?.teamspaces ?? []).filter((item) => item.isDefault).map((item) => item.id) }) : "notOwner";

  async function restore() {
    if (!place || busy || lock) return;
    const archived = place;
    const controller = new AbortController();
    operationRef.current = controller;
    setBusy(true);
    try {
      await createTeamspaceApi().mutateTeamspace(session, workspaceId, {
        action: "restore", teamspaceId: archived.id, expectedRevision: archived.revision, requestId: newId(),
      }, controller.signal);
      if (controller.signal.aborted) return;
      notify(t("notice.restored", { name: archived.name }), "success");
      requestTeamspacesRead(workspaceId);
    } catch (cause) {
      if (controller.signal.aborted) return;
      const code = cause instanceof LeafenApiError ? cause.code : "";
      notify(t(code === teamspaceErrorCodes.defaultRequired ? "error.defaultRequired"
        : code === teamspaceErrorCodes.conflict ? "error.conflict" : "error.action"), "error");
    } finally {
      if (operationRef.current === controller) {
        operationRef.current = null;
        setBusy(false);
      }
    }
  }

  /** Why it is dim, in the reader's words; "" when it may be pressed. */
  const lockReason = lock ? t(lock === "noDefault" ? "restoreNoDefault" : "restoreNotOwner") : "";
  return { busy, lock, lockReason, restore };
}
