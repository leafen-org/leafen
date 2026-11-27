import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import type {
  AccountProfile,
  AccountProfileMutationInput,
} from "@/authWire.generated";
import { newId } from "@/lib/ids";
import { productErrorMessage } from "@/lib/productErrorPresentation";
import { saveSessionIfCurrent } from "@/lib/authSessionStore";
import { readAccountOrganizationPolicies } from "@/lib/organizationManagedUserPolicies";
import { organizationModule } from "@/lib/organizationModule";
import type { Session } from "@/types";

function sessionContext(session: Session) {
  return `${session.user.id}\u0000${session.sessionId ?? session.token}`;
}

type PendingProfileMutation = {
  avatar: string;
  displayName: string;
  input: AccountProfileMutationInput;
};

export function useAccountProfile(
  session: Session,
  onSession: (session: Session) => void,
) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [snapshot, setSnapshot] = useState<AccountProfile | null>(null);
  const [displayName, setDisplayNameState] = useState(session.user.name);
  const [avatar, setAvatarState] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // An organization managing the person may keep their name and photo; while
  // its rule could not be read they wait for it (held), unsaid.
  const [locked, setLocked] = useState(false);
  const [held, setHeld] = useState(false);
  const lifecycleGeneration = useRef(0);
  const readGeneration = useRef(0);
  const readAbort = useRef<AbortController | null>(null);
  const saveAbort = useRef<AbortController | null>(null);
  const currentSession = useRef(session);
  const pending = useRef<PendingProfileMutation | null>(null);
  currentSession.current = session;

  const isCurrent = useCallback((generation: number, context: string) => (
    lifecycleGeneration.current === generation
      && sessionContext(currentSession.current) === context
  ), []);

  const apply = useCallback((profile: AccountProfile) => {
    setSnapshot(profile);
    setDisplayNameState(profile.displayName);
    setAvatarState(profile.avatar);
  }, []);

  const refresh = useCallback(async (activeSession: Session = session) => {
    const generation = lifecycleGeneration.current;
    const issued = ++readGeneration.current;
    const context = sessionContext(activeSession);
    const controller = new AbortController();
    readAbort.current?.abort();
    readAbort.current = controller;
    const current = () => (
      isCurrent(generation, context) && readGeneration.current === issued
    );
    setLoading(true);
    setError("");
    try {
      const profile = await api.readAccountProfile(activeSession, controller.signal);
      // The organizations managing an account are the organization module's.
      const policies = organizationModule()
        ? await readAccountOrganizationPolicies(activeSession, controller.signal).catch(() => undefined)
        : null;
      if (current()) {
        apply(profile);
        setLocked(policies ? !policies.profileChanges : false);
        setHeld(policies === undefined);
      }
      return profile;
    } catch (caught) {
      // A failure says why and what to do next, as the account's other
      // settings do (2026-11-06: a restored server's "reload from the server"
      // read only as "프로필을 변경하지 못했습니다.").
      if (current()) setError(productErrorMessage(caught, "accountProfile.read"));
      return null;
    } finally {
      if (current()) {
        readAbort.current = null;
        setLoading(false);
      }
    }
  }, [apply, isCurrent, session, t]);

  useEffect(() => {
    const generation = ++lifecycleGeneration.current;
    saveAbort.current?.abort();
    saveAbort.current = null;
    pending.current = null;
    setBusy(false);
    void refresh();
    return () => {
      readAbort.current?.abort();
      readAbort.current = null;
      saveAbort.current?.abort();
      saveAbort.current = null;
      if (lifecycleGeneration.current === generation) lifecycleGeneration.current += 1;
      readGeneration.current += 1;
    };
  }, [refresh]);

  function setDisplayName(value: string) {
    setDisplayNameState(value);
    pending.current = null;
    setError("");
  }

  function setAvatar(value: string) {
    setAvatarState(value);
    pending.current = null;
    setError("");
  }

  // What is saved is what the fields hold, or what was just chosen there.
  async function save(next?: { displayName?: string; avatar?: string }) {
    if (!snapshot || busy) return;
    const normalizedName = (next?.displayName ?? displayName).trim();
    const normalizedAvatar = (next?.avatar ?? avatar).trim();
    if (!normalizedName) {
      setError(t("errUpdateProfile"));
      return;
    }
    const generation = lifecycleGeneration.current;
    const context = sessionContext(session);
    const controller = new AbortController();
    saveAbort.current?.abort();
    saveAbort.current = controller;
    const current = () => (
      isCurrent(generation, context) && saveAbort.current === controller
    );
    const retained = pending.current;
    const input = retained && retained.displayName === normalizedName
      && retained.avatar === normalizedAvatar
      ? retained.input
      : {
          requestId: `profile-${newId()}`,
          baseRevision: snapshot.revision,
          displayName: normalizedName,
          avatar: normalizedAvatar,
        };
    pending.current = { displayName: normalizedName, avatar: normalizedAvatar, input };
    setBusy(true);
    setError("");
    try {
      const result = await api.saveAccountProfile(session, input, controller.signal);
      if (!current()) return;
      pending.current = null;
      apply(result.profile);
      const nextSession = {
        ...session,
        user: { ...session.user, name: result.profile.displayName },
      };
      if (!await saveSessionIfCurrent(session, nextSession) || !current()) return;
      onSession(nextSession);
    } catch (caught) {
      if (current()) setError(productErrorMessage(caught, "accountProfile.update"));
    } finally {
      if (current()) setBusy(false);
      if (saveAbort.current === controller) saveAbort.current = null;
    }
  }

  return {
    avatar,
    busy,
    displayName,
    error,
    held,
    loading,
    locked,
    refresh,
    save,
    setAvatar,
    setDisplayName,
    snapshot,
  };
}
