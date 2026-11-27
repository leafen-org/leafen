import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { DotsHorizontal, SignInIcon } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePopoverAppear } from "@/lib/popoverAppear";
import { pushRoute } from "@/lib/router";
import { rememberShareLinkJoin } from "@/lib/shareLinkJoin";
import { styles as topBar } from "./page/topBarStyles";
import css from "./SharedPageMore.module.css";

/**
 * A page read through a web link by someone signed out: "⋯" after its
 * discussions, as Notion's (seen 2026-11-15), leading to this server's
 * sign-in as a published site's "⋯" does (PublicSiteActions), so someone of
 * the page's organization goes on into the app with the link's role
 * (SharedPageView). Notion's band offering its own sign-up, and its
 * "페이지 신고" and "쿠키 설정", belong to Notion's service.
 */
export function SharedPageMore({ token, pageId }: { token: string; pageId: string }) {
  const { t } = useTranslation("publicSiteView");
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null), menuRef = useRef<HTMLDivElement>(null);
  usePopoverAppear(open, menuRef);
  useOverlayLayer(open, { kind: "menu", restoreFocus: false, ref, onEscape: () => setOpen(false), onOutsidePress: () => setOpen(false) });
  return <div ref={ref} className={css.root}>
    <button type="button" className={topBar.action} aria-label={t("moreActions")} title={t("moreActions")}
      aria-haspopup="menu" aria-expanded={open} data-shared-page-more onClick={() => setOpen(shown => !shown)}>
      <DotsHorizontal size="small" aria-hidden="true" />
    </button>
    {open ? <div ref={menuRef} className={css.menu} role="menu">
      <button type="button" role="menuitem" className={css.item} data-shared-page-sign-in
        onClick={() => { rememberShareLinkJoin({ token, pageId }); pushRoute("/"); }}>
        <SignInIcon size="regular" aria-hidden="true" />{t("signIn")}
      </button>
    </div> : null}
  </div>;
}
