import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { LeafenApiError } from "@/apiTransport";
import { X } from "@/components/icons";
import { ReadFailed } from "@/components/ReadFailed";
import {
  addAllowedEmailDomain,
  readAllowedEmailDomains,
  removeAllowedEmailDomain,
  type WorkspaceAllowedEmailDomains as AllowedEmailDomains,
} from "@/lib/workspaceAllowedEmailDomains";
import type { Session } from "@/types";
import local from "./WorkspaceAllowedEmailDomains.module.css";
import { styles } from "./workspaceSettingsOverviewStyles";
import general from "./WorkspaceGeneral.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/**
 * Notion's "허용된 이메일 도메인" in 일반 (seen 2026-10-03): a field taking
 * the email domains whose people join the workspace as members without an
 * invitation. Owners add only domains the workspace's members use.
 */
export function WorkspaceAllowedEmailDomains({ session, workspaceId }: { session: Session; workspaceId: string }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [state, setState] = useState<AllowedEmailDomains | null>(null);
  const [draft, setDraft] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [loadFailed, setLoadFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    setState(null);
    setLoadFailed(false);
    readAllowedEmailDomains(session, workspaceId, controller.signal)
      .then((next) => { if (!controller.signal.aborted) setState(next); })
      .catch(() => { if (!controller.signal.aborted) setLoadFailed(true); });
    return () => controller.abort();
  }, [attempt, session, workspaceId]);

  async function change(run: () => Promise<AllowedEmailDomains>) {
    setBusy(true);
    setError("");
    try {
      setState(await run());
      return true;
    } catch (failure) {
      const code = failure instanceof LeafenApiError ? failure.code : "";
      setError(code === "LEAFEN_ALLOWED_EMAIL_DOMAIN_INVALID" ? t("allowedEmailDomainInvalid")
        : code === "LEAFEN_ALLOWED_EMAIL_DOMAIN_UNUSED" ? t("allowedEmailDomainUnused")
          : t("allowedEmailDomainFailed"));
      return false;
    } finally {
      setBusy(false);
    }
  }

  async function add() {
    if (busy) return;
    const domain = draft.trim().replace(/^@/u, "");
    if (!domain) return;
    if (await change(() => addAllowedEmailDomain(session, workspaceId, domain))) setDraft("");
  }

  return (
    // Flat in its section, as Notion's (seen 2026-10-30): the name over its words, the domains under them.
    <div className={general.stacked} data-allowed-email-domains>
      <strong>{t("allowedDomainsField")}</strong>
      <p>{t("allowedEmailDomainsDesc")}</p>
      <div className={local.domainField} data-busy={busy ? "true" : undefined}>
        {state?.domains.map(({ domain }) => (
          <span key={domain} className={local.domainTag} data-allowed-email-domain={domain}>
            {domain}
            <button
              type="button"
              aria-label={t("allowedEmailDomainRemove", { domain })}
              disabled={busy}
              // The field keeps the keys: the button going away sent them to the settings
              // search and scrolled the page 104px (2026-11-05). Notion's own field is not
              // measured (its workspace's domains were left alone); of its other tag fields
              // (2026-11-15, worker Chrome, no script before the press, a letter typed after),
              // a multi-select's picker keeps the keys, the share window's address field
              // drops them to the page.
              onClick={() => { inputRef.current?.focus({ preventScroll: true }); void change(() => removeAllowedEmailDomain(session, workspaceId, domain)); }}
            >
              <X size="tiny" aria-hidden="true" />
            </button>
          </span>
        ))}
        <input
          ref={inputRef}
          value={draft}
          aria-label={t("allowedEmailDomainsLabel")}
          placeholder={t("allowedEmailDomainsPlaceholder")}
          spellCheck={false}
          autoCapitalize="none"
          disabled={!state}
          aria-busy={busy || undefined}
          onChange={(event) => {
            setDraft(event.target.value);
            if (error) setError("");
          }}
          onKeyDown={(event) => {
            if (!fieldEnter(event)) return;
            event.preventDefault();
            void add();
          }}
          {...(error && error !== t("allowedEmailDomainFailed")
            ? { "aria-invalid": true as const, "aria-describedby": "allowed-email-domain-error" } : {})}
        />
      </div>
      {loadFailed ? (
        <ReadFailed message={t("allowedEmailDomainsLoadFailed")} onRetry={() => setAttempt((current) => current + 1)} />
      ) : error ? (
        <div id="allowed-email-domain-error" className={styles.fieldMessage} data-tone="error" role="alert">{error}</div>
      ) : state && !state.joinsAvailable ? (
        <div className={styles.fieldMessage}>{t("allowedEmailDomainsNeedVerification")}</div>
      ) : null}
    </div>
  );
}
