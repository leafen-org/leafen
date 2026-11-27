import { useRef, useState, type RefObject } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { formSubmitterAccesses, type FormSubmitterAccess } from "@/formWire.generated";
import { SwitchMark } from "@/components/Switch";
import { AnonymousPersonIcon, CheckIcon, ChevronRight, LinkIcon, LockedDocumentIcon, PersonCircleIcon } from "@/icons/leafen";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { placeAround, popoverPlacementStyle, popoverRectOf, type PopoverRect, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { FormAudience } from "@/lib/types";
import styles from "./FormView.module.css";

// What a signed-in respondent may do on the page their response creates, as
// the share menu names each level.
export const submitterAccessLabels: Record<FormSubmitterAccess, string> = {
  [formSubmitterAccesses.none]: "formView:submitterAccessNone",
  [formSubmitterAccesses.reader]: "formView:submitterAccessReader",
  [formSubmitterAccesses.commentOnly]: "formView:submitterAccessCommentOnly",
  [formSubmitterAccesses.readAndWrite]: "formView:submitterAccessReadAndWrite",
  [formSubmitterAccesses.editor]: "formView:submitterAccessEditor",
};

const AUDIENCES: FormAudience[] = ["workspace", "web", "none"];

type Choice<T extends string> = { value: T; label: string; tag?: { text: string; tone: "public" | "private" }; disabled?: boolean };

/**
 * "양식 공유", as the builder's bar and its "변경" open it, Notion's (seen
 * 2026-11-03): 480 wide under the button, "작성 가능한 사용자 ›" and
 * "제출물 사용 권한 ›" rows naming their choice in grey, each opening its
 * list under it, then the form's link beside "폼 링크 복사하기".
 */
export function FormShareMenu({
  anchor, audience, anonymous, submitterAccess, webDisabled, canCopy, saving, link, workspaceName, status, error,
  onAudience, onAnonymous, onSubmitterAccess, onCopy, onClose,
}: {
  anchor: RefObject<HTMLElement | null>;
  audience: FormAudience;
  anonymous: boolean;
  onAnonymous: (anonymous: boolean) => void;
  submitterAccess: FormSubmitterAccess;
  /** A form asking for people or related pages answers only the workspace's members. */
  webDisabled: boolean;
  canCopy: boolean;
  saving: boolean;
  /** The form's link, once it has one. */
  link: string;
  workspaceName: string;
  status: string;
  error: string;
  onAudience: (audience: FormAudience) => void;
  onSubmitterAccess: (access: FormSubmitterAccess) => void;
  onCopy: () => void;
  onClose: () => void;
}) {
  const { t } = useTranslation("formView");
  const menu = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState<{ list: "audience" | "access"; anchor: PopoverRect } | null>(null);
  const place = usePopoverPlacement(menu, (size, viewport) => placeAround(
    popoverRectOf(anchor.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { gap: 8 }),
  [anchor], undefined, { kind: "menu", onEscape: onClose, onOutsidePress: onClose, inside: () => [anchor.current] });
  const audienceLabel = (value: FormAudience) => value === "workspace"
    ? workspaceName ? t("audienceWorkspaceNamed", { workspace: workspaceName }) : t("audienceWorkspace")
    : t(value === "web" ? "audienceWeb" : "audienceNone");
  const audiences: Choice<FormAudience>[] = AUDIENCES.map((value) => ({
    value, label: audienceLabel(value), disabled: value === "web" && webDisabled,
    tag: value === "web" ? { text: t("audienceWebTag"), tone: "public" } : value === "none" ? { text: t("audienceNoneTag"), tone: "private" } : undefined,
  }));
  const accesses: Choice<FormSubmitterAccess>[] = Object.values(formSubmitterAccesses)
    .map((value) => ({ value, label: t(submitterAccessLabels[value].replace("formView:", "")) }));
  // A held row reads as the others but opens nothing, as Notion's while answers are anonymous.
  const row = (list: "audience" | "access", icon: React.ReactNode, label: string, value: string, disabled = false, held = false) => (
    <button type="button" className={styles.shareRow} aria-haspopup="menu" aria-expanded={open?.list === list} disabled={disabled}
      aria-disabled={held || undefined} data-form-share-row={list}
      onClick={(event) => { if (!held) setOpen(open?.list === list ? null : { list, anchor: popoverRectOf(event.currentTarget)! }); }}>
      {icon}<span>{label}</span><span className={styles.shareRowValue}>{value}</span><ChevronRight size="small" aria-hidden="true" />
    </button>
  );
  return (
    <div ref={menu} className={styles.shareMenu} role="dialog" aria-label={t("shareForm")} style={popoverPlacementStyle(place)}
      data-form-share-menu>
      {row("audience", <PersonCircleIcon size="regular" aria-hidden="true" />, t("whoCanFill"), audienceLabel(audience))}
      {/* A closed form shows who can fill it and its link alone (Notion's, seen 2026-11-05). */}
      {audience === "none" ? null : <>
      <button type="button" role="switch" aria-checked={anonymous} className={styles.shareRow} data-form-share-row="anonymous"
        onClick={() => onAnonymous(!anonymous)}>
        <AnonymousPersonIcon size="regular" aria-hidden="true" /><span>{t("anonymousResponses")}</span><span className={styles.shareRowValue} /><SwitchMark on={anonymous} />
      </button>
      {/* A web form's respondents are unknown, so its pages are shared with no one; an anonymous
          form's read "사용 권한 없음" while it is on and keep their choice for when it is off (Notion's, seen 2026-11-05). */}
      {row("access", <LockedDocumentIcon size="regular" aria-hidden="true" />, t("submitterAccess"),
        t(submitterAccessLabels[anonymous ? formSubmitterAccesses.none : submitterAccess].replace("formView:", "")),
        audience === "web", anonymous)}
      </>}
      <div className={styles.shareLink}>
        <input readOnly value={link} aria-label={t("formLink")} onFocus={(event) => event.currentTarget.select()} data-form-link />
        <button type="button" disabled={!canCopy || saving || audience === "none"} onClick={onCopy} data-form-copy-link>
          <LinkIcon size="regular" aria-hidden="true" />{t("copyLink")}
        </button>
      </div>
      {webDisabled && audience === "web" ? <p className={styles.error} role="alert">{t("referenceAudienceRequired")}</p> : null}
      {status ? <p className={styles.success} role="status">{status}</p> : null}
      {error ? <p className={styles.error} role="alert">{error}</p> : null}
      {open?.list === "audience" ? (
        <ShareChoices anchor={open.anchor} label={t("whoCanFill")} choices={audiences} current={audience}
          onChoose={(value) => { setOpen(null); onAudience(value); }} onClose={() => setOpen(null)} />
      ) : open?.list === "access" ? (
        <ShareChoices anchor={open.anchor} label={t("submitterAccess")} choices={accesses} current={submitterAccess}
          onChoose={(value) => { setOpen(null); onSubmitterAccess(value); }} onClose={() => setOpen(null)} />
      ) : null}
    </div>
  );
}

/** A row's list, 300 wide under it at its right end; the current one checked. */
function ShareChoices<T extends string>({ anchor, label, choices, current, onChoose, onClose }: {
  anchor: PopoverRect; label: string; choices: Choice<T>[]; current: T; onChoose(value: T): void; onClose(): void;
}) {
  const list = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(list, (size, viewport) => placeAround(anchor, size, viewport, { gap: 0, align: "end" }),
    [anchor.left, anchor.bottom], undefined, { kind: "menu", restoreFocus: false, onEscape: onClose, onOutsidePress: onClose });
  const picker = useNativeMenu({ items: choices, open: true, menuRef: list, label, keyOf: (choice) => choice.value,
    textOf: (choice) => choice.label, firstKey: current, disabledKeys: choices.filter((choice) => choice.disabled).map((choice) => choice.value),
    onAction: (choice) => onChoose(choice.value) });
  return createPortal(
    <div {...picker.menuProps} ref={list} className={styles.shareChoices} style={popoverPlacementStyle(place)}>
      {choices.map((choice) => (
        <NativeMenuItem key={choice.value} state={picker.state} itemKey={choice.value} data-form-share-choice={choice.value}>
          {({ labelProps }) => <>
            <span {...labelProps}>{choice.label}</span>
            {choice.tag ? <span className={styles.shareTag} data-tone={choice.tag.tone}>{choice.tag.text}</span> : null}
            {choice.value === current ? <CheckIcon size="small" className={styles.shareCheck} aria-hidden="true" /> : null}
          </>}
        </NativeMenuItem>
      ))}
    </div>,
    document.body,
  );
}
