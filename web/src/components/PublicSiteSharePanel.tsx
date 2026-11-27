import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { DotsHorizontal, LinkIcon, Search, ServiceIcon } from "@/icons/leafen";
import { useCopyNotice } from "@/lib/copyNotice";
import { SITE_VISITOR_SHARE_SERVICES, socialShareHref } from "@/lib/socialShare";
import { TitleWithCustomEmoji, type NamedTitle } from "./page/TitleWithCustomEmoji";
import styles from "./PublicSiteSharePanel.module.css";

const SERVICE_NAMES = { linkedin: "LinkedIn", x: "X", whatsapp: "WhatsApp", facebook: "Facebook", email: "Email" } as const;
const SERVICE_LABELS = {
  linkedin: "shareOnLinkedIn", x: "shareOnX", whatsapp: "shareOnWhatsApp", facebook: "shareOnFacebook", email: "shareByEmail",
} as const;

/**
 * A published site's "사이트 공유" for its visitors, as Notion's (seen
 * 2026-11-06 at 1280×683, signed out): a 500px panel under the share button,
 * its title, a small picture of the site fading out at its foot, a line, the
 * address with a blue "URL 복사", and the services as round buttons.
 */
export function PublicSiteSharePanel({ link, title, titleNamed }: { link: string; title: string; titleNamed?: NamedTitle }) {
  // The picture names the page as the site does, its custom emoji drawn (TitleWithCustomEmoji).
  const shown = <TitleWithCustomEmoji text={title} named={titleNamed} />;
  const { t } = useTranslation("publicSiteView");
  // "URL 복사" keeps its words while a toast says the address was copied.
  const notice = useCopyNotice();
  const field = useRef<HTMLInputElement>(null);
  // The address shows its end, as Notion's field does.
  useEffect(() => { if (field.current) field.current.scrollLeft = field.current.scrollWidth; }, [link]);
  return (
    <div className={styles.panel} role="dialog" aria-label={t("sharePanelTitle")} data-site-menu="share">
      <h2 className={styles.title}>{t("sharePanelTitle")}</h2>
      <div className={styles.preview} aria-hidden="true">
        <div className={styles.frameBar}><span /><span /><span /></div>
        <div className={styles.siteBar}>
          <span>{shown}</span>
          <Search size="micro" />
          <DotsHorizontal size="micro" />
        </div>
        <div className={styles.page}>{shown}</div>
      </div>
      <div className={styles.address}>
        <input ref={field} className={styles.link} value={link} readOnly aria-label={t("siteAddress")}
          onFocus={(event) => event.currentTarget.select()} />
        <button type="button" className={styles.copy} data-site-copy-url
          onClick={() => void notice(link, t("linkCopied"))}>
          <LinkIcon size="small" aria-hidden="true" />{t("copyUrl")}
        </button>
      </div>
      <div className={styles.services}>
        {SITE_VISITOR_SHARE_SERVICES.map((service) => (
          <a key={service} className={styles.service} href={socialShareHref(service, link, title)}
            target={service === "email" ? undefined : "_blank"} rel="noopener noreferrer"
            aria-label={t(SERVICE_LABELS[service])} title={t(SERVICE_LABELS[service])}>
            <ServiceIcon service={service} name={SERVICE_NAMES[service]} size="regular" aria-hidden="true" />
          </a>
        ))}
      </div>
    </div>
  );
}
