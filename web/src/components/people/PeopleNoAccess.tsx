import { useEffect, useState } from "react";
import { PageNoAccess, PageProblemPending } from "@/components/product/PageNoAccess";
import { requestPageAccess } from "@/lib/accessRequests";
import { readPeopleDatabase } from "@/lib/peopleDatabase";
import { beginAddingAccount } from "@/lib/serverAccounts";
import type { Session, Workspace } from "@/types";

/**
 * People to a restricted member, who doesn't open People's own page
 * (people_database_access.go): the window a page closed to them takes, as
 * Notion's restricted member gets on People's address — "<workspace>에서 이
 * 페이지에 대한 사용 권한 없음", "사용 권한 요청" and who is signed in, over a
 * page's outline, with no sidebar (seen 2026-11-16 in Chrome at 1440, in two
 * tabs at two times). Their own profile still opens (/people/<them>). The
 * request asks for People's page, whose id People's read names.
 */
export function PeopleNoAccess({ session, workspace }: { session: Session; workspace: Workspace }) {
  const [pageId, setPageId] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    readPeopleDatabase(session, workspace.id, controller.signal).then((database) => setPageId(database.pageId), () => {});
    return () => controller.abort();
  }, [session, workspace.id]);
  if (!pageId) return <PageProblemPending />;
  return <PageNoAccess pageId={pageId} workspaceName={workspace.name} account={session.user.email || session.user.name}
    onAddAccount={beginAddingAccount} onRequest={(id) => requestPageAccess(session, id)} />;
}
