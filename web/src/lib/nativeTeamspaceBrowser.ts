import { newId } from "@/lib/ids";
import { createTeamspaceApi } from "@/teamspaceApi";
import type { Session } from "@/types";
import type {
  TeamspaceManagementResponse,
  TeamspaceMutationInput,
  TeamspaceMutationResponse,
  TeamspaceSnapshotResponse,
} from "@/teamspaceWire.generated";

type TeamspaceBrowserContext = {
  generation: number;
  session: Session;
  workspaceId: string;
  /** 설정 › 팀스페이스's read: every active teamspace for the workspace's owner. */
  everyTeamspace?: boolean;
};

type TeamspaceBrowserOptions = {
  getContext: () => TeamspaceBrowserContext;
};

type ReadLane<T> = {
  key: string;
  context: TeamspaceBrowserContext;
  controller: AbortController;
  promise: Promise<T | null>;
};

function sameContext(left: TeamspaceBrowserContext, right: TeamspaceBrowserContext) {
  return left.generation === right.generation
    && left.workspaceId === right.workspaceId
    && left.everyTeamspace === right.everyTeamspace
    && left.session.token === right.session.token
    && left.session.user.id === right.session.user.id;
}

export function createNativeTeamspaceBrowser({ getContext }: TeamspaceBrowserOptions) {
  const api = createTeamspaceApi();
  let disposed = false;
  let snapshotLane: ReadLane<TeamspaceSnapshotResponse> | null = null;
  let archivedLane: ReadLane<TeamspaceSnapshotResponse> | null = null;
  let managementLane: ReadLane<TeamspaceManagementResponse> | null = null;
  const mutationControllers = new Set<AbortController>();

  function current(context: TeamspaceBrowserContext, controller: AbortController) {
    return !disposed && !controller.signal.aborted && sameContext(context, getContext());
  }

  function startRead<T>(
    lane: ReadLane<T> | null,
    key: string,
    run: (context: TeamspaceBrowserContext, signal: AbortSignal) => Promise<T>,
    publish: (next: ReadLane<T> | null) => void,
  ) {
    const context = getContext();
    if (lane && lane.key === key && sameContext(lane.context, context) && !lane.controller.signal.aborted) {
      return lane.promise;
    }
    lane?.controller.abort();
    const controller = new AbortController();
    const next = {} as ReadLane<T>;
    next.key = key;
    next.context = context;
    next.controller = controller;
    next.promise = run(context, controller.signal).then(
      (value) => current(context, controller) && laneCurrent(next) ? value : null,
      (cause) => {
        if (controller.signal.aborted || !current(context, controller) || !laneCurrent(next)) return null;
        throw cause;
      },
    ).finally(() => {
      if (laneCurrent(next)) publish(null);
    });
    function laneCurrent(candidate: ReadLane<T>) {
      return candidate === snapshotLane || candidate === archivedLane || candidate === managementLane;
    }
    publish(next);
    return next.promise;
  }

  return {
    readSnapshot(cursor?: string) {
      return startRead(
        snapshotLane,
        `snapshot:${cursor ?? ""}`,
        (context, signal) => api.readTeamspaceSnapshot(context.session, context.workspaceId, cursor, signal, context.everyTeamspace),
        (next) => { snapshotLane = next; },
      );
    },
    readArchived(cursor?: string) {
      return startRead(
        archivedLane,
        `archived:${cursor ?? ""}`,
        (context, signal) => api.readArchivedTeamspaces(context.session, context.workspaceId, cursor, signal),
        (next) => { archivedLane = next; },
      );
    },
    readManagement(
      teamspaceId: string,
      cursors: { memberCursor?: string; requestCursor?: string; principalCursor?: string } = {},
    ) {
      const key = `management:${teamspaceId}:${cursors.memberCursor ?? ""}:${cursors.requestCursor ?? ""}:${cursors.principalCursor ?? ""}`;
      return startRead(
        managementLane,
        key,
        (context, signal) => api.readTeamspaceManagement(
          context.session,
          context.workspaceId,
          teamspaceId,
          cursors,
          signal,
        ),
        (next) => { managementLane = next; },
      );
    },
    cancelArchived() {
      archivedLane?.controller.abort();
      archivedLane = null;
    },
    cancelSnapshot() {
      snapshotLane?.controller.abort();
      snapshotLane = null;
    },
    cancelManagement() {
      managementLane?.controller.abort();
      managementLane = null;
    },
    mutate(input: Omit<TeamspaceMutationInput, "requestId">): Promise<TeamspaceMutationResponse | null> {
      const context = getContext();
      const controller = new AbortController();
      const request = Object.assign(
        Object.create(Object.getPrototypeOf(input)) as TeamspaceMutationInput,
        input,
        { requestId: newId() },
      );
      mutationControllers.add(controller);
      return api.mutateTeamspace(
        context.session,
        context.workspaceId,
        request,
        controller.signal,
      ).then(
        (value) => current(context, controller) ? value : null,
        (cause) => {
          if (controller.signal.aborted || !current(context, controller)) return null;
          throw cause;
        },
      ).finally(() => {
        controller.abort();
        mutationControllers.delete(controller);
      });
    },
    dispose() {
      disposed = true;
      snapshotLane?.controller.abort();
      archivedLane?.controller.abort();
      managementLane?.controller.abort();
      for (const controller of mutationControllers) controller.abort();
    },
  };
}
