import type { createDatabaseToolbarConfigService } from "@/lib/databaseToolbarConfigService";
import { keptSentFor } from "@/lib/keptSentFor";
import { answeredUnlessAway } from "@/lib/serverReach";
import type { Session } from "@/types";

type ToolbarSettings = ReturnType<typeof createDatabaseToolbarConfigService>;

/**
 * The settings a change of a database's toolbar settings goes on
 * (NativeDatabaseToolbarMount mutate), once what it may name is on the
 * server: this device's writes to the page holding the database went
 * (keptSentFor) — a task role, a dependency pair or a template's value may
 * name a property just added, which the server refuses until it has it —
 * and the settings were read, a change asked for before their first read
 * waiting for it rather than coming to nothing (2026-11-24: "작업 데이터베이스로
 * 전환" pressed 0.96 s after a new database's two dates were added, its
 * settings read at 1.24 s, sent nothing and said nothing). With the server
 * away it doesn't wait for those writes: the change goes and is said as
 * unanswered, as every settings change is then (useSettingsWriteUnanswered).
 * Null once the settings are no longer this database's (`current`).
 */
export async function settingsToWriteOn(
  service: ToolbarSettings, session: Session, holderPageId: string, pageId: string, dataSourceId: string, current: () => boolean,
) {
  await answeredUnlessAway(keptSentFor(session, holderPageId)).catch(() => false);
  // A read under way is joined; one a newer read replaced gives nothing, and the newer one is joined next.
  for (let tries = 0; tries < 3 && current() && !service.snapshot(); tries += 1) await service.refresh(pageId, dataSourceId);
  return current() ? service.snapshot() : null;
}

/**
 * What a change of the toolbar settings the server took reads again before
 * its writer goes on (a window closes and says so, a template's shown change
 * gives way to what is read): the settings, which hold the revision the next
 * change goes from. A change that makes or retypes properties (a task
 * database's conversion) reads the page's graph too, behind it and not waited
 * for: that read waits for every write kept for the page (readSettled),
 * however long the server takes them, and a saved conversion's window stayed
 * busy on it, Esc and a press outside it held too (2026-11-24, decided here:
 * the window closes once the server has the change and its settings are read).
 */
export async function readAfterSettingsSaved(
  service: ToolbarSettings, pageId: string, dataSourceId: string, rereadGraph?: () => Promise<unknown> | undefined,
) {
  void Promise.resolve(rereadGraph?.()).catch(() => undefined);
  await service.refresh(pageId, dataSourceId);
}
