"use client";

import { useTranslation } from "react-i18next";
import type { NativeEditorGraphAdapter } from "@/components/editor/editorRuntimeTypes";
import { AutomationBlockDraftEditor } from "./AutomationBlockDraftEditor";
import { AutomationEditPagesFields } from "./AutomationEditPagesFields";
import type { AutomationAction, DbProperty, Page } from "@/lib/types";
import type { UsableApp } from "@/appPlatformWire.generated";
import { AutomationAppActionFields } from "./AutomationAppActionFields";
import { AutomationMessageField } from "./AutomationMessageField";
import { AutomationRecipientsField } from "./AutomationRecipientsField";
import { AutomationVariablesFields } from "./AutomationVariablesFields";
import { AutomationWebhookFields } from "./AutomationWebhookFields";
import type { useAutomationTargetDatabases } from "./automationTargetDatabases";
import { automationActionProblem, type AutomationEditorSurface } from "./automationActionModel";
import { AutomationAddPageFields } from "./AutomationAddPageFields";
import { AutomationPageIconField } from "./AutomationPageIconField";
import { automationCheckedNames, automationFormulaFault } from "./automationFormulaFault";
import type { AutomationVariableList } from "./automationFormulaNames";
import { WarningFillIcon } from "@/icons/leafen/automationIcons";
import type { AutomationRecipientKind } from "./AutomationRecipientKind";
import editStyles from "./AutomationEditProperty.module.css";
import styles from "./automationActionEditor.module.css";
import messageStyles from "./automationMessageField.module.css";

