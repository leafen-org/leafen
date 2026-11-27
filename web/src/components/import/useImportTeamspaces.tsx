import { Suspense, useState, type ReactNode } from "react";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import { lazyScreen } from "@/lib/lazyScreen";
import { canCreateNativeTeamspace } from "@/lib/nativeWorkspaceRolePolicy";
import type { Session } from "@/types";
import { useProductPages } from "../product/productPagesContext";
import { useTeamspacePlaces } from "../useMovePlaces";
import type { CsvImportPlace, CsvImportTeamspace } from "./CsvImportPlacePicker";

const TeamspaceDialog = lazyScreen(() =>
  import("@/components/TeamspaceDialog").then(({ TeamspaceDialog }) => ({ default: TeamspaceDialog })),
);

/**
 * The teamspaces an import window's "위치 가져오기" lists, and its "+ 새
 * 팀스페이스" for a reader who may make one there, as Notion's CSV and file
 * import windows (seen 2026-11-19 at 1440, dark): the row opens the window
 * that makes a teamspace over the import window, and the teamspace made
 * there becomes the import's place, listed with the others; closed without
 * making one, the place stays.
 */
export function useImportTeamspaces(session: Session, workspaceId: string, onMade: (place: CsvImportPlace) => void): {
  teamspaces: CsvImportTeamspace[];
  /** Opens the window that makes one; none for a reader who may not. */
  onNewTeamspace?: () => void;
  /** That window, while it is open. */
  window: ReactNode;
} {
  const workspace = useProductPages()?.workspaces?.find(({ id }) => id === workspaceId);
  const { places, ownersOnlyCreate, readAgain } = useTeamspacePlaces(session, workspaceId);
  const [making, setMaking] = useState(false);
  const mayMake = Boolean(workspace && !workspace.guest && ownersOnlyCreate !== null
    && canCreateNativeTeamspace(workspace, ownersOnlyCreate));
  return {
    teamspaces: places.filter(({ teamspaceId }) => teamspaceId),
    onNewTeamspace: mayMake ? () => setMaking(true) : undefined,
    window: making && workspace ? (
      <Suspense fallback={null}>
        <ErrorBoundary scope="import-teamspace-dialog">
          <TeamspaceDialog session={session} workspace={workspace} initialView="create"
            onCreated={(made) => {
              onMade({ kind: "teamspace", teamspaceId: made.id, label: made.name, icon: made.icon });
              readAgain();
            }}
            onClose={() => setMaking(false)} />
        </ErrorBoundary>
      </Suspense>
    ) : null,
  };
}
