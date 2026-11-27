import { Fragment, useEffect, useMemo, useRef, type ReactNode } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { Trans, useTranslation } from "react-i18next";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { koreanWithAnd } from "@/lib/pageUpdates";
import { unansweredReadText } from "@/lib/unansweredRead";
import { LockIcon, MailIcon } from "@/icons/leafen";
import { activeDateLocale } from "@/lib/i18n";
import { KOREAN_OBJECT_PAIR, KOREAN_SUBJECT_PAIR } from "@/lib/koreanParticles";
import { inboxAge } from "./updatesPanelModel";
import type { WorkspaceActivityPlace, WorkspaceActivityRecord } from "@/workspaceActivityWire.generated";
import { pagePathPlaceHref, pagePathPlaceTitle, type PagePathPlace } from "@/lib/pagePathPieces";
import type { Page } from "@/types";
import { PropertyTypeIcon } from "../database/PropertyTypeIcon";
import { ActivityViewIcon } from "./ActivityViewIcon";
import type { GraphValueType } from "@/contentGraphWire.generated";
import type { useWorkspaceActivityController } from "./useWorkspaceActivityController";
import { WorkspaceActivityChanges } from "./WorkspaceActivityChanges";
import { InboxPermissionChange } from "./InboxPermissionChange";
import { ActivityTarget, followActivityLink } from "./ActivityTarget";
import styles from "../UpdatesPanelFeed.module.css";
import rowStyles from "./WorkspaceActivityRow.module.css";
import { untitledActivityName, untitledPageName } from "./activityNames";
import { listedPageNameKind } from "@/lib/pageTitle";
import { permissionLineSentence } from "./permissionLine";
import { pageMapOf } from "@/lib/pageIndex";
import { pageHref } from "@/lib/navigation";
import { rememberInlineDatabaseAddress } from "@/lib/inlineDatabaseAddress";
import { useProductPagesSelect } from "../product/productPagesStore";
import { keysFocusHolder } from "@/lib/lastPress";

// A row the keys' focus is in shows its buttons (data-keys-within).
keysFocusHolder(styles.row);

function activityTime(at: string, now: number, t: (key: string, options?: Record<string, unknown>) => string) {
  const date = new Date(at);
  const age = inboxAge(date.getTime(), now, 7);
  if (age.unit === "now") return t("inbox.time.now");
  if (age.unit !== "date") return t(`inbox.activity.time.${age.unit}`, { count: age.count });
  return date.toLocaleDateString(activeDateLocale(), { month: "short", day: "numeric" });
}

/** Who made an update: names in the page's weight unless it is the reader's own. */
type ActivityActorParts = { lead?: string; leadBold?: boolean; join?: string; who: string; bold: boolean; tail?: string };

function ActivityActor({ parts, children }: { parts: ActivityActorParts; children?: ReactNode }) {
  const name = (text: ReactNode, bold?: boolean) => bold ? <strong className={styles.activityName}>{text}</strong> : text;
  return <>{parts.lead ? <>{name(parts.lead, parts.leadBold)}{parts.join}</> : null}{name(children, parts.bold)}{parts.tail}</>;
}

/**
 * A database's view or property: its layout's or type's icon, then its name,
 * as Notion's lines show it. A view is a link to its database showing it; a
 * property is no link (Notion's, seen 2026-11-16).
 */
function ActivityDatabasePart({ entityType, kind, href, onOpen, children }: {
  entityType: string; kind: string; href?: string; onOpen?: (href: string) => void; children?: ReactNode;
}) {
  const part = <span className={`${styles.pageMark} ${rowStyles.partName}`}>{entityType === "view" ? <ActivityViewIcon kind={kind} size="tiny"/>
    : kind ? <PropertyTypeIcon type={kind as GraphValueType} size="tiny"/> : null}{children}</span>;
  return href && onOpen ? <a className={rowStyles.part} href={href} onClick={followActivityLink(href, onOpen)}>{part}</a> : part;
}

