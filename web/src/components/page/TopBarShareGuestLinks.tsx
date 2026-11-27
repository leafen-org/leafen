import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { AppSelect } from "@/components/AppSelect";
import { InternalConnectionPagesPicker } from "@/components/developers/InternalConnectionPagesPicker";
import { useProductPages } from "@/components/product/productPagesContext";
import { accessRequestErrorCodes, type PageGuestLink, type PageGuestLinkInput } from "@/accessRequestWire.generated";
import { X } from "@/icons/leafen";
import { useCopyNotice } from "@/lib/copyNotice";
import { LeafenApiError } from "@/apiTransport";
import { activeDateLocale } from "@/lib/i18n";
import { isPageBusyRefusal } from "@/lib/pageBusy";
import { usePageNames } from "@/lib/pageNames";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { serverUrl } from "@/lib/serverOrigin";
import { userErrorText } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { sharePermissionLabel } from "./TopBarSharePermissionPicker";
import own from "./TopBarShareGuestLinks.module.css";

type Role = PageGuestLinkInput["role"];
const EXPIRY_DAYS = [1, 7, 30] as const;

/**
 * "게스트 초대 링크" in "고급" (Leafen's own: the user's 2026-11-23 "코드도 있어서
 * 바로 접속되는 계정으로", page_guest_links.go): a link that lets whoever opens
 * it in as a guest of the pages chosen — this one first — with the role
 * chosen, until it expires, for one person unless it says otherwise. Its
 * code shows once, as it is made (the server keeps only its hash); the
 * page's links still usable are listed, each turned off with a press —
 * every one of them, window after window as the server gives them, since a
 * link left out lets people in unseen till it expires (2026-11-23: past
 * fifty, the oldest weren't shown).
 */
export function TopBarShareGuestLinks(props: { workspaceId: string; pageId: string; disabled: boolean; guestsAllowed: boolean }) {
  const session = useProductPages()?.session;
  return session ? <GuestLinks session={session} {...props} /> : null;
}