export function AutomationActionFields({
  action,
  index,
  surface,
  properties,
  propertyLabels,
  pages,
  databases,
  apps = [],
  targets,
  saved = false,
  draftContext,
  draftOwnerId,
  variables = [],
  variableNames = [],
  recipientKind = null,
  onChange,
}: {
  action: AutomationAction;
  index: number;
  surface: AutomationEditorSurface;
  properties: DbProperty[];
  propertyLabels: string[];
  pages: Page[];
  databases: Page[];
  apps?: UsableApp[];
  /** The databases on other pages actions name, and the search to choose one. */
  targets?: ReturnType<typeof useAutomationTargetDatabases>;
  /** Saved before the editor opened, so an unfinished choice warns. */
  saved?: boolean;
  draftContext?: NativeEditorGraphAdapter;
  draftOwnerId?: string;
  /** The variables the actions before this one define, which its words may name. */
  variables?: AutomationVariableList;
  /** Every variable name the automation's actions define. */
  variableNames?: readonly string[];
  /** Whom a notification goes to, as its card's chip says; none until chosen. */
  recipientKind?: AutomationRecipientKind | null;
  onChange: (action: AutomationAction) => void;
}) {
  const { t } = useTranslation(["databaseView"]);
  const number = index + 1;

  if (action.type === "run_app_action") {
    return <AutomationAppActionFields action={action} apps={apps} number={number} onChange={onChange} />;
  }

  if (action.type === "insert_blocks") {
    return <>
      <AutomationBlockDraftEditor key={action.id} action={action} context={draftContext} ownerId={draftOwnerId} onChange={onChange} />
    </>;
  }

  // Notion's card has no choice to open the page it makes: its notice's
  // "열기" does (seen 2026-11-04).
  if (action.type === "add_page") {
    return <AutomationAddPageFields action={action} number={number} surface={surface} properties={properties} pages={pages}
      targets={targets} variables={variables} onChange={onChange} />;
  }

  if (action.type === "edit_pages") {
    return <AutomationEditPagesFields action={action} number={number} surface={surface} properties={properties} pages={pages}
      databases={databases} targets={targets} context={draftContext} variables={variables} onChange={onChange} />;
  }

  if (action.type === "set_page_icon") return <AutomationPageIconField action={action} onChange={onChange} />;

  if (action.type === "define_variables") {
    return <AutomationVariablesFields action={action} number={number} surface={surface} properties={properties} pages={pages}
      variables={variables} variableNames={variableNames} onChange={onChange} />;
  }

  if (action.type === "send_notification") {
    // Nothing until the chip names whom it goes to, as Notion's (seen 2026-10-24); once asked for, the chip says so (2026-11-08).
    if (!recipientKind) return null;
    // Once asked for, only its first problem is red, as Notion's (seen 2026-11-21 in a page's button four times in two
    // tabs, a database's button and its automation): nobody to notify turns the people's name and box red and leaves
    // the message's name white; with people picked an empty message's name goes red with what to do under its box.
    // A new one starts grey (seen 2026-11-21 in two tabs).
    const problem = saved ? automationActionProblem(action) : null;
    const warn = Boolean(problem?.endsWith(".recipients"));
    const noMessage = Boolean(problem?.endsWith(".message"));
    // A message's formula its editor names a problem in turns the name red with a warning (Notion's, seen 2026-11-21).
    const faulty = Boolean(action.messageFormula && automationFormulaFault(action.messageFormula, properties, automationCheckedNames(surface, variables, properties)));
    return (
      <>
        <p className={editStyles.fieldName} data-warning={warn ? "" : undefined}>
          {t(recipientKind === "property" ? "databaseView:recipientPropertyLabel" : "databaseView:recipientPeopleLabel")}</p>
        <AutomationRecipientsField action={action} number={number} surface={surface} properties={properties} kind={recipientKind}
          marked={warn} onChange={onChange} />
        <p className={faulty ? `${editStyles.fieldName} ${messageStyles.faultName}` : editStyles.fieldName} data-warning={noMessage || faulty ? "" : undefined}>
          {faulty ? <WarningFillIcon size="small" aria-hidden="true" /> : null}{t("databaseView:actionMessageLabel")}</p>
        <AutomationMessageField action={action} label={t("databaseView:messageForAction", { number })} properties={properties}
          surface={surface} variables={variables} pages={pages} onChange={(next) => onChange(next)} />
        {noMessage ? <p className={styles.actionWarning} data-automation-warning="message">{t("databaseView:notifyMessageMissing")}</p> : null}
      </>
    );
  }

  if (action.type === "send_email") {
    return (
      <>
        <input aria-label={t("databaseView:emailForAction", { number })} value={action.recipientEmail} onChange={(event) => onChange({ ...action, recipientEmail: event.target.value })} />
        <input aria-label={t("databaseView:subjectForAction", { number })} value={action.subject} onChange={(event) => onChange({ ...action, subject: event.target.value })} />
        <AutomationMessageField action={action} label={t("databaseView:messageForAction", { number })} properties={properties}
          surface={surface} variables={variables} pages={pages} onChange={(next) => onChange(next)} />
      </>
    );
  }

  if (action.type === "send_webhook") {
    return <AutomationWebhookFields action={action} number={number} surface={surface} properties={properties}
      propertyLabels={propertyLabels} saved={saved} onChange={onChange} />;
  }

  if (action.type === "send_slack") {
    // Its channel is chosen in its head (AutomationSlackChannelChip).
    return (
      <>
        <AutomationMessageField action={action} label={t("databaseView:messageForAction", { number })} properties={properties}
          surface={surface} variables={variables} pages={pages} onChange={(next) => onChange(next)} />
      </>
    );
  }

  if (action.type === "show_confirmation") {
    return (
      <>
        <div className={`${styles.messageFieldLabel} ${messageStyles.confirmationMessage}`}>
          <span>{t("databaseView:confirmationMessage")}</span>
          <AutomationMessageField action={action} label={t("databaseView:messageForAction", { number })} properties={properties}
            surface={surface} variables={variables} pages={pages} onChange={(next) => onChange(next)} />
        </div>
        <label className={styles.fieldLabel}>{t("databaseView:confirmationContinue")}<input aria-label={t("databaseView:confirmLabelForAction", { number })} value={action.confirmLabel} onChange={(event) => onChange({ ...action, confirmLabel: event.target.value })} /></label>
        <label className={styles.fieldLabel}>{t("databaseView:confirmationCancel")}<input aria-label={t("databaseView:cancelLabelForAction", { number })} value={action.cancelLabel} onChange={(event) => onChange({ ...action, cancelLabel: event.target.value })} /></label>
      </>
    );
  }

  // An open action's choice, and an edit-property action's value, are on its card (AutomationOpenTargetPicker, AutomationEditProperty).
  return null;
}
