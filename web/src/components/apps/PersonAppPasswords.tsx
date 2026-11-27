import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { LeafenApiError } from "@/apiTransport";
import type { AppPasswordApp, AppPasswordCreated, AppPasswordList } from "@/appPlatformWire.generated";
import { appText } from "@/components/admin/administratorAppsApi";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { formatFileSize } from "@/components/database/files";
import { activeDateLocale } from "@/lib/i18n";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { createPersonPassword, deletePersonPassword, readPersonPasswords } from "./personAppsApi";
import adminStyles from "@/components/admin/AdministratorApps.module.css";
import styles from "./PersonAppsPage.module.css";
import own from "./PersonAppPasswords.module.css";
import { useCopyNotice } from "@/lib/copyNotice";
import { keptAppsList, useAppsListReading } from "@/lib/appsChangedEvents";

// How few passwords left in an app before the limit is said.
const PASSWORDS_NOTICE_LEFT = 3;

/**
 * App passwords (document apps decision of 2026-10-10, 8): for each app with
 * endpoints the person may use, the addresses programs outside Leafen reach
 * and the person's passwords for them — named, shown once when made, taken
 * back whenever they like.
 */
export function PersonAppPasswords({ session, ready = true }: { session: Session; ready?: boolean }) {
  const { t, i18n } = useTranslation("common");
  // The list read last in this tab shows at once, the server's answer replacing it.
  const [apps, setApps] = useState<AppPasswordApp[]>(() => keptAppsList<AppPasswordList>(session, "passwords")?.apps ?? []);
  // An app's "앱 비밀번호 만들기" opens /apps#app-passwords: the section comes
  // into view once it has its apps (2026-10-16: it sat at the page's bottom
  // to be found) and the app cards above it have come (2026-11-05: they came
  // after, and pushed it away from under the person's pointer).
  const sectionRef = useRef<HTMLElement>(null);
  // From an app the link names its install: that app's block comes into view
  // with its name field ready, not the first app's (2026-11-06: Web Clips
  // landed on the vault's block, its own below the fold). Once per address:
  // an app installed afterwards on the same screen leaves the scroll alone.
  const shownFor = useRef("");
  useEffect(() => {
    const address = window.location.search + window.location.hash;
    if (!(ready && apps.length && window.location.hash === "#app-passwords") || shownFor.current === address) return;
    shownFor.current = address;
    const install = new URLSearchParams(window.location.search).get("install");
    const block = install ? sectionRef.current?.querySelector<HTMLElement>(`[data-person-app-password-app^="${CSS.escape(`${install}/`)}"]`) : null;
    (block ?? sectionRef.current)?.scrollIntoView({ block: "start" });
    block?.querySelector<HTMLInputElement>("[data-person-app-password-name]")?.focus({ preventScroll: true });
  }, [apps.length, ready]);
  // How many passwords one app keeps, said only when few are left.
  const [limit, setLimit] = useState(() => keptAppsList<AppPasswordList>(session, "passwords")?.passwordLimit ?? 0);
  const shown = (list: AppPasswordList) => {
    setApps(list.apps);
    setLimit(list.passwordLimit);
  };
  const [names, setNames] = useState<Record<string, string>>({});
  const [created, setCreated] = useState<{ app: string; answer: AppPasswordCreated } | null>(null);
  const notice = useCopyNotice();
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");

  // Read again when an app is installed, removed or given to the person on
  // this screen or in a window over it; a reading that fails says so until
  // one works.
  const readError = useRef("");
  const adopt = useAppsListReading(session, "passwords", readPersonPasswords, (list) => {
    shown(list);
    setError((current) => (current && current === readError.current ? "" : current));
  }, (failure) => {
    readError.current = settingsErrorMessage(failure, t("appPeople.passwords.readFailed"));
    setError(readError.current);
  });

  const key = (app: AppPasswordApp) => `${app.installId}/${app.tenancy}`;
  // A button that goes with what it did — a row's "철회", the shown
  // password's "완료" — hands the focus on to the app's name field, to make
  // another, before it goes (it fell to the page, 2026-11-15: the next Tab
  // started at the page's top), by mouse as by keys.
  const focusName = (app: AppPasswordApp) => sectionRef.current?.querySelector<HTMLInputElement>(
    `[data-person-app-password-app="${CSS.escape(key(app))}"] [data-person-app-password-name]`)?.focus();

  async function create(app: AppPasswordApp) {
    setBusy(key(app));
    setError("");
    try {
      const answer = await createPersonPassword(session, { installId: app.installId, tenancy: app.tenancy, name: names[key(app)] ?? "" });
      setCreated({ app: key(app), answer });
      setNames((current) => ({ ...current, [key(app)]: "" }));
      adopt(await readPersonPasswords(session));
    } catch (failure) {
      setError(failure instanceof LeafenApiError && failure.code === "LEAFEN_APP_PASSWORD_LIMIT"
        ? t("appPeople.passwords.limit") : settingsErrorMessage(failure, t("appPeople.passwords.createFailed")));
    } finally {
      setBusy("");
    }
  }

  // Revoking asks as every revoke does, a question and what it ends
  // (2026-11-08: the row's button turned into "확인 / 그대로 두기").
  async function revoke(app: AppPasswordApp, passwordId: string, name: string) {
    if (!await confirmAction({
      message: t("appPeople.passwords.revokeConfirm", { name }), detail: t("appPeople.passwords.revokeConfirmDetail"),
      confirmLabel: t("appPeople.passwords.revoke"), danger: true,
    })) return;
    setBusy(passwordId);
    setError("");
    try {
      adopt(await deletePersonPassword(session, passwordId));
      setCreated((current) => (current?.answer.password.id === passwordId ? null : current));
      focusName(app);
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("appPeople.passwords.revokeFailed")));
    } finally {
      setBusy("");
    }
  }

  async function copy(_id: string, text: string) {
    await notice(text);
  }

  if (!apps.length && !error) return null;
  const when = (at: number) => new Date(at).toLocaleString(activeDateLocale(), { dateStyle: "long", timeStyle: "short" });
  const copyButton = (id: string, text: string) => (
    <button type="button" className={adminStyles.secondary} onClick={() => void copy(id, text)}>
      {t("appPeople.passwords.copy")}
    </button>
  );
  return (
    <section ref={sectionRef} id="app-passwords" className={styles.credentials} data-person-app-passwords>
      <h2 className={styles.heading}>{t("appPeople.passwords.title")}</h2>
      <p className={styles.note}>{t("appPeople.passwords.note")}</p>
      {error ? <div className={adminStyles.error} role="alert">{error}</div> : null}
      {apps.map((app) => (
        <div key={key(app)} className={own.app} data-person-app-password-app={key(app)}>
          <h3 className={own.appName}>
            {appText(app.appName, i18n.language)}
            {apps.some((other) => other.installId === app.installId && other.tenancy !== app.tenancy) ? <span> · {app.tenancyName}</span> : null}
          </h3>
          {app.storedBytes > 0 || app.storageLimitBytes > 0 ? (
            <p className={styles.note} data-person-app-password-storage>
              {app.storageLimitBytes > 0
                ? t("appPeople.passwords.storedOf", { used: formatFileSize(app.storedBytes), limit: formatFileSize(app.storageLimitBytes) })
                : t("appPeople.passwords.stored", { used: formatFileSize(app.storedBytes) })}
            </p>
          ) : null}
          {app.endpoints.map((endpoint) => (
            <div key={endpoint.key} className={own.address}>
              <span className={own.addressText}>
                <span className={styles.credentialName}>{appText(endpoint.name, i18n.language)}</span>
                <code>{endpoint.address}</code>
              </span>
              {copyButton(`${key(app)}/${endpoint.key}`, endpoint.address)}
            </div>
          ))}
          {created?.app === key(app) ? (
            <div className={own.secret} role="status" data-person-app-password-secret>
              <strong>{t("appPeople.passwords.createdTitle")}</strong>
              <span className={styles.note}>{t("appPeople.passwords.createdNote")}</span>
              <label className={own.field}>
                <span>{t("appPeople.passwords.username")}</span>
                <span className={own.fieldRow}>
                  <input readOnly value={created.answer.username} onFocus={(event) => event.target.select()} />
                  {copyButton("created-username", created.answer.username)}
                </span>
              </label>
              <label className={own.field}>
                <span>{t("appPeople.passwords.password")}</span>
                <span className={own.fieldRow}>
                  <input readOnly value={created.answer.secret} spellCheck={false} onFocus={(event) => event.target.select()} />
                  {copyButton("created-secret", created.answer.secret)}
                </span>
              </label>
              <button type="button" className={adminStyles.secondary} onClick={() => { focusName(app); setCreated(null); }}>
                {t("appPeople.passwords.done")}</button>
            </div>
          ) : null}
          <ul className={styles.credentialList}>
            {app.passwords.map((password) => (
              <li key={password.id} className={styles.credential} data-person-app-password={password.id}>
                <span className={styles.credentialText}>
                  <span className={styles.credentialName}>{password.name}</span>
                  <span className={styles.note}>{t("appPeople.passwords.item", {
                    hint: password.hint, created: when(password.createdAt),
                    used: password.lastUsedAt ? when(password.lastUsedAt) : t("appPeople.passwords.unused"),
                  })}</span>
                </span>
                <button type="button" className={adminStyles.secondary} aria-label={t("appPeople.passwords.revokeNamed", { name: password.name })}
                  disabled={busy === password.id} data-person-app-password-revoke onClick={() => void revoke(app, password.id, password.name)}>
                  {t("appPeople.passwords.revoke")}</button>
              </li>
            ))}
          </ul>
          {limit > 0 && limit - app.passwords.length <= PASSWORDS_NOTICE_LEFT ? (
            <p className={app.passwords.length >= limit ? adminStyles.error : styles.note}
              data-person-app-password-capacity={app.passwords.length >= limit ? "full" : "near"}>
              {app.passwords.length >= limit
                ? t("appPeople.passwords.full", { limit })
                : t("appPeople.passwords.near", { left: limit - app.passwords.length, limit })}
            </p>
          ) : null}
          <form className={own.add} onSubmit={(event) => { event.preventDefault(); void create(app); }}>
            <input data-person-app-password-name value={names[key(app)] ?? ""} maxLength={100} placeholder={t("appPeople.passwords.namePlaceholder")}
              aria-label={t("appPeople.passwords.namePlaceholder")}
              onChange={(event) => setNames((current) => ({ ...current, [key(app)]: event.target.value }))} />
            <button type="submit" className={adminStyles.primary} disabled={busy === key(app) || !(names[key(app)] ?? "").trim() || (limit > 0 && app.passwords.length >= limit)}>
              {t("appPeople.passwords.create")}
            </button>
          </form>
        </div>
      ))}
    </section>
  );
}
