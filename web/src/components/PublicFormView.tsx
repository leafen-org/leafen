import { CheckIcon, FormBlockIcon, FormSubmittedIcon } from "@/icons/leafen";
"use client";

import { newId } from "@/lib/ids";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { buildFormSubmissionAnswers, formConfirmationShown } from "@/lib/formViewProjection";
import {
  publicFormErrorStatus,
  publicFormFailureMessage,
  publicFormFailureReference,
  type FormTransportAuthority,
  type PublicFormService,
} from "@/lib/publicFormView";
import type {
  FormDefinitionResult,
  FormReferenceOption,
  FormSubmitResult,
} from "@/lib/types";

import {
  FormCover,
  FormHeader,
  FormQuestionFields,
  FormRespondent,
  FormSubmitButton,
  invalidAnswers,
  unansweredRequiredQuestions,
} from "./forms/FormSurface";
import { pageHref } from "@/lib/navigation";
import { phoneScreen } from "@/lib/screenKind";
import { showsWhole } from "@/lib/openerScroll";
import { onWindowReturn } from "@/lib/windowReturn";
import type { FormSurfaceBounds } from "./forms/formSurfaceBudget";
import type { FormReferenceTarget } from "./forms/FormReferencePicker";
import styles from "./PublicFormView.module.css";

// "closed": a form shown here that stopped taking answers, as Notion's says
// once it is closed or deleted (seen 2026-11-03).
type LoadFailure = "signin" | "unavailable" | "closed" | "error";

const CLOSED_CHECK_MS = 20_000;

function requestId() {
  return newId();
}

