import { useEffect, useId, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { userErrorText, writeRefusalText } from "@/lib/settingsErrors";
import type { RowSendTarget, RowSendTargets } from "@/structureWire.generated";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { useProductToast } from "@/lib/productToast";
import { LeafenApiError } from "@/apiTransport";
import { structureErrorCodes } from "@/structureWire.generated";
import { OPEN_SEND_ITEM_EVENT, readSendTargets, sendRow, type OpenSendItemDetail } from "@/lib/rowSend";
import styles from "./SendItemDialog.module.css";
import { AppSelect } from "@/components/AppSelect";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { signedInPersonId } from "@/lib/peopleDirectory";
import { usePeopleDirectoryRevision } from "@/lib/workspacePeopleDirectory";
import { untitledPageDisplayTitle } from "../../lib/pageTitle";
import { SelfMark } from "@/components/people/SelfMark";

/**
 * "보내기" (Leafen's own): pass a
 * database row to a member with a word, naming them in one of its person
 * properties. Someone who can't open it gets it shared by a sender with full
 * access; for anyone else the window says why it can't go.
 */
export function SendItemDialog({ detail, onClose }: { detail: OpenSendItemDetail; onClose(): void }) {
  const { rowId, title } = detail;
  const { t } = useTranslation(["structureSend", "common", "propertyCell"]);
  usePeopleDirectoryRevision();
  const me = signedInPersonId();
  const { notify } = useProductToast();
  const [targets, setTargets] = useState<RowSendTargets | null>(null);
  const [query, setQuery] = useState("");
  const [chosen, setChosen] = useState<RowSendTarget | null>(null);
  const [property, setProperty] = useState("");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState("");
  const dialogRef = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], onEscape: () => { if (!busy) onClose(); },
  });

  // The workspace's people come a window at a time, each shown as it comes,
  // to the last.
  useEffect(() => {
    const controller = new AbortController();
    setTargets(null);
    void (async () => {
      let cursor = "";
      do {
        const read = await readSendTargets(rowId, cursor, controller.signal);
        if (controller.signal.aborted) return;
        setTargets((current) => current ? { ...current, targets: [...current.targets, ...read.targets] } : read);
        cursor = read.nextCursor ?? "";
      } while (cursor);
    })().catch((error) => { if (!controller.signal.aborted) setFailed(userErrorText(error, t("loadFailed"))); });
    return () => controller.abort();
  }, [rowId]);

  const shown = useMemo(() => {
    const needle = query.trim().toLowerCase();
    // The reader first, as the person property's picker lists them.
    return (targets?.targets ?? []).filter((target) => !needle || target.name.toLowerCase().includes(needle))
      .sort((a, b) => Number(b.userId === me) - Number(a.userId === me));
  }, [me, query, targets]);

  async function send() {
    if (!chosen || busy) return;
    setBusy(true);
    setFailed("");
    try {
      await sendRow(rowId, {
        to: chosen.userId, message: message.trim(), ...(property ? { property } : {}),
      });
      notify(t("sent", { name: chosen.name }));
      onClose();
    } catch (error) {
      setFailed(error instanceof LeafenApiError && error.code === structureErrorCodes.unseen
        ? t("cantShare", { name: chosen.name })
        : writeRefusalText(error, t("sendFailed")));
    } finally {
      setBusy(false);
    }
  }

  return createPortal(
    <div className={styles.overlay} style={{ zIndex }}>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} onClick={() => { if (!busy) onClose(); }} />
      <div ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} data-send-item-dialog>
        <h2 id={titleId}>{t("title")}</h2>
        <p>{title || untitledPageDisplayTitle("page")}</p>
        <label className={styles.field}>
          {t("to")}
          <input className={styles.search} value={query} placeholder={t("search")} onChange={(event) => setQuery(event.target.value)} data-send-search />
        </label>
        <ul className={styles.targets} role="listbox" aria-label={t("to")}>
          {shown.map((target) => (
            <li key={target.userId}>
              <button type="button" role="option" aria-selected={chosen?.userId === target.userId} className={styles.target}
                data-send-target={target.userId} onClick={() => setChosen(target)}>
                <PersonAvatar userId={target.userId} name={target.name} size={20} />
                <span className={styles.name}>{target.name || t("untitled")}{target.userId === me
                  ? <SelfMark className={styles.me} text={t("propertyCell:youMark")} /> : null}</span>
                {!target.canSee ? <span className={styles.noAccount} data-send-cant-see>{t("cantSee")}</span> : null}
              </button>
            </li>
          ))}
          {targets && shown.length === 0 ? <li className={styles.empty}>{t("none")}</li> : null}
        </ul>
        {(targets?.properties.length ?? 0) > 1 ? (
          <label className={styles.field}>
            {t("property")}
            <AppSelect aria-label={t("property")} className={styles.choice} value={property || targets?.properties[0]?.id} onChange={(event) => setProperty(event.target.value)} data-send-property>
              {targets?.properties.map((candidate) => <option key={candidate.id} value={candidate.id}>{candidate.name}</option>)}
            </AppSelect>
          </label>
        ) : null}
        <label className={styles.field}>
          {t("message")}
          <textarea className={styles.message} value={message} maxLength={2000} rows={3} onChange={(event) => setMessage(event.target.value)} data-send-message />
        </label>
        {chosen && !chosen.canSee ? (
          <p className={targets?.canShare ? styles.note : styles.reason} data-send-share-note>
            {t(targets?.canShare ? "willShare" : "cantShare", { name: chosen.name })}
          </p>
        ) : null}
        {failed ? <p className={styles.reason} role="alert">{failed}</p> : null}
        <div className={styles.actions}>
          <button type="button" className={styles.cancel} onClick={onClose} disabled={busy}>{t("common:actions.cancel")}</button>
          <button type="button" className={styles.primary} onClick={() => void send()} disabled={busy || !chosen || (!chosen.canSee && !targets?.canShare)} data-send-submit>{t("send")}</button>
        </div>
      </div>
    </div>,
    document.body,
  );
}

/** Opens the "보내기" a row page's menu asks for (requestSendItemDialog). */
export function SendItemDialogHost() {
  const [detail, setDetail] = useState<OpenSendItemDetail | null>(null);
  useEffect(() => {
    const listener = (event: Event) => setDetail((event as CustomEvent<OpenSendItemDetail>).detail);
    window.addEventListener(OPEN_SEND_ITEM_EVENT, listener);
    return () => window.removeEventListener(OPEN_SEND_ITEM_EVENT, listener);
  }, []);
  return detail ? <SendItemDialog key={detail.rowId} detail={detail} onClose={() => setDetail(null)} /> : null;
}
