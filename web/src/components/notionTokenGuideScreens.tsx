import type { ReactNode } from "react";
import { GuideCaret, GuideClick, GuideCursor, GuideWindowBar } from "./guide/GuideParts";
import { leafenScopeScreen, leafenTokenScreen, type LeafenImportWords } from "./notionTokenGuideLeafen";
import { styles } from "./notionTokenGuideStyles";

/** The words drawn in the Notion token guide's screens (notionTokenGuide:screen). */
export type NotionTokenGuideWords = Record<string, string> & {
  topTabs: string[];
  columns: string[];
  connectionTabs: string[];
  pageNames: string[];
};

/** One screen of the guide, and how long it plays when not the usual 3 seconds. */
export type NotionTokenGuideScreen = { picture: ReactNode; durationMs?: number };

// The seven screens of the Notion token guide, in the order of the import
// screen's steps (importDialog:notionSteps): Leafen's token step, Notion's
// developer tools (Developer connections), the new-connection dialog, the
// connection's content access with its page picker, its configuration with
// the token, then Leafen again for the token and what to move. Each is a CSS
// replica of the real screen as Notion drew it on 2026-11-16 (Notion's own
// app shell with its sidebar; pill tabs; the page picker opened from "편집
// 권한"), with its cursor's path.
export function notionTokenGuideScreens(M: NotionTokenGuideWords, L: LeafenImportWords): NotionTokenGuideScreen[] {
  return [
    { picture: leafenTokenScreen(M, L, "open") },
    {
      picture: (
        <>
          <GuideWindowBar title={M.url} />
          <NotionShell M={M}>
            <DeveloperConnections M={M} cursor />
          </NotionShell>
        </>
      ),
    },
    {
      picture: (
        <>
          <GuideWindowBar title={M.url} />
          <NotionShell M={M}>
            <DeveloperConnections M={M} />
          </NotionShell>
          <NewConnectionDialog M={M} />
        </>
      ),
    },
    { picture: <ContentAccess M={M} />, durationMs: 3800 },
    { picture: <Configuration M={M} /> },
    { picture: leafenTokenScreen(M, L, "paste") },
    { picture: leafenScopeScreen(M, L) },
  ];
}

/** Notion's app around its developer tools: its sidebar, and the top bar's path to the page. */
function NotionShell({ M, connection = false, children }: { M: NotionTokenGuideWords; connection?: boolean; children: ReactNode }) {
  return (
    <div className={styles.notionShell}>
      <div className={styles.notionSidebar} aria-hidden="true">
        <span className={styles.sideSearch} />
        <span className={styles.sideRow} style={{ width: "62%" }} />
        <span className={styles.sideRow} style={{ width: "78%" }} />
        <span className={styles.sideRow} style={{ width: "54%" }} />
        <span className={styles.sideRow} style={{ width: "70%" }} />
        <span className={styles.sideWorkspace}>
          <i>{M.workspaceName.slice(0, 1)}</i>
          <span className={styles.cellText}>{M.workspaceName}</span>
        </span>
      </div>
      <div className={styles.notionMain}>
        <div className={styles.notionTopbar}>
          <span className={styles.crumbGlyph} aria-hidden="true">{"{ }"}</span>
          <span className={styles.cellText}>{M.devTools}</span>
          <span className={styles.crumbSep} aria-hidden="true">/</span>
          <span className={styles.crumbGlyph} aria-hidden="true">⊞</span>
          <span className={styles.cellText}>{M.devConnections}</span>
          {connection ? (
            <>
              <span className={styles.crumbSep} aria-hidden="true">/</span>
              <span className={styles.cellText}>{M.integrationName}</span>
            </>
          ) : null}
        </div>
        <div className={styles.notionPage}>{children}</div>
      </div>
    </div>
  );
}

