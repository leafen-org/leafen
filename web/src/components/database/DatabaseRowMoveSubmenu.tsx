import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronRight, FileText, MoveIcon, TableIcon } from "@/icons/leafen";
import { useProductToast } from "@/lib/productToast";
import styles from "../RowMenu.module.css";
import type { DatabaseRowMoveDrop, DatabaseRowMoveTarget } from "./DatabaseRowMenu";
import { useDatabaseRowMoveTargets, type DatabaseRowMoveTargetSearch } from "./useDatabaseRowMoveTargets";
import { NativeActionMenuItem, NativeMenuHeader, NativeMenuSection } from "@/lib/nativeActionMenu";
import { PhoneSheetGroup, PhoneSheetRow } from "@/components/shell/PhoneSheet";
import { PhoneSheetNoResults } from "@/components/shell/PhoneSheetNoResults";
import { MovePhoneSearch } from "@/components/MovePhoneSearch";
import { untitledPageDisplayTitle } from "../../lib/pageTitle";
import { plainTitleOf } from "@/lib/closedMentions";
import { fieldFocusOnOpen } from "@/lib/screenKind";

/**
 * A database row menu's "옮기기" and the places it lists under it: the
 * databases on the row's page, then the workspace's databases and pages
 * that match what is typed. A move that would drop values asks first and
 * names the properties.
 */
export function DatabaseRowMoveSubmenu({ busy, disabled, hint, moveTargets, searchMoveTargets, onMove, onPreviewMove, onOpen }: {
  busy: boolean;
  disabled: boolean;
  /** The key that moves the row, shown in place of the arrow. */
  hint?: string;
  moveTargets: DatabaseRowMoveTarget[];
  searchMoveTargets?: DatabaseRowMoveTargetSearch;
  /** Moves the row, dropping the values of the accepted properties. */
  onMove: (target: DatabaseRowMoveTarget, acceptDroppedPropertyIds: string[]) => Promise<void>;
  /** Lists the values a move to the target would drop. */
  onPreviewMove?: (target: DatabaseRowMoveTarget) => Promise<DatabaseRowMoveDrop[]>;
  /** Opens the pages' move popover instead, as Notion's menus do (DatabaseRowMovePopover). */
  onOpen?: () => void;
}) {
  const { t } = useTranslation(["rowMenu", "databaseView"]);
  const { notify } = useProductToast();
  const [open, setOpen] = useState(false);
  const moveList = useDatabaseRowMoveTargets(moveTargets, searchMoveTargets, open);
  const [returnToTarget, setReturnToTarget] = useState<string | null>(null);
  const [checking, setChecking] = useState(false);
  const [confirm, setConfirm] = useState<{ target: DatabaseRowMoveTarget; dropped: DatabaseRowMoveDrop[] } | null>(null);

  async function choose(target: DatabaseRowMoveTarget) {
    if (!onPreviewMove) {
      await onMove(target, []);
      return;
    }
    setChecking(true);
    let dropped: DatabaseRowMoveDrop[];
    try {
      dropped = await onPreviewMove(target);
    } catch {
      notify(t("moveConfirm.checkFailed"), "error");
      return;
    } finally {
      setChecking(false);
    }
    if (dropped.length === 0) await onMove(target, []);
    else setConfirm({ target, dropped });
  }

  return (
    <>
      <NativeActionMenuItem
        type="button"
        className={styles.item}
        data-menu-item
        role="menuitem"
        disabled={disabled || busy || (!onOpen && moveTargets.length === 0 && !searchMoveTargets)}
        aria-expanded={onOpen ? undefined : open}
        onClick={() => {
          if (onOpen) { onOpen(); return; }
          setConfirm(null);
          setReturnToTarget(null);
          setOpen((current) => !current);
        }}
      >
        <MoveIcon size="regular" aria-hidden="true" />
        <span>{t("actions.moveTo")}</span>
        {hint
          ? <span className={styles.itemHint}>{hint}</span>
          : <ChevronRight className={styles.menuDisclosure} data-open={open ? "true" : undefined} size="small" aria-hidden="true" />}
      </NativeActionMenuItem>
      {open && (
        <NativeMenuSection className={styles.compactSubmenu} aria-label={t("actions.moveTo")}>
          {confirm ? (
            <NativeMenuSection role="alertdialog" aria-label={t("moveConfirm.title")} data-row-move-confirm>
              <NativeMenuHeader>
              <p className={styles.moveConfirmText}>
                {t("moveConfirm.body", { title: confirm.target.title })}
              </p>
              <ul className={styles.moveConfirmList}>
                {confirm.dropped.map((property) => <li key={property.propertyId}>{property.name}</li>)}
              </ul>
              </NativeMenuHeader>
              <NativeActionMenuItem focusOnMount type="button" className={`${styles.subItem} ${styles.dangerOnHover}`} data-menu-item role="menuitem"
                disabled={busy}
                onClick={() => void onMove(confirm.target, confirm.dropped.map(({ propertyId }) => propertyId))}>
                {t("moveConfirm.confirm")}
              </NativeActionMenuItem>
              <NativeActionMenuItem type="button" className={styles.subItem} data-menu-item role="menuitem"
                onClick={() => { setReturnToTarget(`${confirm.target.kind ?? "database"}:${confirm.target.id}`); setConfirm(null); }}>
                {t("moveConfirm.cancel")}
              </NativeActionMenuItem>
            </NativeMenuSection>
          ) : (
            <>
              {searchMoveTargets && (
                <NativeMenuHeader><input data-native-menu-search className={styles.moveSearch} value={moveList.query} autoFocus={fieldFocusOnOpen()}
                  aria-label={t("moveSearch")} placeholder={t("moveSearch")}
                  onChange={(event) => moveList.setQuery(event.currentTarget.value)} /></NativeMenuHeader>
              )}
              {moveList.targets.map((target) => (
                <NativeActionMenuItem
                  key={`${target.kind ?? "database"}:${target.id}`}
                  focusOnMount={returnToTarget === `${target.kind ?? "database"}:${target.id}`}
                  type="button"
                  className={styles.subItem}
                  data-menu-item
                  data-row-move-target={target.kind ?? "database"}
                  role="menuitem"
                  disabled={busy || checking}
                  onClick={() => void choose(target)}
                >
                  {target.kind === "page"
                    ? <FileText size="small" aria-hidden="true" />
                    : <TableIcon size="small" aria-hidden="true" />}
                  <span>{plainTitleOf(target) || untitledPageDisplayTitle(target.kind === "page" ? undefined : "database")}</span>
                </NativeActionMenuItem>
              ))}
              {searchMoveTargets && moveList.targets.length === 0 && (
                <NativeMenuHeader className={styles.subItem} role="status">{t("databaseView:noSearchResults")}</NativeMenuHeader>
              )}
            </>
          )}
        </NativeMenuSection>
      )}
    </>
  );
}

