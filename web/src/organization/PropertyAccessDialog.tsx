import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { DatabasePropertyPicker } from "@/components/database/DatabasePropertyPicker";
import { PropertyIcon } from "@/components/database/PropertyIcon";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { HoverTip } from "@/components/IconTipButton";
import { ReadFailed } from "@/components/ReadFailed";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneSheet } from "@/components/shell/PhoneSheet";
import { ChevronDown, DoubleChevronRight, EyeIcon, Plus, SharePeopleIcon, X } from "@/icons/leafen";
import { deviceValue, storeDeviceValue } from "@/lib/browserStorage";
import type { PropertyAccessDialogProps } from "@/lib/organizationModule";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { personName } from "@/lib/peopleDirectory";
import { popoverRectOf, type PopoverRect } from "@/lib/popoverPlacement";
import { useProductToast } from "@/lib/productToast";
import {
  loadPropertyAccessPrincipals, previewPropertyAccess, readPropertyAccess, savePropertyAccess,
  type PropertyAccessException, type PropertyAccessGroup, type PropertyAccessIntegration, type PropertyAccessPreview, type PropertyAccessRule,
} from "./propertyAccessApi";
import { AccessPopover, LevelMenu, levelCardRow, levelText, PrincipalIcon, PrincipalPicker, propertyAccessLevels } from "./PropertyAccessParts";
import { LevelPicture } from "./PropertyAccessLevelPicture";
import { PreviewPersonPicker } from "./PropertyAccessPreviewPerson";
import { PropertyAccessHoldersTip } from "./PropertyAccessHoldersTip";
import { PreviewResult } from "./PropertyAccessPreviewResult";
import styles from "./PropertyAccessDialog.module.css";
import phoneCss from "./PropertyAccessPhone.module.css";
import { PhoneLevelSheet, PhonePrincipalSheet, PhonePropertySheet } from "./PropertyAccessPhoneSheets";

type LevelMenuState = { anchor: PopoverRect; index: number | null };

/** Whether this browser shows the window's "미리보기", kept for every rule it opens, as Notion keeps it. */
const PREVIEW_KEY = "leafen:property-access-preview";
/** The narrowest window that shows the preview beside the rule, as Notion's. */
const PREVIEW_WINDOW_WIDTH = 932;

function subscribeWindowWidth(onChange: () => void) {
  window.addEventListener("resize", onChange);
  return () => window.removeEventListener("resize", onChange);
}

/**
 * Notion's "속성 사용 권한" window (seen 2026-10-12): the property, its level
 * for everyone who opens the database ("기본 사용 권한"), the exceptions —
 * those with full access always edit, then people, groups, apps and the
 * database's person properties — and "미리보기", which shows what a person
 * gets under the rule as drafted. "초기화" clears it; "저장" keeps it.
 * Closing with changes asks first. The fixed first exception's level names
 * the rule in a dark tip over it, as Notion's (read 2026-11-19 in Chrome at
 * 1440, dpr 2, twice: "이 데이터베이스에 대해 … 항상 … 권한을 가집니다.",
 * 300 wide, centred 6 over the level's 28px box), its icon and name who has
 * full access to the database (PropertyAccessHoldersTip). Its words keep
 * Leafen's one word for full access, "전체 허용" (docs/screen-words.md),
 * where Notion's window says "모두 허용".
 *
 * It opens with the preview as this browser last left it — closed until it is
 * shown — from the column's menu and the share's "고급" alike, as Notion's
 * (read 2026-11-18 at 16:56 and 17:15 in Chrome at 1440, its code: one kept
 * switch). The preview shows only on a window 932 or wider: there the head
 * holds "미리보기", a line and ✕; narrower only the line and ✕ (read at 900).
 * The window is 600 wide, 900 with the preview, 520 tall, both within 16 of
 * the window's edges. "미리보기" names itself "미리보기 표시" in the dark tip
 * under it and the preview's "»" "미리 보기 숨기기", as Notion's (read
 * 2026-11-18 in Chrome at 1440: 6 under each, in its middle; ✕ has none).
 *
 * A phone's is one white column in a whole sheet with no head under its grip,
 * as Notion's (w1949, read 2026-11-18 on a Galaxy A90's Chrome at 411 in a
 * Business database): the title and its ✕, the property, a line, then the
 * default level and the exceptions each in a white card, and "초기화" and
 * "저장" at the sheet's foot; no "미리보기" (PropertyAccessPhone.module.css).
 * Its menus are sheets over it (PropertyAccessPhoneSheets.tsx).
 */
