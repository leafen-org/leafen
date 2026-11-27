import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { AppGroup, AppInstall, AppTenancy, AppTenancyPerson } from "@/appPlatformWire.generated";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { listTenancyGroups, listTenancyPeople, setInstalledAppGrants } from "./administratorAppsApi";
import styles from "./AdministratorApps.module.css";
import pickerStyles from "./AppGrantPeopleDialog.module.css";
import { ReadFailed } from "../ReadFailed";

/**
 * Who may use an app in one organization instead of everyone: its groups,
 * whose people now may (SCIM's groups follow their directory), and its
 * people one by one, found by name or email, checked or not.
 */
export function AppGrantPeopleDialog({ session, install, tenancy, name, onSaved, onClose }: {
  session: Session;
  install: AppInstall;
  tenancy: AppTenancy;
  name: string;
  onSaved: (install: AppInstall) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation(["serverConsole", "common"]);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const current = install.grants.find((grant) => grant.tenancyKey === tenancy.key);
  const [chosen, setChosen] = useState<Set<string>>(new Set(current?.userIds ?? []));
  const [groups, setGroups] = useState<Set<string>>(new Set(current?.groupIds ?? []));
  const [tenancyGroups, setTenancyGroups] = useState<AppGroup[]>([]);
  // Who may be chosen, once read; a failed read of them or of the groups is
  // said in their place with a way to read again, never as no one found.
  const [people, setPeople] = useState<AppTenancyPerson[] | null>(null);
  const [readFailed, setReadFailed] = useState(false);
  const [readTurn, setReadTurn] = useState(0);
  const [more, setMore] = useState(false);
  const [query, setQuery] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!busy) onClose(); }, initialFocus: searchRef,
  });

  useEffect(() => {
    let live = true;
    const timer = window.setTimeout(() => {
      listTenancyPeople(session, tenancy.key, query).then((listed) => {
        if (!live) return;
        setPeople(listed.people);
        setMore(listed.more);
      }, () => { if (live) { setReadFailed(true); setPeople(null); } });
    }, query ? 200 : 0);
    return () => { live = false; window.clearTimeout(timer); };
  }, [query, readTurn, session, tenancy.key]);

  // The organization's groups, read a window at a time as the dialog opens.
  useEffect(() => {
    const controller = new AbortController();
    listTenancyGroups(session, tenancy.key, controller.signal).then(
      (listed) => { if (!controller.signal.aborted) setTenancyGroups(listed); },
      () => { if (!controller.signal.aborted) setReadFailed(true); },
    );
    return () => controller.abort();
  }, [readTurn, session, tenancy.key]);

  const toggle = (userId: string, on: boolean) => setChosen((previous) => {
    const next = new Set(previous);
    if (on) next.add(userId);
    else next.delete(userId);
    return next;
  });

  async function save() {
    setBusy(true);
    setError("");
    try {
      onSaved(await setInstalledAppGrants(session, install.installId,
        [{ tenancyKey: tenancy.key, everyone: false, userIds: [...chosen], groupIds: [...groups] }]));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("apps.changeFailed")));
      setBusy(false);
    }
  }

  return createPortal(
    <div ref={overlayRef} className={styles.overlay} style={{ zIndex }} data-app-grant-people>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} disabled={busy} onClick={onClose} />
      <section ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} aria-busy={busy || undefined}>
        <h2 id={titleId} className={styles.dialogTitle}>{t("apps.people.title", { name })}</h2>
        <p className={styles.note}>{t("apps.people.lead", { tenancy: tenancy.name || t("apps.serverTenancy") })}</p>
        {tenancyGroups.length ? (
          <>
            <h3 className={pickerStyles.heading}>{t("apps.people.groups")}</h3>
            <ul className={`${pickerStyles.list} ${pickerStyles.groupList}`}>
              {tenancyGroups.map((group) => (
                <li key={group.id}>
                  <label className={pickerStyles.person} data-app-grant-group={group.id}>
                    <input type="checkbox" checked={groups.has(group.id)} disabled={busy}
                      onChange={(event) => setGroups((previous) => {
                        const next = new Set(previous);
                        if (event.target.checked) next.add(group.id);
                        else next.delete(group.id);
                        return next;
                      })} />
                    <span className={pickerStyles.name}>{group.name}</span>
                    <span className={pickerStyles.email}>
                      {[t("apps.people.groupMembers", { count: group.members }), group.managedBy === "scim" ? "SCIM" : ""].filter(Boolean).join(" · ")}
                    </span>
                  </label>
                </li>
              ))}
            </ul>
            <h3 className={pickerStyles.heading}>{t("apps.people.peopleHeading")}</h3>
          </>
        ) : null}
        <input ref={searchRef} type="search" className={pickerStyles.search} value={query} placeholder={t("apps.people.search")}
          aria-label={t("apps.people.search")} onChange={(event) => setQuery(event.target.value)} data-app-grant-search />
        <ul className={pickerStyles.list}>
          {(people ?? []).map((person) => (
            <li key={person.userId}>
              <label className={pickerStyles.person} data-app-grant-person={person.userId}>
                <input type="checkbox" checked={chosen.has(person.userId)} disabled={busy}
                  onChange={(event) => toggle(person.userId, event.target.checked)} />
                <span className={pickerStyles.name}>{person.name || person.email}</span>
                <span className={pickerStyles.email}>{person.email}</span>
              </label>
            </li>
          ))}
        </ul>
        {readFailed ? <ReadFailed message={t("apps.people.readFailed")} onRetry={() => { setReadFailed(false); setReadTurn((turn) => turn + 1); }} />
          : people && !people.length ? <p className={styles.note}>{t("apps.people.none")}</p> : null}
        {more ? <p className={styles.note}>{t("apps.people.more")}</p> : null}
        {error ? <div className={styles.error} role="alert">{error}</div> : null}
        <div className={styles.dialogActions}>
          <span className={pickerStyles.count} data-app-grant-count>
            {groups.size ? t("apps.people.countGroups", { groups: groups.size, count: chosen.size }) : t("apps.people.count", { count: chosen.size })}
          </span>
          <button type="button" className={styles.cancel} disabled={busy} onClick={onClose}>{t("common:actions.cancel")}</button>
          <button type="button" className={styles.primary} disabled={busy} data-app-grant-save onClick={() => void save()}>{t("apps.people.save")}</button>
        </div>
      </section>
    </div>,
    document.body,
  );
}