/**
 * "옮기기" on a phone where the row is neither read nor in a view to move as
 * the pages' move does (DatabaseRowMovePopover), in a sheet of that one's
 * kind (RowMenuPhoneSheet): a search over the places a row may go, a row
 * each, and the drop question in the sheet when a move would lose values.
 */
export function DatabaseRowMovePhoneList({ busy, moveTargets, searchMoveTargets, onMove, onPreviewMove, onDone }: {
  busy: boolean;
  moveTargets: DatabaseRowMoveTarget[];
  searchMoveTargets?: DatabaseRowMoveTargetSearch;
  onMove: (target: DatabaseRowMoveTarget, acceptDroppedPropertyIds: string[]) => Promise<void>;
  onPreviewMove?: (target: DatabaseRowMoveTarget) => Promise<DatabaseRowMoveDrop[]>;
  onDone: () => void;
}) {
  const { t } = useTranslation(["rowMenu", "databaseView", "moveToDialog"]);
  const { notify } = useProductToast();
  const moveList = useDatabaseRowMoveTargets(moveTargets, searchMoveTargets, true);
  const [checking, setChecking] = useState(false);
  const [confirm, setConfirm] = useState<{ target: DatabaseRowMoveTarget; dropped: DatabaseRowMoveDrop[] } | null>(null);

  async function move(target: DatabaseRowMoveTarget, dropped: string[]) {
    onDone();
    await onMove(target, dropped);
  }

  async function choose(target: DatabaseRowMoveTarget) {
    if (!onPreviewMove) return move(target, []);
    setChecking(true);
    let dropped: DatabaseRowMoveDrop[];
    try {
      dropped = await onPreviewMove(target);
    } catch {
      notify(t("moveConfirm.checkFailed"), "error");
      return;
    } finally {
      setChecking(false);
    }
    if (dropped.length === 0) await move(target, []);
    else setConfirm({ target, dropped });
  }

  if (confirm) return (
    <div data-row-move-confirm>
      <p className={styles.moveConfirmText}>{t("moveConfirm.body", { title: confirm.target.title })}</p>
      <ul className={styles.moveConfirmList}>
        {confirm.dropped.map((property) => <li key={property.propertyId}>{property.name}</li>)}
      </ul>
      <PhoneSheetGroup>
        <PhoneSheetRow more={false} danger label={t("moveConfirm.confirm")} disabled={busy}
          onPress={() => void move(confirm.target, confirm.dropped.map(({ propertyId }) => propertyId))} />
        <PhoneSheetRow more={false} label={t("moveConfirm.cancel")} onPress={() => setConfirm(null)} />
      </PhoneSheetGroup>
    </div>
  );
  return <>
    {searchMoveTargets ? <MovePhoneSearch value={moveList.query} label={t("moveToDialog:search.label")} placeholder={t("moveToDialog:search.placeholder")}
      onValueChange={moveList.setQuery} /> : null}
    {searchMoveTargets && moveList.targets.length === 0 ? <PhoneSheetNoResults>{t("databaseView:noSearchResults")}</PhoneSheetNoResults> : (
      <PhoneSheetGroup>
        {moveList.targets.map((target) => (
          <PhoneSheetRow key={`${target.kind ?? "database"}:${target.id}`} more={false} disabled={busy || checking}
            data={{ "data-row-move-target": target.kind ?? "database" }}
            icon={target.kind === "page" ? <FileText size="regular" aria-hidden="true" /> : <TableIcon size="regular" aria-hidden="true" />}
            label={target.title || untitledPageDisplayTitle(target.kind === "page" ? undefined : "database")} onPress={() => void choose(target)} />
        ))}
      </PhoneSheetGroup>
    )}
  </>;
}
