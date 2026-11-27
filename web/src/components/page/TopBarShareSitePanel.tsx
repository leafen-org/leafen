import type { SharePage } from "./topBarShareModel";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { Session } from "@/types";
import { styles } from "./topBarStyles";
import type { SitePreviewSettings } from "./TopBarShareSitePreview";
import { pageTitle, siteHeadTitle } from "./topBarShareModel";
import { TopBarSitePublishIntro } from "./TopBarSitePublishIntro";
import { TopBarSiteIndexing } from "./TopBarSiteIndexing";
import { TopBarSiteEmbed, TopBarSitePublished, TopBarSiteSocial } from "./TopBarSitePublished";
import { LocalAddressHint } from "@/components/product/LocalAddressHint";
import { GlobeIcon, GlobeOffIcon } from "@/icons/leafen";
import access from "./TopBarShareAccess.module.css";
import shareStyles from "./TopBarShare.module.css";
import { ShareErrorLine } from "./ShareErrorLine";

/** The tab for someone without full access, as Notion's (seen 2026-11-04). */
export function TopBarSharePublishNeedsAccess() {
  const { t } = useTranslation("topBar");
  return <div className={access.publishNeedsAccess} data-share-publish-needs-access>
    <GlobeIcon size="regular" aria-hidden="true" />
    <p>{t("publishNeedsAccess")}</p>
    <p>{t("publishNeedsAccessDetail")}</p>
  </div>;
}

/**
 * The tab for an unpublished page in the trash, or under a page in it, as
 * Notion's (seen 2026-11-13): a struck-through globe, that a page in the
 * trash can't be published, and "복원" where the window can restore it. A
 * published one keeps its published tab (TopBarSharePublishTab).
 */
export function TopBarSharePublishInTrash({ onRestore }: { onRestore?: () => void }) {
  const { t } = useTranslation("topBar");
  return <div className={shareStyles.publishInTrash} data-share-publish-in-trash>
    <GlobeOffIcon pixels={32} aria-hidden="true" />
    <p>{t("publishInTrash")}</p>
    <p>{t("publishInTrashDetail")}</p>
    {onRestore ? <button type="button" className={shareStyles.publishInTrashRestore} data-share-publish-restore onClick={onRestore}>
      {t("publishInTrashRestore")}</button> : null}
  </div>;
}

/**
 * The publish tab: the intro before a page is published, then the published
 * site's rows and the screens they open. "사이트 스타일 사용자 지정" opens
 * the site editor over everything, as Notion's (`SiteStyleEditor`).
 */
export function TopBarShareSitePanel({
  addressError,
  busy,
  controlsDisabled,
  copied,
  errorCode,
  failure,
  navigationCandidates,
  onAddressChange,
  onCopy,
  onIndexingChange,
  onSearchDetailsSave,
  onSearchTyped,
  onDuplicateChange,
  onManageSites,
  onPublish,
  onScreenChange,
  onStyle,
  onUnpublish,
  page,
  published,
  publishingAllowed,
  searchErrors,
  session,
  siteLink,
  sitePreview,
  siteTitle,
  siteDescription,
  siteDefaultDescription,
  untitled,
}: {
  /** Why the address typed in the address row can't be the site's (another site has it). */
  addressError: string;
  busy: boolean;
  controlsDisabled: boolean;
  copied: boolean;
  errorCode: string;
  /** Why the published panel's last change, other than its address, didn't take: said at the end of the screen it was made on. */
  failure: string;
  navigationCandidates: SharePage[];
  onCopy: () => void;
  /** Publishes the site at the page path typed in the address row, saying whether it took (shareChangeTook). */
  onAddressChange: (slug: string) => Promise<boolean>;
  /** Lets search engines index the site or not, kept at once. */
  onIndexingChange: (value: boolean) => void;
  /** Keeps the site's link title and description ("변경 사항 저장"), saying whether they took (shareChangeTook). */
  onSearchDetailsSave: (title: string, description: string) => Promise<boolean>;
  /** The link title or description typed in again, which puts away why it was refused. */
  onSearchTyped: (field: "title" | "description") => void;
  onDuplicateChange: (value: boolean) => void;
  /** Opens the settings' public pages, "모든 사이트 및 링크 관리". */
  onManageSites: () => void;
  onPublish: () => void;
  /** Another of the published panel's screens is shown: what the last one said goes with it. */
  onScreenChange: () => void;
  /** Opens the site editor, "사이트 스타일 사용자 지정". */
  onStyle: () => void;
  onUnpublish: () => void;
  page: SharePage;
  published: boolean;
  /** Whether the organization lets the page be published to the web. */
  publishingAllowed: boolean;
  /** Why the link title or description can't be the site's, said under each on the indexing screen (shareFieldErrors). */
  searchErrors: { title?: string; description?: string };
  session: Session;
  siteLink: string;
  sitePreview: SitePreviewSettings;
  siteTitle: string;
  siteDescription: string;
  /** What the site's head says under its title while it keeps no description (publicPageDescription). */
  siteDefaultDescription: string;
  untitled: string;
}) {
  const { t } = useTranslation(["siteBuilder", "topBar"]);
  // The published site's list, or one of the screens its rows open.
  const [screen, setShownScreen] = useState<"list" | "indexing" | "embed" | "social">("list");
  const setScreen = (next: typeof screen) => { onScreenChange(); setShownScreen(next); };
  if (!published) {
    return (
      <TopBarSitePublishIntro busy={busy} disabled={controlsDisabled} errorCode={errorCode} onPublish={onPublish} page={page}
        publishingAllowed={publishingAllowed} subpages={navigationCandidates.filter((candidate) => candidate.parentId === page.id)}
        untitled={untitled} />
    );
  }
  if (screen === "embed") return <TopBarSiteEmbed siteLink={siteLink} onBack={() => setScreen("list")} />;
  if (screen === "social") {
    return <TopBarSiteSocial siteLink={siteLink} title={siteTitle || pageTitle(page, untitled)} onBack={() => setScreen("list")} />;
  }
  if (screen === "indexing") {
    return (
      <TopBarSiteIndexing session={session} siteLink={siteLink} faviconFileId={sitePreview.faviconFileId}
        defaultTitle={siteHeadTitle(page)} title={siteTitle} description={siteDescription} defaultDescription={siteDefaultDescription}
        allowIndexing={sitePreview.allowIndexing} disabled={controlsDisabled}
        onIndexingChange={onIndexingChange} onSave={onSearchDetailsSave} onTyped={onSearchTyped} onBack={() => setScreen("list")}
        errors={searchErrors} failure={failure} />
    );
  }
  return (<>
    {publishingAllowed ? null : (
      <p className={styles.shareSecurityNote} data-share-publishing-off="true">{t("topBar:publishingTurnedOff")}</p>
    )}
    <TopBarSitePublished siteLink={siteLink} copied={copied} busy={busy} disabled={controlsDisabled} allowIndexing={sitePreview.allowIndexing}
      addressError={addressError} onAddressChange={onAddressChange}
      allowDuplicate={sitePreview.allowDuplicate} onDuplicateChange={onDuplicateChange}
      onCopy={onCopy} onStyle={onStyle} onIndexing={() => setScreen("indexing")}
      onEmbed={() => setScreen("embed")} onSocial={() => setScreen("social")} onManage={onManageSites}
      onUnpublish={onUnpublish} />
    <ShareErrorLine text={failure} className={styles.shareError} data-site-failure />
    <LocalAddressHint session={session} address={siteLink} className={styles.shareSecurityNote} />
  </>);
}
