export type RecordCacheBudgetContinuationDomain = "block" | "database";
export type RecordCacheBudgetContinuationTurn = "complete" | "pending";
export type RecordCacheBudgetContinuationAdmission =
  | "coalesced"
  | "refused"
  | "scheduled";

export interface RecordCacheBudgetContinuationOptions {
  defer(callback: () => void): void;
  maxPendingUsers: number;
  maxUserIdCharacters: number;
  onError(error: unknown): void;
  runTurn(
    userId: string,
    domain: RecordCacheBudgetContinuationDomain,
  ): Promise<RecordCacheBudgetContinuationTurn>;
}

export interface RecordCacheBudgetContinuation {
  dispose(): void;
  idle(): Promise<void>;
  idle(
    userId: string,
    domain: RecordCacheBudgetContinuationDomain,
  ): Promise<void>;
  schedule(
    userId: string,
    domain: RecordCacheBudgetContinuationDomain,
  ): RecordCacheBudgetContinuationAdmission;
}

interface LaneState {
  domain: RecordCacheBudgetContinuationDomain;
  idlePromise: Promise<void> | null;
  phase: "running" | "scheduled";
  requestedWhileRunning: boolean;
  resolveIdle: (() => void) | null;
  userId: string;
}

type UserLanes = Partial<Record<RecordCacheBudgetContinuationDomain, LaneState>>;

function assertPositiveSafeInteger(value: number, label: string): void {
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new RangeError(`${label} must be a positive safe integer.`);
  }
}

function assertUserId(value: string, maximum: number): void {
  if (typeof value !== "string" || value.length === 0 || value.length > maximum) {
    throw new RangeError("Record-cache continuation user identity is invalid.");
  }
}

function assertDomain(
  value: string,
): asserts value is RecordCacheBudgetContinuationDomain {
  if (value !== "block" && value !== "database") {
    throw new RangeError("Record-cache continuation domain is invalid.");
  }
}

function assertTurn(value: string): asserts value is RecordCacheBudgetContinuationTurn {
  if (value !== "complete" && value !== "pending") {
    throw new Error("Record-cache continuation returned an invalid turn result.");
  }
}

export function createRecordCacheBudgetContinuation(
  options: RecordCacheBudgetContinuationOptions,
): RecordCacheBudgetContinuation {
  assertPositiveSafeInteger(options.maxPendingUsers, "maxPendingUsers");
  assertPositiveSafeInteger(options.maxUserIdCharacters, "maxUserIdCharacters");

  const users = new Map<string, UserLanes>();
  let disposed = false;
  let generation = 0;
  let idlePromise: Promise<void> | null = null;
  let resolveIdle: (() => void) | null = null;

  const settleIdle = (): void => {
    if (users.size !== 0 || !resolveIdle) return;
    const resolve = resolveIdle;
    resolveIdle = null;
    idlePromise = null;
    resolve();
  };

  const isCurrent = (lane: LaneState, token: number): boolean => (
    !disposed
    && token === generation
    && users.get(lane.userId)?.[lane.domain] === lane
  );

  const settleLaneIdle = (lane: LaneState): void => {
    const resolve = lane.resolveIdle;
    lane.idlePromise = null;
    lane.resolveIdle = null;
    resolve?.();
  };

  const finish = (lane: LaneState): void => {
    const lanes = users.get(lane.userId);
    if (lanes?.[lane.domain] !== lane) return;
    settleLaneIdle(lane);
    delete lanes[lane.domain];
    if (!lanes.block && !lanes.database) users.delete(lane.userId);
    settleIdle();
  };

  const reportAndFinish = (lane: LaneState, error: unknown): void => {
    try {
      options.onError(error);
    } finally {
      finish(lane);
    }
  };

  const deferLane = (lane: LaneState): void => {
    const token = generation;
    lane.phase = "scheduled";
    try {
      options.defer(() => {
        if (!isCurrent(lane, token)) return;
        lane.phase = "running";
        lane.requestedWhileRunning = false;
        void options.runTurn(lane.userId, lane.domain).then(
          (result) => {
            if (!isCurrent(lane, token)) return;
            assertTurn(result);
            if (result === "pending" || lane.requestedWhileRunning) {
              deferLane(lane);
            } else {
              finish(lane);
            }
          },
          (error) => {
            if (isCurrent(lane, token)) reportAndFinish(lane, error);
          },
        ).catch((error: unknown) => {
          if (isCurrent(lane, token)) reportAndFinish(lane, error);
        });
      });
    } catch (error) {
      if (isCurrent(lane, token)) reportAndFinish(lane, error);
    }
  };

  return {
    dispose() {
      if (disposed) return;
      disposed = true;
      generation += 1;
      users.forEach((lanes) => {
        if (lanes.block) settleLaneIdle(lanes.block);
        if (lanes.database) settleLaneIdle(lanes.database);
      });
      users.clear();
      settleIdle();
    },

    idle(userId?: string, domain?: RecordCacheBudgetContinuationDomain) {
      if (userId !== undefined || domain !== undefined) {
        if (userId === undefined || domain === undefined) {
          throw new RangeError("A record-cache continuation lane requires user and domain.");
        }
        assertUserId(userId, options.maxUserIdCharacters);
        assertDomain(domain);
        const lane = users.get(userId)?.[domain];
        if (!lane) return Promise.resolve();
        if (lane.idlePromise) return lane.idlePromise;
        lane.idlePromise = new Promise<void>((resolve) => {
          lane.resolveIdle = resolve;
        });
        return lane.idlePromise;
      }
      if (users.size === 0) return Promise.resolve();
      if (idlePromise) return idlePromise;
      idlePromise = new Promise<void>((resolve) => {
        resolveIdle = resolve;
      });
      return idlePromise;
    },

    schedule(userId, domain) {
      assertUserId(userId, options.maxUserIdCharacters);
      assertDomain(domain);
      if (disposed) return "refused";

      const existing = users.get(userId)?.[domain];
      if (existing) {
        if (existing.phase === "running") existing.requestedWhileRunning = true;
        return "coalesced";
      }
      if (!users.has(userId) && users.size >= options.maxPendingUsers) {
        return "refused";
      }

      const lanes = users.get(userId) ?? {};
      const lane: LaneState = {
        domain,
        idlePromise: null,
        phase: "scheduled",
        requestedWhileRunning: false,
        resolveIdle: null,
        userId,
      };
      lanes[domain] = lane;
      users.set(userId, lanes);
      deferLane(lane);
      return "scheduled";
    },
  };
}