/**
 * Where an update's thing is (workspace_activity_location.go), after its time
 * as Notion's workspace updates say it (seen 2026-11-13): "· 〈top〉 / ... /
 * 〈last〉", the first piece and the last with "..." for those between, two or
 * fewer whole, each a link to where it opens; the line is cut at its end, no
 * name shrinking to "…".
 */
function ActivityPlace({ place, untitled, onOpen }: {
  place: WorkspaceActivityPlace[];
  /** What a piece with no name is called (activityNames.ts). */
  untitled: (piece: WorkspaceActivityPlace) => string;
  onOpen: (href: string) => void;
}) {
  const shown = place.length > 2 ? [place[0], null, place[place.length - 1]] : place;
  return <span className={styles.activityPlace} data-activity-place>
    <span className={styles.activityPlaceDot}>·</span>
    {shown.map((piece, index) => <Fragment key={index}>
      {index ? <span className={styles.activityPlaceSlash}>/</span> : null}
      {/* A page's teamspace leads by name alone, no link (workspace_activity_location.go). */}
      {!piece ? <span>...</span> : piece.kind === "teamspace" ? <span className={rowStyles.placeTeamspace}>{piece.title}</span>
        : <a className={styles.activityPlacePiece} href={pagePathPlaceHref(piece as PagePathPlace)} onClick={followActivityLink(pagePathPlaceHref(piece as PagePathPlace), onOpen)}>
          {pagePathPlaceTitle(piece as PagePathPlace) || untitled(piece)}</a>}
    </Fragment>)}
  </span>;
}

// A database's own line, a view's and a property's, as Notion's workspace
// updates say them (seen 2026-11-13): "〈DB〉 컬렉션을 생성했습니다.", "〈person〉
// 님이 〈DB〉에서 ⊞ 〈view〉 보기를 편집했습니다" (no stop, "님이" after the
// reader's own "본인" too), "〈DB〉의 ≡ 〈property〉 속성을 편집했습니다." — the
// database by its name now and a new name an edit.
const databaseEntities = new Set(["dataSource", "view", "property"]);
const databaseVerbs = new Set(["create", "edit", "delete", "move", "restore"]);

/**
 * A view's line puts " 님이" after its maker whoever they are: "본인 님이
 * 〈DB〉에서 ≡ 〈view〉 보기를 생성했습니다", "나와 〈integration〉님 님이 …" —
 * Notion's (seen 2026-11-16 in a database's updates, two databases in two
 * tabs at two times), where a property's line has none ("본인 〈DB〉의 …").
 * Another person's name has it already.
 */
function viewActorTail(tail = "") {
  return tail.endsWith(" 님이") ? tail : `${tail} 님이`;
}