function GuestLinks({ session, workspaceId, pageId, disabled, guestsAllowed }: {
  session: Session; workspaceId: string; pageId: string; disabled: boolean; guestsAllowed: boolean;
}) {
  const { t } = useTranslation("topBar");
  const notice = useCopyNotice();
  const pageNames = usePageNames();
  const pages = useProductPages()?.pages;
  const [links, setLinks] = useState<PageGuestLink[] | null>(null);
  const [making, setMaking] = useState(false);
  const [chosen, setChosen] = useState<string[]>([pageId]);
  const [role, setRole] = useState<Role>("edit");
  const [days, setDays] = useState<number>(7);
  const [once, setOnce] = useState(true);
  const [picking, setPicking] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [made, setMade] = useState("");
  // Each read of the links counts up, and an older one still reading on stops.
  const linksRead = useRef(0);
  const load = useCallback(async (signal?: AbortSignal) => {
    const read = ++linksRead.current;
    const current = () => read === linksRead.current && !signal?.aborted;
    try {
      let list = await api.listPageGuestLinks(session, pageId, signal);
      if (!current()) return;
      setLinks(list.links);
      while (list.nextCursor) {
        list = await api.listPageGuestLinks(session, pageId, signal, list.nextCursor);
        if (!current()) return;
        const more = list.links;
        setLinks((shown) => [...(shown ?? []), ...more.filter((link) => !(shown ?? []).some((item) => item.id === link.id))]);
      }
    } catch {
      if (current()) setLinks((shown) => shown ?? []);
    }
  }, [session, pageId]);
  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);
  const title = useCallback((id: string) => {
    const page = pages?.find((candidate) => candidate.id === id);
    return (page ? pageNames.title(page) : "") || untitledPageDisplayTitle("page");
  }, [pages, pageNames]);
  const chosenSet = useMemo(() => new Set(chosen), [chosen]);
  // The chosen page other than this one that an operation holds, which a refusal names.
  const heldElsewhere = (failure: unknown) => {
    const held = isPageBusyRefusal(failure) && failure instanceof LeafenApiError ? failure.targetId ?? "" : "";
    return held !== pageId && chosen.includes(held) ? held : "";
  };
  async function make() {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const created = await api.createPageGuestLink(session, workspaceId, {
        pageIds: chosen, role, expiresInDays: days, ...(once ? {} : { reusable: true }),
      });
      setMade(serverUrl(created.path));
      setMaking(false);
      void load();
    } catch (failure) {
      const code = (failure as { code?: string } | null)?.code;
      const held = heldElsewhere(failure);
      setError(code === "LEAFEN_ORGANIZATION_SHARING_SECURITY_BLOCKED" ? t("guestLinkBlocked")
        : code === accessRequestErrorCodes.guestInviteRequiresApproval ? t("guestLinkOwnersOnly")
          // A chosen page the person can't give a link to is named not found (page_guest_link_refusals.go).
          : code === "LEAFEN_PAGE_NOT_FOUND" ? t("guestLinkNoAccess")
            // A chosen page an operation holds a while is named, when it isn't this one (page_held_refusal.go).
            : held ? t("guestLinkPageHeld", { title: title(held), held: userErrorText(failure, "") })
              : userErrorText(failure, t("guestLinkFailed")));
    } finally {
      setBusy(false);
    }
  }
  async function turnOff(link: PageGuestLink) {
    try {
      await api.revokePageGuestLink(session, link.id);
      setLinks((current) => (current ?? []).filter((item) => item.id !== link.id));
    } catch (failure) {
      // One already gone, or not the person's to turn off, leaves the list as the server has it.
      const gone = (failure as { code?: string } | null)?.code === accessRequestErrorCodes.guestLinkNotFound;
      setError(gone ? t("guestLinkGone") : userErrorText(failure, t("guestLinkOffFailed")));
      if (gone) void load();
    }
  }
  const until = (at: number) => new Intl.DateTimeFormat(activeDateLocale(), { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" }).format(new Date(at));
  if (!guestsAllowed) return <p className={own.note} data-share-guest-links-off>{t("guestsTurnedOff")}</p>;
  return <div className={own.body} data-share-guest-links>
    <p className={own.note}>{t("guestLinksNote")}</p>
    {made ? <div className={own.made} data-share-guest-link-made>
      <input className={own.address} readOnly value={made} aria-label={t("guestLinks")} onFocus={(event) => event.currentTarget.select()} />
      <button type="button" className={own.primary} data-share-guest-link-copy
        onClick={() => void notice(made, t("copiedLink"))}>{t("copyLink")}</button>
      <p className={own.note}>{t("guestLinkShownOnce")}</p>
    </div> : null}
    {making ? <div className={own.form} data-share-guest-link-form>
      <div className={own.field}>
        <span className={own.fieldName}>{t("guestLinkPages")}</span>
        <div className={own.chips}>
          {chosen.map((id) => <span key={id} className={own.chip} data-share-guest-link-page={id}>
            <span>{title(id)}</span>
            {id === pageId ? null : <button type="button" aria-label={t("guestLinkRemovePage", { title: title(id) })}
              onClick={() => setChosen((current) => current.filter((item) => item !== id))}><X size="tiny" aria-hidden="true" /></button>}
          </span>)}
          <button type="button" className={own.add} data-share-guest-link-add onClick={() => setPicking(true)}>{t("guestLinkAddPages")}</button>
        </div>
      </div>
      <label className={own.field}>
        <span className={own.fieldName}>{t("guestLinkRole")}</span>
        <AppSelect look="value" value={role} aria-label={t("guestLinkRole")} data-share-guest-link-role=""
          onChange={(change) => setRole(change.target.value as Role)}>
          {(["edit", "comment", "view"] as const).map((value) => <option key={value} value={value}>{sharePermissionLabel(value, t)}</option>)}
        </AppSelect>
      </label>
      <label className={own.field}>
        <span className={own.fieldName}>{t("guestLinkExpiry")}</span>
        <AppSelect look="value" value={days} aria-label={t("guestLinkExpiry")} data-share-guest-link-days=""
          onChange={(change) => setDays(Number(change.target.value))}>
          {EXPIRY_DAYS.map((value) => <option key={value} value={value}>{t(`guestLinkDays${value}`)}</option>)}
        </AppSelect>
      </label>
      <label className={own.check}>
        <input type="checkbox" checked={once} data-share-guest-link-once onChange={(event) => setOnce(event.currentTarget.checked)} />
        <span>{t("guestLinkOnce")}</span>
      </label>
      <div className={own.actions}>
        <button type="button" className={own.secondary} onClick={() => { setMaking(false); setError(""); }}>{t("guestLinkCancel")}</button>
        <button type="button" className={own.primary} disabled={busy || disabled} data-share-guest-link-create onClick={() => void make()}>
          {t("guestLinkCreate")}
        </button>
      </div>
    </div> : <button type="button" className={own.open} disabled={disabled} data-share-guest-link-new
      onClick={() => { setMaking(true); setMade(""); setChosen([pageId]); }}>{t("guestLinkNew")}</button>}
    {error ? <p className={own.error} role="alert" data-share-guest-link-error>{error}</p> : null}
    {(links ?? []).map((link) => <div key={link.id} className={own.link} data-share-guest-link={link.id}>
      <span className={own.linkText}>
        <span>{[sharePermissionLabel(link.role, t), t("guestLinkUntil", { date: until(link.expiresAt) }),
          link.reusable ? t("guestLinkJoined", { count: link.uses ?? 0 }) : t("guestLinkOnceMark")].join(" · ")}</span>
        <span className={own.linkPages}>{link.pages.map((page) => page.title || untitledPageDisplayTitle("page")).join(", ")}</span>
      </span>
      {/* Wherever the server lets the reader turn it off: their own, or any as the workspace's owner (page_guest_link_turn_off.go). */}
      {link.revocable ? <button type="button" className={own.secondary} disabled={disabled} data-share-guest-link-off
        onClick={() => void turnOff(link)}>{t("guestLinkTurnOff")}</button> : null}
    </div>)}
    {picking ? <InternalConnectionPagesPicker session={session} chosen={chosenSet} title={t("guestLinkPickTitle")}
      toggle={async (id, add) => setChosen((current) => add ? [...current, id] : current.filter((item) => item !== id || item === pageId))}
      onChanged={() => undefined} onClose={() => setPicking(false)} /> : null}
  </div>;
}