/** The developer tools' Developer connections tab, before the new connection is there. */
function DeveloperConnections({ M, cursor = false }: { M: NotionTokenGuideWords; cursor?: boolean }) {
  return (
    <>
      <strong className={styles.pageTitle}>{M.devTools}</strong>
      <div className={styles.pillTabs}>
        {M.topTabs.map((tab, index) => (
          <span key={tab} data-active={index === 2 ? "true" : undefined}>
            {tab}
            {index === 1 ? <em className={styles.navBadge}>{M.navNew}</em> : null}
          </span>
        ))}
      </div>
      <div className={styles.devToolbar}>
        <span className={styles.toolbarIcon} aria-hidden="true">≡</span>
        <span className={styles.toolbarIcon} aria-hidden="true">⌕</span>
        <span className={styles.notionPrimary}>
          + {M.newConnection}
          {cursor ? <GuideCursor path={[{ at: 500, x: -187, y: 133 }, { at: 1300, x: 0, y: 0 }]} clickAt={2050} /> : null}
        </span>
      </div>
      <div className={styles.devTableHead}>
        {M.columns.map((column, index) => (
          <span key={column}>
            {column}
            {index === M.columns.length - 1 ? " ↓" : null}
          </span>
        ))}
      </div>
      {["68%", "54%"].map((width) => (
        <div key={width} className={styles.devRow} aria-hidden="true">
          <span className={styles.rowMain}>
            <i className={styles.integrationAvatar} />
            <span className={styles.rowText}>
              <span className={styles.skeletonLine} style={{ width }} />
              <span className={styles.skeletonLine} style={{ width: "86%" }} />
            </span>
          </span>
          <span className={styles.skeletonLine} style={{ width: "60%" }} />
          <span className={styles.skeletonLine} style={{ width: "55%" }} />
          <span className={styles.skeletonLine} style={{ width: "50%" }} />
          <span className={styles.skeletonLine} style={{ width: "70%" }} />
        </div>
      ))}
    </>
  );
}

/** "신규 연결": the name typed, the API token kept, Create pressed. */
function NewConnectionDialog({ M }: { M: NotionTokenGuideWords }) {
  return (
    <div className={styles.modalBackdrop}>
      <div className={styles.notionModal}>
        <div className={styles.modalHead}>
          <strong>{M.modalTitle}</strong>
          <span aria-hidden="true">✕</span>
        </div>
        <p className={styles.modalLead}>{M.modalLead}</p>
        <span className={styles.fieldLabel}>{M.nameLabel}</span>
        <span className={`${styles.notionInput} ${styles.typedInput}`}>
          {M.integrationName}
          <GuideCaret />
        </span>
        <span className={styles.fieldLabel}>{M.authMethod}</span>
        <div className={`${styles.authOption} ${styles.authOptionSelected}`}>
          <span className={styles.radioDot} aria-hidden="true" />
          <span className={styles.authText}>
            <strong>{M.apiToken}</strong>
            <span>{M.apiTokenDesc}</span>
          </span>
        </div>
        <div className={styles.authOption}>
          <span className={styles.radioDot} aria-hidden="true" />
          <span className={styles.authText}>
            <strong>{M.oauth}</strong>
            <span>{M.oauthDesc}</span>
          </span>
        </div>
        <div className={styles.modalFooter}>
          <span>{M.terms}</span>
          <span className={styles.notionPrimary}>
            {M.createButton}
            <GuideCursor path={[{ at: 500, x: -217, y: -162 }, { at: 1300, x: 0, y: 0 }]} clickAt={2050} />
          </span>
        </div>
      </div>
    </div>
  );
}

/** The connection's page: its name and its tabs, `active` the one shown. */
function ConnectionHead({ M, active }: { M: NotionTokenGuideWords; active: number }) {
  return (
    <>
      <strong className={styles.pageTitle}>{M.integrationName}</strong>
      <div className={styles.pillTabs}>
        {M.connectionTabs.map((tab, index) => (
          <span key={tab} data-active={index === active ? "true" : undefined}>
            {tab}
          </span>
        ))}
      </div>
    </>
  );
}

