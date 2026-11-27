import { type FormEvent, useEffect, useId, useMemo, useRef, useState } from "react";
import type { MakeOnce } from "@/lib/uncertainWrites";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { AppSelect } from "@/components/AppSelect";
import { ChevronDown, ChevronRight } from "@/icons/leafen";
import { QuestionCircleIcon, WarningFillIcon, WebhookIcon } from "@/icons/leafen/automationIcons";
import {
  createInternalConnectionWebhook, updateInternalConnectionWebhook,
  type InternalConnection, type InternalConnectionWebhook, type InternalConnectionWebhooks,
} from "@/lib/internalConnections";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { settingsErrorMessage, writeUncertainMark } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import own from "./InternalConnectionWebhooks.module.css";
import portal from "./DeveloperSelect.module.css";

/** A box that is checked, unchecked or partly checked, as Notion's event tree draws them. */
function TreeCheck({ state, label, onChange }: { state: "all" | "some" | "none"; label: string; onChange(checked: boolean): void }) {
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => { if (ref.current) ref.current.indeterminate = state === "some"; }, [state]);
  return <input ref={ref} type="checkbox" className={own.check} checked={state === "all"} aria-label={label}
    onChange={(event) => onChange(event.currentTarget.checked || state === "some")} />;
}

/**
 * "구독 생성하기", as Notion's (seen 2026-10-31): a 600px window over the
 * page — the webhook mark, its title and what it is for, "웹훅 URL",
 * "API 버전" (the newest first chosen) and "들을 이벤트를 선택하세요.": a
 * 192px tree, "이벤트(N개 선택됨)" over each kind's row (a box, its name,
 * "N개 이벤트" and ⌄ that opens its events, each with what it means on
 * hover). Comments warn while the connection can't read them. Editing,
 * it is "구독 편집" over "구독 업데이트", dimmed until something changes; a
 * verified subscription keeps its address.
 */
