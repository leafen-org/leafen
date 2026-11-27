import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { PublicFormView } from "@/components/PublicFormView";
import { TopBar } from "@/components/page/TopBar";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { styles as topBarStyles } from "@/components/page/topBarStyles";
import { useProductPages } from "@/components/product/productPagesContext";
import { CopyTipButton } from "@/components/CopyTipButton";
import { IconTipButton } from "@/components/IconTipButton";
import type { FormDefinitionResult } from "@/lib/types";
import { LinkIcon, Pencil, Star, StarFilled } from "@/icons/leafen";
import { useFormFavorites } from "@/lib/formFavorites";
import { formEditHref, formHref } from "@/lib/formViewModel";
import { createNativeFormService, nativeFormSurfaceBounds } from "@/lib/nativeFormService";
import { resetDocumentChrome } from "@/lib/documentChrome";
import { pushRoute } from "@/lib/router";
import { serverUrl } from "@/lib/serverOrigin";
import styles from "./MemberFormPage.module.css";

/**
 * A form's link opened by someone signed in: a page of the app, beside the
 * sidebar under a bar with no path, as Notion opens a member's form (seen
 * 2026-11-03) — its title alone at the bar's start (seen 2026-11-05);
 * signed out, the same link opens the form on its own (App).
 * The bar holds "양식 편집", which goes to its builder, "양식 링크 복사" and
 * 즐겨찾기, the form itself atop the favorites (Notion's, seen 2026-11-05).
 * On a phone, where no form is edited, the bar's end holds the title, the
 * link and the favorite, no "양식 편집" (MemberFormPage.module.css).
 */
export default function MemberFormPage({ token }: { token: string }) {
  const { t } = useTranslation("formView");
  const pages = useProductPages();
  const session = pages?.session ?? null;
  const service = useMemo(() => createNativeFormService(api, () => session), [session]);
  const [place, setPlace] = useState<FormDefinitionResult["place"] | null>(null);
  const [title, setTitle] = useState("");
  // The window and its tab say "Leafen" for the form, as before its own name.
  useEffect(() => { resetDocumentChrome(); }, []);
  const { favorites, setFavorite } = useFormFavorites(session, pages?.workspaceId);
  const favorite = favorites.some((item) => item.token === token);
  const { phone } = useSidebarLayout();
  const actions = (
    <>
      {place?.canEdit && !phone ? (
        <IconTipButton className={topBarStyles.iconAction} label={t("editForm")} placement="bottom" data-member-form-edit
          onClick={() => pushRoute(formEditHref(place))}>
          <Pencil size="regular" aria-hidden="true" />
        </IconTipButton>
      ) : null}
      {/* Each named in a dark tip under it, the link's saying "복사됨" once copied, no toast (Notion's, seen 2026-11-15). */}
      <CopyTipButton className={topBarStyles.iconAction} label={t("copyFormLink")} placement="bottom" data-member-form-link
        text={() => serverUrl(formHref(token))} copied={t("linkCopiedTip")} failed={t("copyLinkFailed")}>
        <LinkIcon size="regular" aria-hidden="true" />
      </CopyTipButton>
      <IconTipButton className={topBarStyles.iconAction} label={t("favorite")} placement="bottom" aria-pressed={favorite} data-favorite={favorite ? "true" : undefined}
        data-member-form-favorite onClick={() => void setFavorite(token, !favorite)}>
        {favorite ? <StarFilled size="regular" aria-hidden="true" /> : <Star size="regular" aria-hidden="true" />}
      </IconTipButton>
    </>
  );
  return (
    <>
      <TopBar title={title} crumbs={phone ? <span className={styles.phoneTitle}>{title}</span> : title ? undefined : <></>}
        screenActions={phone ? <span className={styles.phoneRail} data-member-form-phone-bar>{actions}</span>
          : <span className={styles.rail} data-member-form-bar>{actions}</span>} />
      <PublicFormView bounds={nativeFormSurfaceBounds} service={service} token={token} inApp onDefinition={(definition) => {
        setPlace(definition.place ?? null);
        setTitle(definition.form.title?.trim() ?? "");
      }} />
    </>
  );
}