export function WorkspaceActivityFeed({ controller, pages, actorId, now, onOpen }: {
  controller: ReturnType<typeof useWorkspaceActivityController>; pages: Page[]; actorId: string; now: number;
  /** Opens an update's page, or where one of its lines goes. */
  onOpen: (item: WorkspaceActivityRecord, href?: string) => void;
}) {
  const { t, i18n } = useTranslation(["updatesPanel", "common"]);
  const sentinel = useRef<HTMLDivElement>(null);
  const list = useRef<HTMLUListElement>(null);
  const virtual = useVirtualizer({
    count: controller.data.items.length,
    getScrollElement: () => list.current?.closest<HTMLElement>("[data-inbox-list]") ?? null,
    getItemKey: (index) => controller.data.items[index].id,
    estimateSize: () => 80,
    overscan: 8,
  });
  const virtualized = controller.data.items.length > 100;
  // Measure the initial plain list too. Its existing sizes and scroll offset
  // must survive the first continuation that switches to a windowed list.
  const rendered = virtualized ? virtual.getVirtualItems().map((row) => ({ item: controller.data.items[row.index], row, index: row.index }))
    : controller.data.items.map((item, index) => ({ item, row: null, index }));
  const profiles = useMemo(() => new Map(controller.data.actorProfiles.map((profile) => [profile.userId, profile])), [controller.data.actorProfiles]);
  // A page's access for everyone in the workspace is named by the workspace,
  // as Notion's line names it ("〈작업공간〉의 모든 사용자", page_share_general_access_notices.go).
  const workspaceName = useProductPagesSelect((product) => product?.workspaces?.find(({ id }) => id === controller.data.workspaceId)?.name ?? "");
  const pageMap = pageMapOf(pages);
  useEffect(() => {
    if (!sentinel.current || !controller.data.nextCursor || controller.loading) return;
    const observer = new IntersectionObserver(([entry]) => { if (entry?.isIntersecting) controller.loadMore(); }, { root: sentinel.current.closest("[data-inbox-list]"), rootMargin: "160px" });
    observer.observe(sentinel.current);
    return () => observer.disconnect();
  }, [controller.data.nextCursor, controller.loading, controller.loadMore]);
  // The server not there to ask is no failure to try again: it is read again once it answers.
  if (controller.unanswered) return <div className={styles.empty} role="status" data-read-unanswered><span>{unansweredReadText()}</span></div>;
  if (controller.failed) return <div className={styles.empty} role="alert" data-error><span>{t("updatesPanel:inbox.activity.failed")}</span><button type="button" onClick={controller.retry}>{t("updatesPanel:inbox.activity.retry")}</button></div>;
  if (controller.loading && !controller.data.items.length) return <div className={styles.skeleton} aria-hidden="true">{[0,1,2,3].map((key) => <div key={key} className={styles.skeletonRow}><span/><span/></div>)}</div>;
  if (!controller.data.items.length) return <div className={styles.empty} role="status"><MailIcon size="mark" aria-hidden="true"/><span>{t("updatesPanel:inbox.empty.workspace")}</span></div>;
  return <>
    <ul ref={list} className={styles.sectionList} data-workspace-activity-list style={virtualized ? { position: "relative", height: virtual.getTotalSize() } : undefined}>
      {rendered.map(({ item, row, index }) => {
        const profile = profiles.get(item.actorId);
        const self = item.actorId === actorId;
        const person = self ? t("updatesPanel:inbox.activity.self") : profile?.displayName || t("updatesPanel:inbox.someone");
        // An integration's edit is named by it alone, and beside its person
        // when they edited too ("나와 〈integration〉님"), as Notion's are (seen 2026-11-06).
        const integration = item.integrations?.join(", ") ?? "";
        // An app's own account is named as an integration is: its name alone
        // on a connection's face, never "〈app〉 님이" (the page's history marks it "(앱)").
        const app = !self && profile?.app === true;
        const bot = Boolean(integration && item.integrationsOnly) || app;
        const ko = i18n.language.startsWith("ko");
        // Other people and integrations are named in the page's weight, the reader
        // in the line's: "나와 〈integration〉님", "june lee, 〈integration〉님" (Notion, seen 2026-11-06).
        const joiner = new Intl.ListFormat(i18n.language, { type: "conjunction" }).formatToParts(["a", "b"]).find((part) => part.type === "literal")?.value ?? ", ";
        // Made through an app or an agent, its person is named with it, as
        // the page's history names them ("본인이 〈app〉을 통해").
        const through = item.through ?? "";
        const actor: ActivityActorParts = bot ? { who: app ? person : integration, bold: true }
          : through ? { who: person, bold: !self, tail: t(`updatesPanel:inbox.activity.${self ? "throughSelf" : "throughOther"}`,
            { connection: through, object: KOREAN_OBJECT_PAIR }) }
          : integration ? ko
            ? self ? { lead: koreanWithAnd(t("updatesPanel:rows.bySelf")), join: " ", who: integration, bold: true, tail: "님" }
              : { lead: person, leadBold: true, join: ", ", who: integration, bold: true, tail: "님" }
            : { lead: person, leadBold: !self, join: joiner, who: integration, bold: true }
          : { who: person, bold: !self, tail: !self && ko ? " 님이" : "" };
        const page = pageMap.get(item.pageId);
        // A page's own block — the page back from the trash — is named and
        // marked as the page is (workspace_activity_page_block.go).
        const pageBlock = item.entityType === "block" && item.kind === "page";
        const linePage = item.entityType === "page" || pageBlock ? page : undefined;
        // A thing with no name is called what its kind is (activityNames.ts), a database's page as its path calls it (`untitledName`).
        const untitled = item.entityType === "block" && !pageBlock ? t(`updatesPanel:inbox.activity.kinds.${item.kind}`,{defaultValue:t("common:labels.untitled")})
          : item.untitledName?.trim() || untitledActivityName(pageBlock ? "page" : item.entityType, item.kind, linePage,
            listedPageNameKind(item));
        const title = item.name || untitled;
        // A thing named when it had no name was renamed from what it was called then.
        const previousName = item.previousName || untitled;
        // Where it is, a piece with no name named as its kind — a database's page as its path names it (`untitledName`, page_path_route.go).
        const untitledPlace = (piece: WorkspaceActivityPlace) => piece.kind === "database"
          ? untitledActivityName("dataSource", "") : piece.untitledName?.trim() || untitledPageName(pageMap.get(piece.id));
        // A share of the reader's given, changed or taken away (workspace_activity_permission_notices.go)
        // reads as their inbox read it, its person and roles under it; who did it bold, the reader "본인".
        const permission = item.action === "permission";
        const entity = permission ? "" : t(`updatesPanel:inbox.activity.entities.${item.entityType}`);
        // After its kind's own word the particle is that word's ("페이지를", "블록을"), as Notion's lines
        // (activity.blockEdited "{pageTitle} 페이지를 편집했습니다."; seen 2026-11-17) — no pair after a set word.
        const entityObject = permission ? "" : t(`updatesPanel:inbox.activity.objectParticles.${item.entityType}`, { defaultValue: "" });
        // A row back from the trash comes as made again, any other page as its block restored (the server's reading).
        const action = item.action;
        // A page's own made and deleted lines say where it is now — "개인 페이지로",
        // "워크스페이스에" — as Notion's do (workspace_activity_page_scope.go).
        const sentence = item.scope && item.entityType === "page" && (action === "create" || action === "delete") ? `${item.scope}${action === "create" ? "Create" : "Delete"}` : action;
        const database = databaseEntities.has(item.entityType);
        const databaseName = item.databaseName?.trim() || untitledActivityName("dataSource", "");
        const databaseVerb = t(`updatesPanel:inbox.activity.database.verbs.${databaseVerbs.has(action) ? action : "edit"}`);
        // A view's line has " 님이" after whoever made it, the reader's "본인" and
        // "나와 〈integration〉님" too (viewActorTail).
        const lineActor = database && item.entityType === "view" && ko && !through ? { ...actor, tail: viewActorTail(actor.tail) } : actor;
        // What the sentence names is its link: the page, the database, a view
        // with its database showing it (Notion's, seen 2026-11-16) — an inline
        // database opened alone, by its block, not the page holding it.
        const databaseId = database && item.blockId ? item.blockId : item.pageId;
        if (databaseId !== item.pageId) rememberInlineDatabaseAddress(databaseId, item.pageId);
        const href = pageHref(databaseId);
        const follow = (to: string) => onOpen(item, to);
        return <li key={item.id} className={`${styles.row} ${rowStyles.row}`} data-workspace-activity={item.id}
          data-page-life={item.entityType === "page" && (action === "create" || action === "delete") || undefined}
          data-index={index} ref={virtual.measureElement}
          style={row ? { position: "absolute", top: 0, left: 0, width: "100%", transform: `translateY(${row.start}px)` } : undefined}
          aria-posinset={row ? row.index + 1 : undefined} aria-setsize={virtualized ? controller.data.items.length : undefined}>
          {/* Only its links open anything — what the sentence names, where it is,
              a changed line — the line itself pressing nothing, as Notion's
              (seen 2026-11-16: its blank, its words and its face left the panel
              as it was). The face is the person's own, the reader's too: their
              picture or their name's letter, never one of the word the line calls them by. */}
          <div className={rowStyles.main}>
            <span className={rowStyles.face}><span className={styles.lead} data-person="true" aria-hidden="true"><PersonAvatar userId={bot && !app ? null : item.actorId} avatar={bot && !app ? null : profile?.avatar} name={bot ? actor.who : self ? profile?.displayName : person} size={24} bot={bot}/></span></span>
            <span className={rowStyles.body}>
              <span className={rowStyles.sentence}>{permission
                ? <Trans t={t} i18nKey={`updatesPanel:inbox.sentences.${permissionLineSentence(item)}`}
                  values={{ name: person, title, subject: ko ? KOREAN_SUBJECT_PAIR : "" }}
                  components={{ name: <ActivityActor parts={{ who: person, bold: !self }}/>, page: <ActivityTarget href={href} onOpen={follow}/> }}/>
                : database
                ? <Trans t={t} i18nKey={`updatesPanel:inbox.activity.database.${item.entityType}`}
                  values={{ name: actor.who, title, database: databaseName, verb: databaseVerb }}
                  components={{ actor: <ActivityActor parts={lineActor}/>, db: <ActivityTarget href={href} onOpen={follow}/>,
                    target: <ActivityDatabasePart entityType={item.entityType} kind={item.kind} onOpen={follow}
                      href={item.entityType === "view" ? pageHref(databaseId, new URLSearchParams({ v: item.entityId })) : undefined}/> }}/>
                : <Trans t={t} i18nKey={`updatesPanel:inbox.activity.actions.${sentence}`}
                  values={{ name: actor.who, title, entity, previousName, object: entityObject }} components={{ actor: <ActivityActor parts={actor}/>, target: <ActivityTarget page={linePage} href={href} onOpen={follow}/> }}/>}</span>
              <span className={rowStyles.trail}>
                <span>{activityTime(item.occurredAt, now, (key, options) => t(`updatesPanel:${key}`, options))}</span>
                {item.location?.length ? <ActivityPlace place={item.location} untitled={untitledPlace} onOpen={(href) => onOpen(item, href)}/> : null}
              </span>
              {/* Each share the line changed, in the order first changed (page_share_permission_groups.go). */}
              {permission ? (item.permissions?.length ? item.permissions : [item]).map((change, at) => <InboxPermissionChange key={at}
                reader={change.entityType === "generalAccess" ? t("updatesPanel:inbox.activity.everyoneInWorkspace", { name: workspaceName })
                  : profiles.get(change.entityId)?.displayName ?? ""} from={change.fromRole ?? ""} to={change.toRole ?? ""}/>) : null}
            </span>
            {/* The filled lock at the end of the sentence's first line while only the reader opens the page now,
                as its share button's lock (workspace_activity_only_reader.go; Notion's, seen 2026-11-16 and 2026-11-17). */}
            {item.onlyReader ? <span className={rowStyles.lock}><LockIcon size="tiny" weight="fill" aria-label={t("updatesPanel:inbox.activity.private")}/></span> : null}
          </div>
          {item.hasChanges ? <WorkspaceActivityChanges item={item} read={controller.readChanges} onOpen={onOpen}
            pageOf={(id) => pageMap.get(id)} untitledPage={(id) => untitledPageName(pageMap.get(id))}/> : null}
        </li>;
      })}
    </ul>
    <div ref={sentinel} data-workspace-activity-continuation aria-busy={controller.loading || undefined}>
      {controller.data.nextCursor ? <button type="button" className={styles.activityContinue} disabled={controller.loading} onClick={controller.loadMore}>{t("updatesPanel:inbox.activity.more")}</button> : null}
    </div>
  </>;
}