export function InternalConnectionWebhookDialog({ session, workspaceId, connection, options, webhook, make, onSaved, onClose }: {
  session: Session; workspaceId: string; connection: InternalConnection; options: InternalConnectionWebhooks;
  webhook?: InternalConnectionWebhook; onSaved(saved: InternalConnectionWebhook): void; onClose(): void;
  /** Runs a new one's make so one sent without the answer isn't made twice. */
  make: MakeOnce;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null), dialogRef = useRef<HTMLElement>(null), urlRef = useRef<HTMLInputElement>(null);
  const [url, setUrl] = useState(webhook?.url ?? "");
  const [version, setVersion] = useState(webhook?.apiVersion ?? options.apiVersions.at(-1) ?? "");
  const [chosen, setChosen] = useState(() => new Set(webhook?.eventTypes ?? options.defaultEventTypes));
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!busy) onClose(); }, initialFocus: urlRef,
  });
  const groups = useMemo(() => {
    const byKind = new Map<string, string[]>();
    for (const type of options.eventTypes) {
      const kind = type.slice(0, type.indexOf("."));
      byKind.set(kind, [...(byKind.get(kind) ?? []), type]);
    }
    return [...byKind];
  }, [options.eventTypes]);
  const stateOf = (types: readonly string[]) => {
    const count = types.filter((type) => chosen.has(type)).length;
    return count === 0 ? "none" : count === types.length ? "all" : "some";
  };
  const choose = (types: readonly string[], checked: boolean) => setChosen((current) => {
    const next = new Set(current);
    for (const type of types) { if (checked) next.add(type); else next.delete(type); }
    return next;
  });
  const addressFixed = Boolean(webhook?.verified);
  // Editing, "구독 업데이트" waits for a change, as Notion's (seen 2026-11-21).
  const changed = !webhook || url.trim() !== webhook.url || version !== webhook.apiVersion
    || chosen.size !== webhook.eventTypes.length || webhook.eventTypes.some((type) => !chosen.has(type));
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || !url.trim() || chosen.size === 0 || !changed) return;
    setBusy(true);
    setError("");
    const eventTypes = options.eventTypes.filter((type) => chosen.has(type));
    try {
      onSaved(webhook
        ? await updateInternalConnectionWebhook(session, workspaceId, connection.id, webhook.id,
          { eventTypes, apiVersion: version, ...(addressFixed ? {} : { url: url.trim() }) })
        : await make(() => createInternalConnectionWebhook(session, workspaceId, connection.id, { url: url.trim(), apiVersion: version, eventTypes })));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("appFailed")));
    } finally {
      setBusy(false);
    }
  }
  const label = (key: string, values?: Record<string, unknown>) => t(`developerTools.internal.webhooks.${key}`, values);
  return createPortal(
    <div ref={overlayRef} className={own.overlay} style={{ zIndex }} data-connection-webhook-dialog>
      <button type="button" className={own.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} disabled={busy} onClick={onClose} />
      <section ref={dialogRef} className={own.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} aria-busy={busy || undefined}>
        <WebhookIcon size="card" aria-hidden="true" className={own.dialogMark} />
        <h2 id={titleId} className={own.dialogTitle}>{label(webhook ? "editTitle" : "create")}</h2>
        <p className={own.dialogNote}>{label("createDescription")}</p>
        <form className={own.form} onSubmit={(event) => void submit(event)}>
          <label className={own.field}>
            <span className={own.label}>{label("url")}</span>
            <input ref={urlRef} type="url" className={own.input} value={url} placeholder={label("urlPlaceholder")} disabled={addressFixed}
              data-connection-webhook-url onChange={(event) => setUrl(event.currentTarget.value)} />
            <span className={own.help}>{label(addressFixed ? "urlFixed" : "urlHelp")}</span>
          </label>
          <div className={own.field}>
            <span className={own.label}>{label("version")}</span>
            <AppSelect aria-label={label("version")} value={version} data-connection-webhook-version
              className={portal.select} settingsList menuWidth={200} menuGap={0} menuAlign="start"
              onChange={(event) => {
                // A new subscription takes the version's events, as Notion's dialog does.
                const picked = options.versionEventTypes[event.target.value];
                if (!webhook && picked) setChosen(new Set(picked));
                setVersion(event.target.value);
              }}>
              {options.apiVersions.map((candidate) => <option key={candidate} value={candidate}>{candidate}</option>)}
            </AppSelect>
            <span className={own.help}>{label("versionHelp")}</span>
          </div>
          <div className={own.field}>
            <span className={own.label}>{label("events")}</span>
            <div className={own.tree} data-connection-webhook-events>
              <div className={own.treeHead}>
                <TreeCheck state={stateOf(options.eventTypes)} label={label("allEvents", { count: chosen.size })}
                  onChange={(checked) => choose(options.eventTypes, checked)} />
                <span>{label("allEvents", { count: chosen.size })}</span>
              </div>
              {groups.map(([kind, types]) => (
                <div key={kind} data-connection-webhook-group={kind}>
                  <div className={own.groupRow}>
                    <TreeCheck state={stateOf(types)} label={label(`group.${kind}`)} onChange={(checked) => choose(types, checked)} />
                    <span className={own.groupName}>{label(`group.${kind}`)}</span>
                    {kind === "comment" && !connection.capabilities.readComments ? (
                      <span className={own.warn} title={label("commentsNeedRead")} aria-label={label("commentsNeedRead")}>
                        <WarningFillIcon size="tiny" aria-hidden="true" /></span>
                    ) : null}
                    <span className={own.groupCount}>{label("groupCount", { count: types.length })}</span>
                    <button type="button" className={own.groupToggle} aria-expanded={open.has(kind)} aria-label={label(`group.${kind}`)}
                      onClick={() => setOpen((current) => {
                        const next = new Set(current);
                        if (!next.delete(kind)) next.add(kind);
                        return next;
                      })}>
                      {open.has(kind) ? <ChevronDown size="small" aria-hidden="true" /> : <ChevronRight size="small" aria-hidden="true" />}
                    </button>
                  </div>
                  {open.has(kind) ? types.map((type) => (
                    <label key={type} className={own.eventRow} data-connection-webhook-event={type}>
                      <input type="checkbox" className={own.check} checked={chosen.has(type)}
                        onChange={(event) => choose([type], event.currentTarget.checked)} />
                      <span>{label(`event.${type}`)}</span>
                      <span className={own.eventHelp} title={label(`eventHelp.${type}`)} aria-label={label(`eventHelp.${type}`)}>
                        <QuestionCircleIcon size="tiny" aria-hidden="true" /></span>
                    </label>
                  )) : null}
                </div>
              ))}
            </div>
          </div>
          {error ? <p className={own.error} role="alert" {...writeUncertainMark(error)}>{error}</p> : null}
          <button type="submit" className={own.primary} disabled={busy || !url.trim() || chosen.size === 0 || !changed} data-connection-webhook-save>
            {label(webhook ? "update" : "create")}</button>
          <button type="button" className={own.secondary} disabled={busy} onClick={onClose}>{t("common:actions.cancel")}</button>
        </form>
      </section>
    </div>,
    document.body,
  );
}