export function PublicFormView({
  bounds,
  service,
  token,
  inApp = false,
  onDefinition,
  onSignIn,
}: {
  bounds: FormSurfaceBounds;
  service: PublicFormService;
  token: string;
  /** A member's form as a page of the app (MemberFormPage), as Notion's. */
  inApp?: boolean;
  /** The form as read, for its page's bar. */
  onDefinition?: (definition: FormDefinitionResult) => void;
  /**
   * A members' form opened signed out goes to the sign-in screen at its
   * link, which names its workspace and comes back to it, as Notion's form
   * link (seen 2026-11-15 signed out in Chromium and WebKit: no form, no
   * refusal, "거의 다 됐습니다!" and the sign-in).
   */
  onSignIn?: () => void;
}) {
  const { t } = useTranslation(["formView", "common"]);
  const onDefinitionRef = useRef(onDefinition);
  onDefinitionRef.current = onDefinition;
  const onSignInRef = useRef(onSignIn);
  onSignInRef.current = onSignIn;
  // The form is read again for a new link, not for each new service its page
  // hands over (2026-11-04: the builder's preview read it 8 times in 17
  // seconds, each read letting its relation's list go).
  const serviceRef = useRef(service);
  serviceRef.current = service;
  const [definition, setDefinition] = useState<FormDefinitionResult | null>(null);
  const [loadFailure, setLoadFailure] = useState<LoadFailure | null>(null);
  const [loadErrorMessage, setLoadErrorMessage] = useState("");
  const [values, setValues] = useState<Record<string, unknown>>({});
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState("");
  // Required questions left unanswered at the last "보내기"; answering one clears it.
  const [missing, setMissing] = useState<string[]>([]);
  // After a "보내기", answers not of their kind say so until they are (Notion's, seen 2026-11-04).
  const [checked, setChecked] = useState(false);
  const [confirmation, setConfirmation] = useState<FormSubmitResult["confirmation"] | null>(null);
  // A member's response opens as its row's page from "내 응답 보기", as Notion's.
  const [submittedRowId, setSubmittedRowId] = useState("");
  // "이메일로 사본 받기" names the response by the request that sent it.
  const [submittedRequestId, setSubmittedRequestId] = useState("");
  const [copyState, setCopyState] = useState<"idle" | "sending" | "sent" | "failed">("idle");
  const [referenceOptions, setReferenceOptions] = useState<Record<string, FormReferenceOption[] | undefined>>({});
  const [referenceAfter, setReferenceAfter] = useState<Record<string, string | null | undefined>>({});
  const [referenceHasMore, setReferenceHasMore] = useState<Record<string, boolean>>({});
  const [referenceLoading, setReferenceLoading] = useState<Record<string, boolean>>({});
  const [referenceErrors, setReferenceErrors] = useState<Record<string, string | undefined>>({});
  const [referenceTaskDatabase, setReferenceTaskDatabase] = useState<Record<string, boolean>>({});
  const [referenceTarget, setReferenceTarget] = useState<Record<string, FormReferenceTarget>>({});
  const requestIdRef = useRef("");
  const transportAuthorityRef = useRef<FormTransportAuthority>("public");
  const optionFlightsRef = useRef(new Map<string, Promise<void>>());
  const optionGenerationRef = useRef(0);
  // Each question's list follows what was last typed in its search; a list
  // asked for before it is let go.
  const optionQueriesRef = useRef(new Map<string, { query: string; turn: number }>());

  const resetReferenceOptions = useCallback(() => {
    optionGenerationRef.current += 1;
    optionFlightsRef.current.clear();
    optionQueriesRef.current.clear();
    setReferenceOptions({});
    setReferenceAfter({});
    setReferenceHasMore({});
    setReferenceLoading({});
    setReferenceErrors({});
  }, []);

  const load = useCallback(async (
    authority: FormTransportAuthority = "public",
    allowAuthRetry = true,
  ) => {
    setLoadFailure(null);
    setLoadErrorMessage("");
    try {
      const result = await serviceRef.current.getDefinition(token, authority);
      // A form open to the web is answered as the web answers it, whoever reads it.
      transportAuthorityRef.current = result.audience === "web" ? "public" : authority;
      setDefinition(result);
      onDefinitionRef.current?.(result);
      resetReferenceOptions();
      return result;
    } catch (error) {
      const status = publicFormErrorStatus(error);
      if (status === 401 && authority === "public" && allowAuthRetry) {
        let userId = "";
        try {
          userId = await serviceRef.current.restoreAuthSession();
        } catch (restoreError) {
          publicFormFailureReference(restoreError, "auth.restore");
        }
        if (userId) return load("member", false);
      }
      setDefinition(null);
      const failure = status === 401
        ? "signin"
        : status === 404 || status === 403 || status === 410
          ? "unavailable"
          : "error";
      setLoadFailure(failure);
      if (failure === "signin") onSignInRef.current?.();
      if (failure === "error") {
        setLoadErrorMessage(publicFormFailureMessage(
          error,
          "definition.read",
        ));
      }
      return null;
    }
  }, [resetReferenceOptions, t, token]);

  const loadReferenceOptions = useCallback((propertyId: string, append: boolean, query = "", pageFilter?: "all" | "verified" | "archived") => {
    const searched = optionQueriesRef.current.get(propertyId);
    if (append && optionFlightsRef.current.has(propertyId)) return;
    const after = append ? referenceAfter[propertyId] ?? undefined : undefined;
    if (append && !after) return;
    if (!definition) return;
    const turn = append ? searched?.turn ?? 0 : (searched?.turn ?? 0) + 1;
    const typed = append ? searched?.query ?? "" : query.trim();
    optionQueriesRef.current.set(propertyId, { query: typed, turn });
    const generation = optionGenerationRef.current;
    const current = () => generation === optionGenerationRef.current && optionQueriesRef.current.get(propertyId)?.turn === turn;
    setReferenceLoading((loading) => ({ ...loading, [propertyId]: true }));
    setReferenceErrors((errors) => ({ ...errors, [propertyId]: undefined }));
    const flight = serviceRef.current.getOptions({
      token,
      propertyId,
      revision: definition.revision,
      after,
      query: typed || undefined,
      pageFilter,
    })
      .then((result) => {
        if (!current()) return;
        setReferenceTaskDatabase((known) => ({ ...known, [propertyId]: result.taskDatabase === true }));
        setReferenceTarget((known) => ({ ...known, [propertyId]: { title: result.targetTitle ?? "", icon: result.targetIcon, iconType: result.targetIconType } }));
        setReferenceOptions((current) => {
          const combined = append
            ? [...(current[propertyId] ?? []), ...result.options]
            : result.options;
          return {
            ...current,
            [propertyId]: Array.from(
              new Map(combined.map((option) => [option.id, option])).values(),
            ),
          };
        });
        setReferenceAfter((current) => ({ ...current, [propertyId]: result.after }));
        setReferenceHasMore((current) => ({ ...current, [propertyId]: result.hasMore }));
      })
      .catch((error) => {
        if (!current()) return;
        setReferenceErrors((current) => ({
          ...current,
          [propertyId]: publicFormFailureMessage(
            error,
            "options.read",
          ),
        }));
      })
      .finally(() => {
        if (!current()) return;
        if (optionFlightsRef.current.get(propertyId) === flight) {
          optionFlightsRef.current.delete(propertyId);
        }
        setReferenceLoading((current) => ({ ...current, [propertyId]: false }));
      });
    optionFlightsRef.current.set(propertyId, flight);
  }, [definition, referenceAfter, t, token]);

  useEffect(() => {
    let active = true;
    // Someone signed in reads a form in the app as themselves from the first
    // read, a members' one with no refused read before it (2026-11-09: each
    // opening logged a 401, then read it again); a visitor reads it as the web.
    void load(inApp && serviceRef.current.respondent() ? "member" : "public").then((result) => {
      if (!active || result) return;
    });
    return () => {
      active = false;
    };
  }, [load]);

  // A form closed while it is open turns to the screen of a form that is
  // private or deleted, as Notion's (seen 2026-11-03: at once), here when the window comes back,
  // every twenty seconds while it shows, or when an answer can't go.
  const shown = Boolean(definition) && !confirmation;
  useEffect(() => {
    if (!shown) return;
    const check = () => {
      if (document.visibilityState !== "visible") return;
      void serviceRef.current.getDefinition(token, transportAuthorityRef.current).catch((error) => {
        const status = publicFormErrorStatus(error);
        if (status === 404 || status === 403 || status === 410) { setDefinition(null); setLoadFailure("closed"); }
      });
    };
    const timer = window.setInterval(check, CLOSED_CHECK_MS);
    const stopReturn = onWindowReturn(check);
    return () => { window.clearInterval(timer); stopReturn(); };
  }, [shown, token]);

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!definition || submitting) return;
    const unanswered = unansweredRequiredQuestions(definition.form, definition.properties, values);
    const problems = invalidAnswers(definition.form, definition.properties, values);
    setMissing(unanswered);
    setChecked(true);
    const first = definition.form.questions.find(({ propertyId }) => unanswered.includes(propertyId) || problems[propertyId]);
    // The first such question comes into view only when it isn't in it, as
    // Notion's (seen 2026-11-08: a shown one stays put).
    if (first) {
      const shown = event.currentTarget.querySelector(`[data-form-question="${CSS.escape(first.propertyId)}"]`);
      if (shown && !showsWhole(shown)) shown.scrollIntoView({ block: "start", behavior: "smooth" });
      return;
    }
    setSubmitting(true);
    setSubmitError("");
    if (!requestIdRef.current) requestIdRef.current = requestId();
    try {
      const result = await service.submit({
        token,
        revision: definition.revision,
        requestId: requestIdRef.current,
        answers: buildFormSubmissionAnswers(definition.form, definition.properties, values),
      }, transportAuthorityRef.current);
      setConfirmation(result.confirmation);
      setSubmittedRowId(transportAuthorityRef.current === "member" ? result.rowId : "");
      setSubmittedRequestId(transportAuthorityRef.current === "member" && definition.responseCopy ? requestIdRef.current : "");
      setCopyState("idle");
      requestIdRef.current = "";
    } catch (error) {
      const status = publicFormErrorStatus(error);
      if (status === 409) {
        await load(transportAuthorityRef.current, false);
        setSubmitError(t("formView:formChanged"));
      } else if (status === 404 || status === 403 || status === 410) {
        setDefinition(null);
        setLoadFailure("closed");
      } else {
        setSubmitError(publicFormFailureMessage(
          error,
          "response.submit",
        ));
      }
    } finally {
      setSubmitting(false);
    }
  }

  async function copyResponse() {
    if (!submittedRequestId || copyState === "sending") return;
    setCopyState("sending");
    try {
      setCopyState(await service.copyResponse({ token, requestId: submittedRequestId }) ? "sent" : "failed");
    } catch (error) {
      publicFormFailureReference(error, "response.copy");
      setCopyState("failed");
    }
  }

  const respondent = definition && transportAuthorityRef.current === "member" ? service.respondent() : null;

  // In the app a form, its end and its absence read as Notion's (seen
  // 2026-11-03): a 600 column on the page's white, its ends in the middle
  // under a grey picture.
  const place = inApp ? "app" : undefined;
  if ((!definition && !loadFailure) || (loadFailure === "signin" && onSignIn)) {
    return <main className={styles.page} data-place={place}><div className={styles.state} aria-busy="true">{t("formView:loading")}</div></main>;
  }

  if (!definition) {
    // Signed out with nowhere to sign in, a members' form reads as one not
    // open to the public: its picture, the line saying the form isn't public
    // and its words in the middle.
    const centered = inApp || loadFailure === "signin";
    return (
      <main className={styles.page} data-place={centered ? "app" : place} data-centered={centered || undefined}>
        <div className={styles.state} role="status" data-form-unavailable={loadFailure}>
          {centered ? <FormBlockIcon size="card" className={styles.stateMark} aria-hidden="true" /> : null}
          {/* A form the server didn't answer for is not a form closed to the reader. */}
          <h1>{loadFailure === "closed" ? t("formView:closedFormTitle") : loadFailure === "error" ? t("formView:loadFailedTitle") : t("formView:unavailableTitle")}</h1>
          <p>{loadFailure === "closed" ? t("formView:closedFormBody") : loadErrorMessage || t("formView:unavailableBody")}</p>
          {loadFailure === "error" && (
            <button type="button" onClick={() => void load()}>{t("common:retry")}</button>
          )}
        </div>
      </main>
    );
  }

  // "다른 응답 제출하기" starts the form afresh.
  function submitAnother() {
    setConfirmation(null);
    setValues({});
    setMissing([]);
    setChecked(false);
    setSubmitError("");
    setSubmittedRowId("");
    setSubmittedRequestId("");
    setCopyState("idle");
  }

  if (confirmation) {
    const thanks = formConfirmationShown(confirmation);
    return (
      <main className={styles.page} data-place={place} data-centered={inApp || undefined}>
        <section className={styles.confirmation} role="status" data-form-confirmation>
          {inApp ? <FormSubmittedIcon size="card" className={styles.stateMark} aria-hidden="true" />
            : <div className={styles.confirmationMark} aria-hidden="true"><CheckIcon size="large" /></div>}
          <h1>{thanks.confirmationTitle}</h1>
          {thanks.confirmationBody && <p>{thanks.confirmationBody}</p>}
          <div className={styles.confirmationActions}>
          {/* Its buttons are those the form's "제출 화면" keeps on, as Notion's (seen 2026-11-04). */}
          {submittedRowId && confirmation.showViewResponse !== false ? <a className={styles.confirmationAction} href={pageHref(submittedRowId)} data-form-view-response>
            {t("formView:viewResponse")}</a> : null}
          {submittedRequestId && confirmation.showResponseCopy !== false ? (
            <button type="button" className={styles.confirmationAction} data-form-response-copy
              disabled={copyState === "sending" || copyState === "sent"} onClick={() => void copyResponse()}>
              {/* A phone's says it shorter, "사본 보내기" (Notion's, seen 2026-11-08 at 375 and 2026-11-15 at 411). */}
              {t(copyState === "sent" ? "formView:responseCopySent" : phoneScreen() ? "formView:responseCopyPhone" : "formView:responseCopy")}
            </button>
          ) : null}
          {confirmation.showSubmitAnother !== false ? (
            <button type="button" className={styles.confirmationAction} data-form-submit-another onClick={submitAnother}>
              {t("formView:submitAnother")}</button>
          ) : null}
          </div>
          {copyState === "failed" ? <p className={styles.error} role="alert">{t("formView:responseCopyFailed")}</p> : null}
        </section>
      </main>
    );
  }

  return (
    <main className={styles.page} data-public-form data-place={place}>
      <FormCover form={definition.form} />
      <form className={styles.form} noValidate onSubmit={(event) => void submit(event)}>
        <FormHeader form={definition.form} bounds={bounds} />
        {respondent ? <FormRespondent respondent={respondent} anonymous={definition.form.anonymous === true} /> : null}
        <FormQuestionFields
          form={definition.form}
          bounds={bounds}
          properties={definition.properties}
          values={values}
          disabled={submitting}
          referenceOptions={referenceOptions}
          referenceLoading={referenceLoading}
          referenceErrors={referenceErrors}
          referenceHasMore={referenceHasMore}
          referenceTaskDatabase={referenceTaskDatabase}
          onRequestReferenceOptions={loadReferenceOptions}
          referenceTarget={referenceTarget}
          respondentId={respondent?.id}
          uploadFile={service.uploadFile ? (propertyId, file, upload) => service.uploadFile!(
            { token, propertyId, requestId: upload, file }, transportAuthorityRef.current) : undefined}
          searchPlaces={service.searchPlaces ? (propertyId, query, signal) => service.searchPlaces!(
            { token, propertyId, query }, transportAuthorityRef.current, signal) : undefined}
          missing={missing}
          invalid={checked ? invalidAnswers(definition.form, definition.properties, values) : undefined}
          onChange={(propertyId, value) => {
            setValues((current) => ({ ...current, [propertyId]: value }));
            setMissing((current) => current.includes(propertyId) ? current.filter((id) => id !== propertyId) : current);
          }}
        />
        {submitError && <p className={styles.error} role="alert">{submitError}</p>}
        <FormSubmitButton
          form={definition.form}
          bounds={bounds}
          disabled={submitting}
          label={submitting ? t("formView:submitting") : undefined}
        />
      </form>
    </main>
  );
}

/** A closed form's preview, which says it takes no answers, as Notion's preview of a form no one can fill (seen 2026-11-05). */
export function FormUnavailableNotice() {
  const { t } = useTranslation(["formView", "common"]);
  return (
    <main className={styles.page} data-place="app" data-centered>
      <div className={styles.state} role="status" data-form-unavailable="none">
        <FormBlockIcon size="card" className={styles.stateMark} aria-hidden="true" />
        <h1>{t("formView:unavailableTitle")}</h1>
        <p>{t("formView:unavailableBody")}</p>
      </div>
    </main>
  );
}