// "콘텐츠 사용 권한" › "편집 권한" opens "페이지 사용 권한 관리"; a top page
// is ticked and "저장하기" pressed. The picker's list is shown scrolled to
// the private pages, opened.
function ContentAccess({ M }: { M: NotionTokenGuideWords }) {
  return (
    <>
      <GuideWindowBar title={M.urlConnection} />
      <NotionShell M={M} connection>
        <ConnectionHead M={M} active={1} />
        <div className={styles.card}>
          <div className={styles.cardHead}>
            <span className={styles.cardHeadText}>
              <strong>{M.connectionTabs[1]}</strong>
              <span>{M.contentAccessDesc}</span>
            </span>
            <span className={styles.textButton} data-guide-target="editAccess">
              {M.editAccess}
              <GuideClick at={800} />
            </span>
          </div>
          <span className={styles.cardSearch}>⌕ {M.contentSearch}</span>
        </div>
      </NotionShell>
      <div className={styles.accessBackdrop}>
        <div className={styles.accessModal}>
          <i className={styles.accessIcon} aria-hidden="true" />
          <strong className={styles.accessTitle}>{M.pageAccessTitle}</strong>
          <p className={styles.accessLead}>{M.pageAccessDesc}</p>
          <span className={styles.fieldLabel}>{M.pagePick} ⓘ</span>
          <span className={styles.notionInput}>⌕ {M.pageSearch}</span>
          <div className={styles.accessList}>
            <span className={styles.groupRow}>
              {M.privatePages}
              <i aria-hidden="true">⌄</i>
            </span>
            <span className={styles.topPagesRow}>
              <span>{M.topPages}</span>
              <span className={styles.blueText}>{M.selectAll}</span>
            </span>
            {M.pageNames.map((name, index) => (
              <span key={name} className={styles.pageRow}>
                <i
                  className={index === 0 ? `${styles.checkbox} ${styles.checkboxTicks}` : styles.checkbox}
                  data-guide-target={index === 0 ? "pickPage" : undefined}
                  aria-hidden="true"
                >
                  {index === 0 ? <GuideClick at={1750} /> : null}
                </i>
                <i className={styles.pageGlyph} aria-hidden="true" />
                <strong>{name}</strong>
              </span>
            ))}
          </div>
          <div className={styles.accessFooter}>
            <span>{M.cancel}</span>
            <span className={styles.notionPrimary}>
              {M.save}
              <GuideCursor
                clickAt={2700}
                path={[
                  { at: 0, x: -150, y: -40 },
                  { at: 560, on: "editAccess", x: 2, y: 1 },
                  { at: 900, on: "editAccess", x: 2, y: 1 },
                  { at: 1550, on: "pickPage", x: 1, y: 1 },
                  { at: 1900, on: "pickPage", x: 1, y: 1 },
                  { at: 2550, x: 0, y: 0 },
                ]}
              />
            </span>
          </div>
        </div>
      </div>
    </>
  );
}

// "구성": "콘텐츠 읽기" ticked under "기능", and the API token copied with
// the button beside it ("복사하기"). The page is shown scrolled past its
// first card ("정보 표시").
function Configuration({ M }: { M: NotionTokenGuideWords }) {
  return (
    <>
      <GuideWindowBar title={M.urlConnection} />
      <NotionShell M={M} connection>
        <ConnectionHead M={M} active={0} />
        <div className={styles.card}>
          <strong className={styles.cardTitle}>{M.tokenSectionTitle}</strong>
          <p className={styles.cardLead}>{M.tokenSectionDesc}</p>
          <div className={styles.tokenFields}>
            <span className={styles.tokenField}>
              <span className={styles.fieldLabel}>{M.wsLabel}</span>
              <span className={styles.notionInput}>
                <i className={styles.wsMiniChip} aria-hidden="true">{M.workspaceName.slice(0, 1)}</i>
                <span className={styles.cellText}>{M.workspaceName}</span>
              </span>
            </span>
            <span className={styles.tokenField}>
              <span className={styles.fieldLabel}>{M.apiToken}</span>
              <span className={styles.notionInput}>
                <span className={styles.tokenDots}>{M.tokenDots}</span>
                <i className={styles.tokenIcon} aria-hidden="true">◉</i>
                <i className={styles.tokenIcon} aria-hidden="true">↻</i>
                <i className={`${styles.tokenIcon} ${styles.copyIcon}`} aria-hidden="true">
                  ⧉
                  <GuideCursor
                    clickAt={1950}
                    path={[
                      { at: 300, x: -240, y: 90 },
                      { at: 800, on: "readContent", x: 1, y: 1 },
                      { at: 1250, on: "readContent", x: 1, y: 1 },
                      { at: 1800, x: 0, y: 0 },
                    ]}
                  />
                </i>
              </span>
            </span>
          </div>
        </div>
        <div className={styles.card}>
          <strong className={styles.cardTitle}>{M.capabilitiesTitle}</strong>
          <p className={styles.cardLead}>{M.capabilitiesDesc}</p>
          <span className={styles.fieldLabel}>{M.contentCapabilities}</span>
          <span className={styles.checkRow}>
            <i className={`${styles.checkbox} ${styles.checkboxOn}`} data-guide-target="readContent" aria-hidden="true" />
            <span className={styles.authText}>
              <strong>{M.readContent}</strong>
              <span>{M.readContentDesc}</span>
            </span>
          </span>
        </div>
      </NotionShell>
    </>
  );
}
