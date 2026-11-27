import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { DatabaseViewOpenMode } from "@/components/database/DatabaseView";
import { readPeopleDatabase, type PeopleDatabase } from "@/lib/peopleDatabase";
import { readPeopleDirectory, usePeopleRevision, type PeopleDirectoryPerson } from "@/lib/peopleProfiles";
import type { Session, Workspace } from "@/types";
import { PeopleDatabaseIcon } from "@/components/page/PageIcon";
import { PeopleDatabaseView } from "./PeopleDatabaseView";
import styles from "./PeopleView.module.css";

/**
 * The people the reader sees and People's database (people_database_routes.go),
 * read where the workspace lists its people, and again as People changes —
 * someone joins, leaves, is deactivated or changes — the list as it was
 * staying until the new one comes; failed is set only when none was shown.
 */
export function usePeopleDirectoryRead(session: Session, workspace: Workspace, directory: boolean) {
  const [people, setPeople] = useState<PeopleDirectoryPerson[] | null>(null);
  const [database, setDatabase] = useState<PeopleDatabase | null>(null);
  const [failed, setFailed] = useState(false);
  const revision = usePeopleRevision(workspace.id);
  const loaded = useRef(false);
  const read = useCallback(async (signal?: AbortSignal) => {
    const [response, read] = await Promise.all([readPeopleDirectory(session, workspace.id, signal), readPeopleDatabase(session, workspace.id, signal)]);
    loaded.current = true; setPeople(response.people); setDatabase(read); setFailed(false);
  }, [session, workspace.id]);
  useEffect(() => {
    if (!directory) return undefined;
    const controller = new AbortController();
    // A list already shown stays as it was when reading it again fails; the next change reads it again.
    read(controller.signal).catch(() => { if (!controller.signal.aborted && !loaded.current) setFailed(true); });
    return () => controller.abort();
  }, [directory, read, revision]);
  const reread = useCallback(() => read().then(() => true, () => false), [read]);
  return { people, database, failed, reread };
}

/**
 * People's title and its database, where a database page has them — on
 * People's own screen (PeopleView) and in its peek beside a page (PeoplePeek).
 */
export function PeopleDirectoryBody({ session, workspace, directory, read, onOpenPerson }: {
  session: Session;
  workspace: Workspace;
  directory: boolean;
  read: ReturnType<typeof usePeopleDirectoryRead>;
  onOpenPerson(userId: string, mode: DatabaseViewOpenMode): void;
}) {
  const { t } = useTranslation("peopleProfiles");
  const { people, database, failed, reread } = read;
  return <>
    <h1 className={styles.directoryTitle}><PeopleDatabaseIcon pixels={36} />{t("people")}</h1>
    {directory ? null : <div className={styles.unavailable} role="status" data-people-unavailable>{t("unavailable")}</div>}
    {failed ? <div className={styles.unavailable} role="alert" data-error>{t("loadFailed")}</div> : null}
    {people && database ? <PeopleDatabaseView session={session} workspace={workspace} people={people} database={database} onReread={reread}
      onOpenPerson={onOpenPerson} /> : null}
  </>;
}
