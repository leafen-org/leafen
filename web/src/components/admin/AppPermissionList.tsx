import { useCallback, useEffect, useRef, useState, type Ref } from "react";
import type { AppPermission } from "@/appPlatformWire.generated";
import { groupAppPermissions, permissionText, type AppPermissionGroup } from "./appPermissionWords";
import styles from "./AdministratorApps.module.css";
import reviewStyles from "./AppPackageReviewDialog.module.css";

type Translate = (key: string, values?: Record<string, unknown>) => string;

// A group shows this many lines until asked for the rest. Lines an update
// adds and high-risk lines always show: only the quieter ones fold (decided
// 2026-11-05, a mail app's credential lines were folded away).
const SHOWN = 3;
// A list this short shows whole: folding a few lines saves nothing.
const WHOLE_UP_TO = 10;

/**
 * A control that shows more lines and then goes once pressed — a group's
 * "n개 더 보기", an update's "모두 보기" — hands the focus it held to the
 * first line it showed, so the keys carry on from there rather than from the
 * window's first control or the control after the list (2026-11-15: an
 * update's "모두 보기" by Enter sent it past the list to the trust box).
 * `within` holds the lines; `reveal` is called from the control's press with
 * the index of the first line it shows among them.
 */
export function useRevealedLineFocus<T extends HTMLElement>() {
  const within = useRef<T>(null);
  const [revealed, setRevealed] = useState<number | null>(null);
  useEffect(() => {
    if (revealed === null) return;
    within.current?.querySelectorAll<HTMLElement>("li")[revealed]?.focus({ preventScroll: false });
    setRevealed(null);
  }, [revealed]);
  const reveal = useCallback((event: { currentTarget: Element }, index = 0) => {
    if (event.currentTarget === event.currentTarget.ownerDocument.activeElement) setRevealed(index);
  }, []);
  return { within, reveal };
}

function PermissionGroup({ group, permissions, added, folds, t, language }: {
  group: AppPermissionGroup;
  permissions: readonly AppPermission[];
  added?: (permission: AppPermission) => boolean;
  folds: boolean;
  t: Translate;
  language: string;
}) {
  const [open, setOpen] = useState(false);
  const { within: listRef, reveal } = useRevealedLineFocus<HTMLUListElement>();
  const always = (permission: AppPermission) => Boolean(added?.(permission)) || permission.risk === "high";
  const ordered = [...permissions].sort((left, right) =>
    Number(Boolean(added?.(right))) - Number(Boolean(added?.(left))) || Number(right.risk === "high") - Number(left.risk === "high"));
  const shown = ordered.filter((permission, index) => !folds || open || index < SHOWN || always(permission));
  const hidden = ordered.length - shown.length;
  return (
    <div className={styles.permissionGroup} data-app-permission-group={group}>
      <div className={styles.permissionGroupTitle}>{t(`apps.review.groups.${group}`)}</div>
      <ul ref={listRef} className={styles.permissions}>
        {ordered.map((permission, index) => (
          <li key={`${permission.kind}-${index}`} hidden={!shown.includes(permission)} data-app-permission={permission.kind} tabIndex={-1}
            data-app-permission-change={added?.(permission) ? "added" : undefined}>
            {permissionText(permission, t, language)}
            {added?.(permission) ? <span className={reviewStyles.added}>{t("apps.review.permissionAdded")}</span> : null}
          </li>
        ))}
      </ul>
      {hidden > 0 ? (
        <button type="button" className={styles.permissionsMore} data-app-permissions-more onClick={(event) => {
          setOpen(true);
          reveal(event, ordered.findIndex((permission) => !shown.includes(permission)));
        }}>
          {t("apps.review.showMore", { count: hidden })}</button>
      ) : null}
    </div>
  );
}

/** What an app can do, by what it is about, without alarm: reading it is the point, not being warned off. */
export function AppPermissionList({ permissions, added, t, language, ref }: {
  permissions: readonly AppPermission[];
  added?: (permission: AppPermission) => boolean;
  t: Translate;
  language: string;
  /** The list's lines, for the control that shows it (useRevealedLineFocus). */
  ref?: Ref<HTMLDivElement>;
}) {
  return (
    <div ref={ref} data-app-permissions>
      {groupAppPermissions(permissions).map(({ group, permissions: lines }) => (
        <PermissionGroup key={group} group={group} permissions={lines} added={added} folds={permissions.length > WHOLE_UP_TO}
          t={t} language={language} />
      ))}
    </div>
  );
}