export function PropertyAccessDialog({ session, workspaceId, dataSourceId, properties, personProperties, propertyId, onSaved, onClose }: PropertyAccessDialogProps) {
  const { t } = useTranslation("propertyAccess");
  const { notify } = useProductToast();
  const [currentId, setCurrentId] = useState(propertyId);
  const [saved, setSaved] = useState<PropertyAccessRule | null>(null);
  const [draft, setDraft] = useState<PropertyAccessRule | null>(null);
  const [error, setError] = useState("");
  // Reads that failed — the rule, the groups and apps it names, the
  // preview — each say so with a way to read again.
  const [failed, setFailed] = useState({ rule: false, principals: false, preview: false });
  const [attempt, setAttempt] = useState(0);
  const readFailed = (read: keyof typeof failed, value: boolean) => setFailed((current) => ({ ...current, [read]: value }));
  const [busy, setBusy] = useState(false);
  const [principals, setPrincipals] = useState<{ groups: PropertyAccessGroup[]; integrations: PropertyAccessIntegration[] }>({ groups: [], integrations: [] });
  const [levelMenu, setLevelMenu] = useState<LevelMenuState | null>(null);
  const [picker, setPicker] = useState<PopoverRect | null>(null);
  const [propertyMenu, setPropertyMenu] = useState<PopoverRect | null>(null);
  const [previewOpen, setPreviewOpenHere] = useState(() => deviceValue(PREVIEW_KEY) === "true");
  const setPreviewOpen = (open: boolean) => { setPreviewOpenHere(open); storeDeviceValue(PREVIEW_KEY, open ? "true" : null); };
  const wide = useSyncExternalStore(subscribeWindowWidth, () => window.innerWidth >= PREVIEW_WINDOW_WIDTH, () => true);
  const [previewUser, setPreviewUser] = useState(session.user.id);
  const [preview, setPreview] = useState<PropertyAccessPreview | null>(null);
  const { phone } = useSidebarLayout(true);
  const property = properties.find((candidate) => candidate.id === currentId);
  const dirty = Boolean(saved && draft && JSON.stringify(saved) !== JSON.stringify(draft));

  useEffect(() => {
    const controller = new AbortController();
    setSaved(null); setDraft(null); setError(""); readFailed("rule", false);
    readPropertyAccess(session, dataSourceId, currentId, controller.signal)
      .then((rule) => { setSaved(rule); setDraft(rule); })
      .catch(() => { if (!controller.signal.aborted) readFailed("rule", true); });
    return () => controller.abort();
  }, [attempt, currentId, dataSourceId, session]);

  useEffect(() => {
    const controller = new AbortController();
    readFailed("principals", false);
    void loadPropertyAccessPrincipals(session, workspaceId, controller.signal).then(setPrincipals)
      .catch(() => { if (!controller.signal.aborted) readFailed("principals", true); });
    return () => controller.abort();
  }, [attempt, session, workspaceId]);

  // The preview follows the draft and the person chosen; a phone's window has none.
  const previewShown = previewOpen && wide && !phone;
  useEffect(() => {
    if (!previewShown || !draft) return;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      readFailed("preview", false);
      previewPropertyAccess(session, draft, previewUser, controller.signal).then(setPreview).catch(() => {
        setPreview(null);
        if (!controller.signal.aborted) readFailed("preview", true);
      });
    }, 150);
    return () => { window.clearTimeout(timer); controller.abort(); };
  }, [attempt, draft, previewShown, previewUser, session]);

  // Unsaved changes ask first in the one confirmation card, as Notion's settings windows ask
  // before their changes go: "변경 사항 취소" filled red over the outlined "계속 편집".
  const askFirst = (then: () => void) => {
    if (!dirty) return then();
    void confirmAction({ message: t("discardTitle"), detail: t("discardBody"), confirmLabel: t("discard"),
      cancelLabel: t("keepEditing"), danger: "filled", markers: { dialog: { "data-access-discard": "" } } })
      .then((discard) => { if (!discard) return; setDraft(saved); then(); });
  };
  const close = () => askFirst(onClose);

  // A modal layer, so the menus it opens stack above it and Escape closes
  // the top one first.
  const dialogRef = useRef<HTMLDivElement>(null);
  const { zIndex } = useOverlayLayer(!phone, { kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement],
    onEscape: close, initialFocus: "first" });

  function change(next: Partial<PropertyAccessRule>) {
    setDraft((current) => (current ? { ...current, ...next } : current));
  }

  async function save() {
    if (!draft || !property) return;
    setBusy(true);
    setError("");
    try {
      const rule = await savePropertyAccess(session, draft);
      setSaved(rule); setDraft(rule);
      notify(t("saved", { name: property.name }), "success");
      onSaved?.();
      onClose();
    } catch {
      setError(t("saveFailed"));
    } finally {
      setBusy(false);
    }
  }

  const principalLabel = (exception: PropertyAccessException) => {
    switch (exception.principalType) {
      case "group": return principals.groups.find((group) => group.id === exception.principalId)?.name ?? t("unknownGroup");
      case "integration": return principals.integrations.find((app) => app.userId === exception.principalId)?.name ?? personName(exception.principalId);
      case "person_property": return exception.principalId === "page_creator" ? t("pageCreator")
        : personProperties.find((candidate) => candidate.id === exception.principalId)?.name ?? t("unknownProperty");
      default: return personName(exception.principalId) || t("unknownPerson");
    }
  };

  const defaultLevel = draft?.defaultLevel ?? "inherit";
  const defaultText = levelText(t, defaultLevel);

  // A phone's parts take its sizes beside the computer's (PropertyAccessPhone.module.css).
  const c = (name: keyof typeof phoneCss & keyof typeof styles) => (phone ? `${styles[name]} ${phoneCss[name]}` : styles[name]);
  // English names the rule's property "For …"; Korean says nothing around it,
  // and the chip then stands alone under the title.
  const [wordsBefore = "", wordsAfter = ""] = t("propertyFor").split("<property/>");
  const propertyWords = /\S/u.test(wordsBefore + wordsAfter);
  const propertyChip = (
    <button type="button" className={c("propertyButton")} data-access-property onClick={(event) => setPropertyMenu(popoverRectOf(event.currentTarget))}>
      {property ? <PropertyIcon property={property} /> : null}<span>{property?.name ?? ""}</span><ChevronDown size={phone ? "small" : "tiny"} aria-hidden="true" />
    </button>
  );
  const main = (
    <div className={phone ? `${styles.main} ${phoneCss.main}` : styles.main}>
      <div className={c("head")}>
        <h2 className={c("title")}>{t("title")}</h2>
        {!previewShown ? <>
          {phone ? null : <>
            {wide ? <button type="button" className={styles.previewButton} data-access-preview-open data-icon-tip={t("showPreview")}
              onClick={() => setPreviewOpen(true)}>
              <EyeIcon size="small" aria-hidden="true" />{t("preview")}
            </button> : null}
            <span className={styles.headRule} aria-hidden="true" />
          </>}
          <button type="button" className={c("iconButton")} aria-label={t("close")} onClick={close}><X size={phone ? "page" : "small"} aria-hidden="true" /></button>
        </> : null}
      </div>
      {propertyWords ? <div className={c("propertyLine")}>{wordsBefore}{propertyChip}{wordsAfter}</div> : propertyChip}
      <div className={c("rule")} aria-hidden="true" />
      <div className={c("body")}>
        <section className={c("section")}>
          <div className={c("sectionTitle")}>{t("defaultTitle")}</div>
          <div className={c("sectionDescription")}>{t("defaultDescription")}</div>
          <button type="button" className={c("levelBox")} data-access-default={defaultLevel} disabled={!draft}
            onClick={(event) => setLevelMenu({ anchor: levelCardRow(event.currentTarget), index: null })}>
            <span className={styles.levelGlyph}><LevelPicture level={defaultLevel} size="mini" /></span>
            <span className={styles.levelBoxLabel}>{defaultText.label}</span><ChevronDown size="small" aria-hidden="true" />
          </button>
        </section>
        <section className={c("section")}>
          <div className={c("sectionTitle")}>{t("exceptionsTitle")}</div>
          <div className={c("sectionDescription")}>{t("exceptionsDescription")}</div>
          <div className={c("exceptions")}>
            <div className={c("exception")} data-access-full>
              {/* Its icon and name name who has full access, its level the rule, in Notion's dark tips
                  (PropertyAccessHoldersTip; the level's 300 wide and centred 6 over it); a phone's
                  show none, as Notion's touch screens show no tips. */}
              {phone ? <>
                <span className={styles.principalGlyph}><SharePeopleIcon size="small" weight="fill" aria-hidden="true" /></span>
                <span className={c("exceptionName")}>{t("fullAccessEveryone")}</span>
                <span className={c("exceptionFixed")}>{t("fullAccess")}</span>
              </> : <>
                <PropertyAccessHoldersTip fullAccess={draft?.fullAccess} className={styles.fullAccessWho}>
                  <span className={styles.principalGlyph}><SharePeopleIcon size="small" weight="fill" aria-hidden="true" /></span>
                  <span className={styles.exceptionName}>{t("fullAccessEveryone")}</span>
                </PropertyAccessHoldersTip>
                <HoverTip label={t("fullAccessTip")} placement="top" wide className={styles.exceptionFixed}>{t("fullAccess")}</HoverTip>
              </>}
            </div>
            {(draft?.exceptions ?? []).map((exception, index) => (
              <div key={`${exception.principalType}:${exception.principalId}`} className={c("exception")}
                data-access-exception={`${exception.principalType}:${exception.principalId}`}>
                <PrincipalIcon type={exception.principalType} id={exception.principalId} label={principalLabel(exception)} />
                <span className={c("exceptionName")}>{principalLabel(exception)}</span>
                <button type="button" className={c("exceptionLevel")}
                  onClick={(event) => setLevelMenu({ anchor: popoverRectOf(event.currentTarget)!, index })}>
                  {levelText(t, exception.level, exception.principalType).label}<ChevronDown size="small" aria-hidden="true" />
                </button>
              </div>
            ))}
            <button type="button" className={c("addException")} data-access-add disabled={!draft}
              onClick={(event) => setPicker(popoverRectOf(event.currentTarget))}>
              <span aria-hidden="true"><Plus size={phone ? "wide" : "regular"} /></span>{t("addException")}
            </button>
          </div>
        </section>
      </div>
      {failed.rule || failed.principals ? (
        <div className={styles.error}>
          <ReadFailed message={t(failed.rule ? "loadFailed" : "principalsLoadFailed")}
            onRetry={() => setAttempt((current) => current + 1)} />
        </div>
      ) : null}
      {error ? <div className={styles.error} role="alert">{error}</div> : null}
      <div className={c("foot")}>
        <button type="button" className={c("resetButton")} disabled={!draft || busy || (draft.defaultLevel === "inherit" && !draft.exceptions.length)}
          onClick={() => change({ defaultLevel: "inherit", exceptions: [] })}>{t("reset")}</button>
        <button type="button" className={c("saveButton")} data-access-save disabled={!dirty || busy} onClick={() => void save()}>{t("save")}</button>
      </div>
    </div>
  );
  // A phone's menus are sheets over its own, a computer's popovers by what opened them.
  const levelKind: "default" | PropertyAccessException["principalType"] = levelMenu && draft ? (levelMenu.index === null ? "default" : draft.exceptions[levelMenu.index].principalType) : "default";
  const levelChoice = levelMenu && draft ? {
    kind: levelKind,
    value: levelMenu.index === null ? draft.defaultLevel : draft.exceptions[levelMenu.index].level,
    onClose: () => setLevelMenu(null),
    onChoose: (level: PropertyAccessRule["defaultLevel"]) => levelMenu.index === null ? change({ defaultLevel: level })
      : change({ exceptions: draft.exceptions.map((exception, index) => index === levelMenu.index
        ? { ...exception, level: level as PropertyAccessException["level"] } : exception) }),
    onRemove: levelMenu.index === null ? undefined
      : () => change({ exceptions: draft.exceptions.filter((_, index) => index !== levelMenu.index) }),
  } : null;
  const pickerChoice = picker && draft ? {
    taken: draft.exceptions, groups: principals.groups, integrations: principals.integrations, personProperties,
    onClose: () => setPicker(null),
    onPick: (choice: { type: PropertyAccessException["principalType"]; id: string }) => change({ exceptions: [...draft.exceptions, { principalType: choice.type,
      principalId: choice.id, level: (propertyAccessLevels(choice.type).includes("view") ? "view" : "edit") as PropertyAccessException["level"] }] }),
  } : null;
  const menus = <>
    {levelChoice ? (phone ? <PhoneLevelSheet {...levelChoice} /> : <LevelMenu anchor={levelMenu!.anchor} {...levelChoice} />) : null}
    {pickerChoice ? (phone ? <PhonePrincipalSheet {...pickerChoice} /> : <PrincipalPicker anchor={picker!} {...pickerChoice} />) : null}
    {propertyMenu ? (phone
      ? <PhonePropertySheet properties={properties} currentId={currentId} onClose={() => setPropertyMenu(null)} onChoose={(id) => askFirst(() => setCurrentId(id))} />
      : <PropertyMenu anchor={propertyMenu} properties={properties} currentId={currentId} onClose={() => setPropertyMenu(null)}
        onChoose={(id) => askFirst(() => setCurrentId(id))} />) : null}
  </>;

  if (phone) return (
    <PhoneSheet title={null} head={false} plain label={t("title")} bodyClassName={phoneCss.sheetBody} onClose={close}
      data={{ "data-property-access-dialog": currentId }}>
      {main}{menus}
    </PhoneSheet>
  );
  return createPortal(
    <div className={styles.backdrop} style={{ zIndex }} onMouseDown={(event) => { if (event.target === event.currentTarget) close(); }}>
      <div ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-label={t("title")} data-property-access-dialog={currentId}
        data-preview={previewShown ? "true" : undefined}>
        {main}
        {previewShown ? (
          <aside className={styles.preview} data-access-preview>
            <div className={styles.previewHead}>
              <button type="button" className={styles.collapseButton} aria-label={t("hidePreview")} data-icon-tip={t("hidePreview")}
                onClick={() => setPreviewOpen(false)}>
                <DoubleChevronRight size="small" aria-hidden="true" />
              </button>
              <button type="button" className={styles.iconButton} aria-label={t("close")} onClick={close}><X size="small" aria-hidden="true" /></button>
            </div>
            <span className={styles.previewLabel}>{t("previewAs")}</span>
            <PreviewPersonPicker selfId={session.user.id} value={previewUser} onChange={setPreviewUser} />
            {preview ? <PreviewResult preview={preview} /> : failed.preview ? (
              <ReadFailed message={t("previewFailed")} onRetry={() => setAttempt((current) => current + 1)} />
            ) : null}
          </aside>
        ) : null}
      </div>
      {menus}
    </div>,
    document.body,
  );
}

/**
 * The rule's property, chosen as in Notion's (its code, read 2026-11-16: the
 * picker its database's filter and sort use, the current one ticked).
 */
function PropertyMenu({ anchor, properties, currentId, onChoose, onClose }: {
  anchor: PopoverRect;
  properties: PropertyAccessDialogProps["properties"];
  currentId: string;
  onChoose: (id: string) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation("propertyAccess");
  return (
    <AccessPopover anchor={anchor} onClose={onClose} width={240} flush>
      <DatabasePropertyPicker placeholder={t("searchProperties")} properties={properties} keepOrder checked={currentId}
        onPick={(property) => { onChoose(property.id); onClose(); }} />
    </AccessPopover>
  );
}
