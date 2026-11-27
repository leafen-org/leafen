import type { Ref } from "react";
import { useTranslation } from "react-i18next";
import { LockIcon } from "@/icons/leafen";
import type { GraphView } from "@/contentGraph";
import { formSubmitterAccesses, type FormSubmitterAccess } from "@/formWire.generated";
import type { FormAudience } from "@/lib/types";
import { submitterAccessLabels } from "./FormShareMenu";
import styles from "./FormView.module.css";

// A form never set up is open to the workspace's members, as Notion's new
// form is (seen 2026-11-02); one that was closed stays closed.
export function audienceForView(view: GraphView): FormAudience {
  const audience = view.config.leafenFormAudience;
  if (audience === "workspace" || audience === "web") return audience;
  return formIsNew(view) ? "workspace" : "none";
}

export function formIsNew(view: GraphView) {
  return !view.config.leafenForm && view.config.leafenFormAudience === undefined;
}

export function submitterAccessForView(view: GraphView): FormSubmitterAccess {
  const access = view.config.leafenFormSubmitterAccess;
  return typeof access === "string" && access in submitterAccessLabels
    ? access as FormSubmitterAccess
    : formSubmitterAccesses.none;
}

/**
 * The builder's line under the description saying who may answer, with its
 * "변경" opening "양식 공유" (FormShareMenu) there.
 */
export function FormAudienceNotice({ audience, workspaceName, buttonRef, onChange }: {
  audience: FormAudience;
  workspaceName: string;
  buttonRef: Ref<HTMLButtonElement>;
  onChange(): void;
}) {
  const { t } = useTranslation("formView");
  const notice = audience === "web" ? t("noticeWeb")
    : audience === "workspace" ? (workspaceName ? t("noticeWorkspaceNamed", { workspace: workspaceName }) : t("noticeWorkspace"))
    : t("noticeClosed");
  return (
    <div className={styles.audienceNotice} data-form-audience-notice={audience}>
      <LockIcon size="small" aria-hidden="true" />
      <span>{notice}</span>
      <button ref={buttonRef} type="button" onClick={onChange}>
        {t("change")}
      </button>
    </div>
  );
}
