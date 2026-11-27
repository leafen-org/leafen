import { request } from "./apiTransport";
import type { Session } from "./types";

/**
 * A step an agent left to its person (person_steps.go): an operation held
 * until the person, signed in, types the secret fields it asks, keeps the
 * recovery code it shows, or opens what it makes for them alone.
 */
export type PersonStep = {
  id: string;
  state: "waiting" | "done" | "canceled" | "expired";
  operation: string;
  title: string;
  place?: string;
  section: string;
  fields: string[];
  keep?: "recovery-code";
  kept?: boolean;
  shows?: boolean;
  agent?: string;
  failureCode?: string;
  failure?: string;
  createdAt: number;
  expiresAt: number;
};

/**
 * The person's steps waiting in one settings section, newest first: every
 * page of them, so none past the first is left out of the section.
 */
export async function listPersonSteps(session: Session, section: string, signal?: AbortSignal) {
  const steps: PersonStep[] = [];
  let cursor: string | null = null;
  // A bound on the pages, should a list ever answer the same cursor again.
  for (let page = 0; page < 100; page += 1) {
    const query: string = cursor ? `&cursor=${encodeURIComponent(cursor)}` : "";
    const answer = await request<{ steps: PersonStep[]; nextCursor?: string | null; hasMore?: boolean }>(
      `/person-steps?section=${encodeURIComponent(section)}${query}`, { signal }, session);
    steps.push(...answer.steps);
    if (!answer.hasMore || !answer.nextCursor || answer.nextCursor === cursor) break;
    cursor = answer.nextCursor;
  }
  return { steps };
}

/** What the step shows the person alone: the recovery code to keep, or what its operation made. */
export function revealPersonStep(session: Session, id: string) {
  return request<{ personStep: PersonStep; shown: Record<string, string> }>(
    `/person-steps/${encodeURIComponent(id)}/reveal`, { method: "POST" }, session);
}

/** The fields the person typed, and that they kept the code: the agent's operation runs as them. */
export function enterPersonStep(session: Session, id: string, input: { values: Record<string, string>; kept: boolean }) {
  return request<{ personStep: PersonStep }>(
    `/person-steps/${encodeURIComponent(id)}/enter`, { method: "POST", body: JSON.stringify(input) }, session);
}

/** The person doesn't want it. */
export function cancelPersonStep(session: Session, id: string) {
  return request<{ personStep: PersonStep }>(`/person-steps/${encodeURIComponent(id)}`, { method: "DELETE" }, session);
}
