import { type FormEvent, useEffect, useId, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { appPlatformErrorCodes, type AppCatalogBrowse, type AppCatalogEntry } from "@/appPlatformWire.generated";
import { LeafenApiError } from "@/apiTransport";
import { announceAppsChanged } from "@/lib/appsChangedEvents";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { appCatalogFacts } from "./appPermissionWords";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import {
  addAppCatalog, appDeveloperName, appText, listAppCatalogs, refreshAppCatalog, removeAppCatalog, uploadAppCatalog,
} from "./administratorAppsApi";
import styles from "./AdministratorApps.module.css";
import browseStyles from "./AppBrowseDialog.module.css";
import { displayDateTime } from "@/lib/displayDate";
import { AppSelect } from "@/components/AppSelect";
import { AppRowMenu } from "./AppRowMenu";
import { AppRowIcon } from "./AppRowIcon";
import { ReadFailed } from "../ReadFailed";

type Sort = "score" | "updated" | "added" | "name";

/**
 * "앱 둘러보기" (docs/app-platform.md 16): Leafen's built-in apps and the
 * apps of every catalog the server trusts — featured first, then by category, searched and filtered
 * (no outside connections, risk, paid, language) and sorted — each fetched
 * for the usual review before it installs. "목록 관리" lists the trusted
 * catalogs and adds one by its address and list key, or a closed network's
 * bundle file.
 */
export function AppBrowseDialog({ session, reviewing, onRead, onClose, allowed, onToggleAllowed }: {
  session: Session;
  /** A review opened from here is open over this window. */
  reviewing: boolean;
  /** Reads an app for its review, in the review's window over this one. */
  onRead: (app: AppCatalogEntry) => void;
  onClose: () => void;
  /** Under the allowlist policy, whether people may add an app themselves. */
  allowed?: (app: AppCatalogEntry) => boolean;
  onToggleAllowed?: (app: AppCatalogEntry, on: boolean) => Promise<void>;
}) {
  const { t, i18n } = useTranslation(["serverConsole", "common"]);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const [browse, setBrowse] = useState<AppCatalogBrowse | null>(null);
  const [view, setView] = useState<"browse" | "catalogs">("browse");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [category, setCategory] = useState("");
  const [noNetwork, setNoNetwork] = useState(false);
  const [risk, setRisk] = useState<"" | "low" | "medium">("");
  const [hidePaid, setHidePaid] = useState(false);
  const [language, setLanguage] = useState("");
  const [sort, setSort] = useState<Sort>("score");
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!busy) onClose(); }, initialFocus: closeRef,
  });

  async function run(action: () => Promise<AppCatalogBrowse | void>, fallback: string) {
    setBusy(true);
    setError("");
    try {
      const next = await action();
      if (next) setBrowse(next);
    } catch (failure) {
      setError(settingsErrorMessage(failure, t(fallback)));
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => {
    void run(() => listAppCatalogs(session), "apps.readFailed");
    // Read once when the window opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session]);

  const name = (app: AppCatalogEntry) => appText(app.name, i18n.language);
  const shown = useMemo(() => {
    const words = query.trim().toLowerCase();
    const riskRank = { low: 0, medium: 1, high: 2 } as const;
    return (browse?.apps ?? []).filter((app) => (!category || app.category === category)
      && (!noNetwork || app.network === "none")
      && (!risk || riskRank[app.risk] <= riskRank[risk])
      && (!hidePaid || !app.paid)
      && (!language || app.languages.includes(language))
      && (!words || [name(app), appText(app.summary, i18n.language), app.developer, appDeveloperName(app.developer, i18n.language), ...app.tags].join(" ").toLowerCase().includes(words)))
      .sort((left, right) => sort === "name" ? name(left).localeCompare(name(right))
        : sort === "updated" ? right.updated.localeCompare(left.updated)
          : sort === "added" ? right.added.localeCompare(left.added)
            : right.score - left.score || right.downloads - left.downloads);
    // The list follows the reader's choices and language.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [browse, category, hidePaid, i18n.language, language, noNetwork, query, risk, sort]);
  const featured = shown.filter((app) => app.featured);
  const languages = [...new Set((browse?.apps ?? []).flatMap((app) => app.languages))].sort();

  // A cancelled review gives the focus back to the row's button, which was
  // disabled while the package came and so couldn't keep it.
  const reviewFrom = useRef<HTMLButtonElement | null>(null);
  useEffect(() => {
    if (reviewing || !reviewFrom.current) return;
    reviewFrom.current.focus();
    reviewFrom.current = null;
  }, [reviewing]);

  // The review's window says it reads the app and why it couldn't, over this one.
  function install(app: AppCatalogEntry, button: HTMLButtonElement) {
    reviewFrom.current = button;
    setError("");
    onRead(app);
  }

  const card = (app: AppCatalogEntry) => {
    // An earlier version than the one installed is no update, as the server
    // reckons the versions (2026-11-12: the review called the same version
    // an update).
    const action = app.revoked ? "revoked" : !app.installedVersion ? "install" : app.installedChange === "downgrade" ? "downgrade"
      : app.installedChange === "reinstall" || app.installedVersion === app.version ? "installed" : "update";
    return (
      <li key={`${app.catalogId}/${app.id}`} className={browseStyles.browseCard} data-catalog-app={app.id}>
        <AppRowIcon tile name={name(app)} iconUrl={app.iconUrl} />
        <div className={browseStyles.browseCardText}>
          <span className={browseStyles.browseCardName}>{name(app)}</span>
          <span className={browseStyles.browseCardSummary}>{appText(app.summary, i18n.language)}</span>
          {/* A built-in app is Leafen's own: one name, not "리펜 제작 · 리펜 제공" (2026-10-19). */}
          <span className={browseStyles.browseCardMeta}>{[
            ...(app.catalogKind === "builtin" ? [t("apps.trust.builtin")] : [t("apps.by", { developer: appDeveloperName(app.developer, i18n.language) }),
              app.catalogKind === "official" ? t("apps.catalogs.kind.official") : app.catalogName]),
            t("apps.version", { version: app.version }), ...appCatalogFacts({ ...app, catalogId: app.catalogKind === "builtin" ? "builtin" : app.catalogId }, t)].join(" · ")}</span>
          <button type="button" className={action === "install" || action === "update" ? styles.primary : styles.secondary}
            disabled={busy || action === "revoked" || action === "installed"} data-catalog-install={action}
            onClick={(event) => install(app, event.currentTarget)}>{t(`apps.browse.action.${action}`, { version: app.installedVersion })}</button>
          {allowed && onToggleAllowed && !app.revoked ? (
            <label className={browseStyles.browseAllow}>
              <input type="checkbox" checked={allowed(app)} disabled={busy} data-catalog-allow={app.id}
                onChange={(event) => void run(async () => { await onToggleAllowed(app, event.target.checked); }, "apps.changeFailed")} />
              {t("apps.browse.allow")}
            </label>
          ) : null}
        </div>
      </li>
    );
  };

  return createPortal(
    <div ref={overlayRef} className={styles.overlay} style={{ zIndex }} data-app-browse>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} disabled={busy} onClick={onClose} />
      <section ref={dialogRef} className={`${styles.dialog} ${browseStyles.dialogWide}`} role="dialog" aria-modal="true" aria-labelledby={titleId}
        aria-busy={busy || undefined}>
        <div className={browseStyles.browseHead}>
          <h2 id={titleId} className={styles.dialogTitle}>{t(view === "browse" ? "apps.browse.title" : "apps.catalogs.title")}</h2>
          <button type="button" className={styles.secondary} data-app-catalogs-toggle
            onClick={() => setView(view === "browse" ? "catalogs" : "browse")}>
            {t(view === "browse" ? "apps.catalogs.open" : "apps.browse.back")}
          </button>
        </div>
        {view === "catalogs" && browse ? (
          <AppCatalogsPanel session={session} browse={browse} busy={busy} run={run} />
        ) : browse ? (
          <>
            <input className={browseStyles.browseSearch} type="search" value={query} placeholder={t("apps.browse.search")}
              aria-label={t("apps.browse.search")} onChange={(event) => setQuery(event.target.value)} />
            <div className={browseStyles.browseTabs} role="tablist" aria-label={t("apps.browse.categories")}>
              {["", ...browse.categories].map((key) => (
                <button key={key || "all"} type="button" role="tab" aria-selected={category === key} data-catalog-category={key || "all"}
                  onClick={() => setCategory(key)}>{key ? t(`apps.browse.category.${key}`) : t("apps.browse.all")}</button>
              ))}
            </div>
            <div className={browseStyles.browseFilters}>
              <label><input type="checkbox" checked={noNetwork} onChange={(event) => setNoNetwork(event.target.checked)} />
                {t("apps.browse.noNetworkOnly")}</label>
              <label><input type="checkbox" checked={hidePaid} onChange={(event) => setHidePaid(event.target.checked)} />
                {t("apps.browse.hidePaid")}</label>
              <AppSelect aria-label={t("apps.browse.riskLabel")} value={risk} onChange={(event) => setRisk(event.target.value as typeof risk)}>
                <option value="">{t("apps.browse.riskAny")}</option>
                <option value="low">{t("apps.browse.riskLow")}</option>
                <option value="medium">{t("apps.browse.riskMedium")}</option>
              </AppSelect>
              <AppSelect aria-label={t("apps.browse.languageLabel")} value={language} onChange={(event) => setLanguage(event.target.value)}>
                <option value="">{t("apps.browse.languageAny")}</option>
                {languages.map((code) => <option key={code} value={code}>{code}</option>)}
              </AppSelect>
              <AppSelect aria-label={t("apps.browse.sortLabel")} value={sort} onChange={(event) => setSort(event.target.value as Sort)}>
                {(["score", "updated", "added", "name"] as const).map((key) => <option key={key} value={key}>{t(`apps.browse.sort.${key}`)}</option>)}
              </AppSelect>
            </div>
            {!browse.catalogs.length ? <p className={styles.note}>{t("apps.browse.noCatalogs")}</p> : null}
            {featured.length && !query.trim() && !category ? (
              <>
                <h3 className={styles.permissionsTitle}>{t("apps.browse.featured")}</h3>
                <ul className={browseStyles.browseGrid} data-catalog-featured>{featured.map(card)}</ul>
              </>
            ) : null}
            <h3 className={styles.permissionsTitle}>{t("apps.browse.count", { count: shown.length })}</h3>
            {shown.length ? <ul className={browseStyles.browseGrid} data-catalog-apps>{shown.map(card)}</ul>
              : browse.apps.length ? <p className={styles.note}>{t("apps.browse.none")}</p> : null}
          </>
        ) : null}
        {/* The catalogs not read: what failed, with a way to read them again. */}
        {!browse && error ? <ReadFailed message={error} onRetry={() => void run(() => listAppCatalogs(session), "apps.readFailed")} />
          : error ? <div className={styles.error} role="alert">{error}</div> : null}
        <div className={styles.dialogActions}>
          <button ref={closeRef} type="button" className={styles.secondary} disabled={busy} onClick={onClose}>{t("common:actions.close")}</button>
        </div>
      </section>
    </div>,
    document.body,
  );
}

function AppCatalogsPanel({ session, browse, busy, run }: {
  session: Session;
  browse: AppCatalogBrowse;
  busy: boolean;
  run: (action: () => Promise<AppCatalogBrowse | void>, fallback: string) => Promise<void>;
}) {
  const { t } = useTranslation(["serverConsole", "common"]);
  const [address, setAddress] = useState({ name: "", url: "", publicKey: "" });
  const [bundle, setBundle] = useState<{ name: string; publicKey: string; file: File | null }>({ name: "", publicKey: "", file: null });
  // The file is chosen with a button that names it, as the app file's is.
  const bundleFileRef = useRef<HTMLInputElement>(null);
  // A form to add a catalog opens from its button, one at a time (the
  // settings rows' rule: a form to make something comes when asked for).
  const [opened, setOpened] = useState<"" | "address" | "bundle">("");
  // A catalog that couldn't be read or doesn't hold up says why under its
  // own form, beside its button, rather than at the window's foot.
  const [formError, setFormError] = useState<{ form: "address" | "bundle"; message: string } | null>(null);
  const catalogFailure = (failure: unknown) => {
    const code = failure instanceof LeafenApiError ? failure.code : "";
    if (code === appPlatformErrorCodes.catalogAddressRefused) return t("apps.catalogs.addressRefused");
    if (code === appPlatformErrorCodes.catalogUnreachable) return t("apps.catalogs.unreachable");
    if (code === appPlatformErrorCodes.catalogInvalid) return t("apps.catalogs.invalid");
    return "";
  };
  // A catalog added, read again or removed changes what /apps offers, under
  // the settings window: its lists read again.
  const change = (action: () => Promise<AppCatalogBrowse | void>, fallback: string) => run(async () => {
    const next = await action();
    if (next) announceAppsChanged();
    return next;
  }, fallback);
  const adding = (form: "address" | "bundle", action: () => Promise<AppCatalogBrowse>) => async () => {
    setFormError(null);
    try {
      return await action();
    } catch (failure) {
      const message = catalogFailure(failure);
      if (!message) throw failure;
      setFormError({ form, message });
    }
  };

  function add(event: FormEvent) {
    event.preventDefault();
    void change(adding("address", async () => {
      const next = await addAppCatalog(session, address);
      setAddress({ name: "", url: "", publicKey: "" });
      setOpened("");
      return next;
    }), "apps.catalogs.addFailed");
  }

  function upload(event: FormEvent) {
    event.preventDefault();
    if (!bundle.file) return;
    const file = bundle.file;
    void change(adding("bundle", async () => {
      const next = await uploadAppCatalog(session, { name: bundle.name, publicKey: bundle.publicKey, bundle: file });
      setBundle({ name: "", publicKey: "", file: null });
      setOpened("");
      return next;
    }), "apps.catalogs.addFailed");
  }

  return (
    <>
      <p className={styles.note}>{t("apps.catalogs.body")}</p>
      {browse.catalogs.length ? browse.catalogs.map((catalog) => (
        <div key={catalog.id} className={`${styles.jobRow} ${styles.guestLink}`} data-app-catalog={catalog.id}>
          <span className={styles.secretName}>
            <strong>{catalog.name} <span className={browseStyles.browseKind}>{t(`apps.catalogs.kind.${catalog.kind}`)}</span></strong>
            <span>{[t(`apps.catalogs.source.${catalog.source}`), catalog.url, t("apps.catalogs.key", { fingerprint: catalog.keyFingerprint }),
              t("apps.catalogs.apps", { count: catalog.appCount }),
              catalog.fetchedAt ? t("apps.catalogs.read", { date: displayDateTime(catalog.fetchedAt) }) : ""]
              .filter(Boolean).join(" · ")}</span>
            {catalog.lastError ? <span className={styles.runError}>{catalog.lastError}</span> : null}
          </span>
          <AppRowMenu label={t("apps.rowMenu", { name: catalog.name })} disabled={busy} items={[
            ...(catalog.source === "url" ? [{ key: "refresh", label: t("apps.catalogs.refresh"), data: "data-app-catalog-refresh",
              onSelect: () => void change(() => refreshAppCatalog(session, catalog.id), "apps.catalogs.refreshFailed") }] : []),
            // The official catalog stays, as the built-in apps do: the server puts it back.
            ...(catalog.kind === "official" ? [] : [{ key: "remove", label: t("apps.catalogs.remove"), data: "data-app-catalog-remove",
              onSelect: () => void change(() => removeAppCatalog(session, catalog.id), "apps.changeFailed") }]),
          ]} />
        </div>
      )) : <p className={styles.note}>{t("apps.catalogs.none")}</p>}
      {opened ? null : (
        <div className={browseStyles.catalogAdd}>
          <button type="button" className={styles.secondary} disabled={busy} data-app-catalog-add-open
            onClick={() => { setFormError(null); setOpened("address"); }}>{t("apps.catalogs.addTitle")}</button>
          <button type="button" className={styles.secondary} disabled={busy} data-app-catalog-upload-open
            onClick={() => { setFormError(null); setOpened("bundle"); }}>{t("apps.catalogs.uploadTitle")}</button>
        </div>
      )}
      {opened === "address" ? <form className={browseStyles.browseForm} onSubmit={add} data-app-catalog-add>
        <h3 className={styles.permissionsTitle}>{t("apps.catalogs.addTitle")}</h3>
        <input value={address.name} placeholder={t("apps.catalogs.name")} aria-label={t("apps.catalogs.name")} required
          onChange={(event) => setAddress({ ...address, name: event.target.value })} />
        <input value={address.url} type="url" placeholder="https://…/catalog.json" aria-label={t("apps.catalogs.url")} required
          onChange={(event) => setAddress({ ...address, url: event.target.value })} />
        <input value={address.publicKey} placeholder={t("apps.catalogs.publicKey")} aria-label={t("apps.catalogs.publicKey")} required
          onChange={(event) => setAddress({ ...address, publicKey: event.target.value })} />
        <span className={browseStyles.catalogFormActions}>
          <button type="button" className={styles.secondary} disabled={busy} onClick={() => setOpened("")}>{t("common:actions.cancel")}</button>
          <button type="submit" className={styles.primary} disabled={busy}>{t("apps.catalogs.add")}</button>
        </span>
        {formError?.form === "address" ? <p className={styles.error} role="alert" data-app-catalog-add-error>{formError.message}</p> : null}
      </form> : null}
      {opened === "bundle" ? <form className={browseStyles.browseForm} onSubmit={upload} data-app-catalog-upload>
        <h3 className={styles.permissionsTitle}>{t("apps.catalogs.uploadTitle")}</h3>
        <input value={bundle.name} placeholder={t("apps.catalogs.name")} aria-label={t("apps.catalogs.bundleName")} required
          onChange={(event) => setBundle({ ...bundle, name: event.target.value })} />
        <input value={bundle.publicKey} placeholder={t("apps.catalogs.publicKey")} aria-label={t("apps.catalogs.bundleKey")} required
          onChange={(event) => setBundle({ ...bundle, publicKey: event.target.value })} />
        <input ref={bundleFileRef} type="file" accept=".zip,application/zip" hidden data-app-catalog-bundle-file
          onChange={(event) => setBundle({ ...bundle, file: event.target.files?.[0] ?? null })} />
        <button type="button" className={styles.secondary} disabled={busy} data-app-catalog-bundle-choose
          onClick={() => bundleFileRef.current?.click()}>{bundle.file ? bundle.file.name : t("apps.catalogs.bundleFile")}</button>
        <span className={browseStyles.catalogFormActions}>
          <button type="button" className={styles.secondary} disabled={busy} onClick={() => setOpened("")}>{t("common:actions.cancel")}</button>
          <button type="submit" className={styles.primary} disabled={busy || !bundle.file}>{t("apps.catalogs.upload")}</button>
        </span>
        {formError?.form === "bundle" ? <p className={styles.error} role="alert" data-app-catalog-upload-error>{formError.message}</p> : null}
      </form> : null}
    </>
  );
}
