import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { RemoteAddressChoice } from "@/components/remoteAccess/RemoteAddressChoice";
import { addressReach, outsideAddress } from "@/lib/addressReach";
import { finishFirstRun, firstRunStepsLeft, firstRunStepsLeftNow, followFirstRun, passFirstRunStep, type FirstRunStep } from "@/lib/firstRunSteps";
import { holdPhoneOffer } from "@/lib/phoneOffer";
import { fetchRuntimeConfig } from "@/lib/runtimeConfigService";
import { linkOrigin } from "@/lib/serverOrigin";
import { useAfterTyping } from "@/lib/typingHold";
import type { Session } from "@/types";
import { FirstRunScreen } from "../firstInstall/FirstRunScreen";
import remoteStyles from "../remoteAccess/RemoteAddressChoice.module.css";
import { AgentConnectStep } from "./AgentConnectStep";

// How often the steps look for one an agent's command did or left for later.
const FOLLOW_EVERY = 2000;

/**
 * The first run's steps after the name and the language, one flow in the app
 * and in a browser (the one-program decision of 2026-10-27, 5): the outside
 * address — passed where the server has one — then how outside agents
 * connect, each a full-window screen whose "나중에" is a small one that says
 * what skipping costs; the workspace then opens on its welcome page, with no
 * other device asked for (2026-10-29, as Notion's sign-up). Which are left is
 * the server's (lib/firstRunSteps.ts); a step an agent's `leafen setup` did
 * or left for later passes here too, within seconds (user decision
 * 2026-11-09). Settings brings back what was left for later. Whatever a step
 * offers is done in its own screen (2026-10-27: nothing leaves for
 * Settings), as a domain in the address step.
 */
export function FirstRunSteps({ session, workspaceId, blocked, onFinished }: {
  session: Session;
  /** The workspace, once its list is read; the address step doesn't wait for it. */
  workspaceId: string | null;
  /** Another top-level window (Settings) is open: the steps wait behind it. */
  blocked: boolean;
  /** The steps are over. */
  onFinished(): void;
}) {
  const { t } = useTranslation("serverConsole");
  const userId = session.user.id;
  const [steps, setSteps] = useState<FirstRunStep[] | null>(() => firstRunStepsLeftNow(userId));
  const [addressChecked, setAddressChecked] = useState(false);

  const finished = useRef(false);
  const finish = async () => {
    if (finished.current) return;
    finished.current = true;
    // An address only this computer opens offers no phone: once an outside
    // one comes up, it does, once (PhoneOfferLater).
    if (addressReach(linkOrigin()) === "loopback") holdPhoneOffer(userId);
    await finishFirstRun(session).catch(() => undefined);
    onFinished();
  };

  useEffect(() => {
    let current = true;
    void firstRunStepsLeft(session).then((left) => {
      if (!current) return;
      setSteps(left);
      if (left.length === 0) onFinished();
    }, () => { if (current) onFinished(); });
    return () => { current = false; };
    // The steps are read once as they open; the follow below keeps them.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [onFinished, userId]);

  // A step an agent did or left for later from the terminal passes here, as
  // one passed in another of the person's tabs does: one tab in sight asks
  // for all of them, and a tab behind asks nothing (followFirstRun).
  const showing = steps?.[0];
  useEffect(() => {
    if (!showing) return undefined;
    return followFirstRun(session, FOLLOW_EVERY, ({ steps: left }, here) => {
      if (left.includes(showing)) return;
      setSteps(left);
      if (left.length > 0) return;
      // The tab that learned they're over finishes them on the server; the others just close.
      if (here) void finish();
      else if (!finished.current) {
        finished.current = true;
        onFinished();
      }
    });
    // finish reads the session it renders with.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session, showing]);

  const pass = (step: FirstRunStep, done: boolean) => {
    void passFirstRunStep(session, step, done).catch(() => undefined).then(async () => {
      const left = (steps ?? []).filter((kept) => kept !== step);
      setSteps(left);
      if (left.length === 0) await finish();
    });
  };

  const step = steps?.[0];
  // A server that has its outside address passes the address step.
  useEffect(() => {
    if (step !== "address") return undefined;
    let current = true;
    void fetchRuntimeConfig(true).then(({ config }) => {
      if (!current) return;
      if (outsideAddress(config.publicAddress ?? "")) pass("address", true);
      else setAddressChecked(true);
    }, () => { if (current) setAddressChecked(true); });
    return () => { current = false; };
    // pass reads the steps it renders with.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [step]);

  const clear = useAfterTyping(Boolean(step) && !blocked);
  if (!step || !clear) return null;
  if (step === "address") {
    return addressChecked ? (
      <RemoteAddressChoice session={session} workspaceId={workspaceId ?? undefined} screen onClose={(address) => pass("address", Boolean(address))} />
    ) : (
      // Until the server's address is read, the step's own heading and lead
      // as it opens with them, not the bare mark (2026-11-06: a logo-only
      // frame between the language and this step).
      <FirstRunScreen step="address" name={t("remoteChoice.title")} view="checking">
        <div className={remoteStyles.screenBody}>
          <div className={remoteStyles.copy}><h2>{t("remoteChoice.title")}</h2><p>{t("remoteChoice.body")}</p></div>
        </div>
      </FirstRunScreen>
    );
  }
  return workspaceId ? <AgentConnectStep session={session} workspaceId={workspaceId} onDone={(connected) => pass("agents", connected)} /> : null;
}
