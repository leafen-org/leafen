import { useRef, useState, type FormEvent, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { loadSession } from "@/lib/authSessionStore";
import { isPackagedClient } from "@/lib/packagedClient";
import { knownProfileAt } from "@/lib/addressChangeFollow";
import { addServerProfile, reachedAt } from "@/lib/profileRegistry";
import { openProfile } from "@/lib/profileSwitch";
import { beginAddingAccount, endAddingAccount } from "@/lib/serverAccounts";
import { startServerConnect } from "@/lib/switcherLists";
import { answeringServer, typedServerAddress } from "@/lib/typedServerAddress";
import { isActiveServerOrigin, serverIdentity } from "@/lib/serverOrigin";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { useClosedForLink } from "@/lib/productWindows";
import { styles } from "./sidebarStyles";

/**
 * "Add account": an account on another Leafen server, signed in on that
 * server itself. The server's address — read as the first install
 * reads one, so it needs no scheme — is checked, kept in the menu, and opened
 * for its sign-in (an invite link at its page); a page a server serves asks
 * there to list that account's workspaces here (lib/switcherLists.ts).
 */
export function AddServerAccountDialog({ onClose, returnFocusRef }: { onClose: () => void; returnFocusRef?: RefObject<HTMLElement | null> }) {
  const { t } = useTranslation("sidebar");
  const [address, setAddress] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const dialog = useRef<HTMLFormElement>(null);
  // A modal layer: Escape closes it, and only it, unless it is busy.
  const { zIndex } = useOverlayLayer(true, { kind: "dialog", ref: dialog, modalOwned: () => [dialog.current?.parentElement],
    returnFocusRef, onEscape: () => { if (!busy) onClose(); } });
  // A link going to a place here closes it as Escape does (closeWindowsForLink), unless it is busy.
  useClosedForLink(() => { if (!busy) onClose(); });

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const { origins, path } = typedServerAddress(address);
    if (origins.length === 0) {
      setError(t("profiles.invalidAddress"));
      return;
    }
    // This very server, at any address it answers at or forwards from: another account on it.
    if (origins.some((origin) => origin === serverIdentity() || isActiveServerOrigin(origin))) {
      onClose();
      beginAddingAccount();
      return;
    }
    setBusy(true);
    setError("");
    const found = await answeringServer(origins);
    if (!("origin" in found)) {
      setBusy(false);
      setError(t(found.failure === "plain" ? "profiles.plainAddressRefused" : "profiles.notLeafen"));
      return;
    }
    // A server already known, at the address it moved to, is that account (addressChangeFollow.ts).
    const known = await knownProfileAt(found.origin);
    if (known && known === serverIdentity()) {
      onClose();
      beginAddingAccount();
      return;
    }
    const profile = addServerProfile(known || found.origin);
    if (!profile) {
      setBusy(false);
      return;
    }
    const origin = reachedAt(profile);
    // That server's own sign-in or workspace takes over: this server's "계정
    // 추가" window is done, and isn't kept to open again on coming back
    // (2026-11-05: in the app it stayed over the workspace of a server
    // already signed in).
    endAddingAccount();
    const signedIn = loadSession();
    // A browser hands its sign-in over to the server added; the app opens it
    // and signs in there, keeping that sign-in in the keychain. A setup link
    // opens that server's setup with its code, as opening it would: a server
    // not set up has no sign-in to take one (2026-11-07: its code was left out).
    if (!isPackagedClient() && signedIn && !path.startsWith("/setup")) {
      await startServerConnect(signedIn, origin);
      return;
    }
    openProfile(profile.id, "", path);
  }

  return (
    <div className={styles.pendingSignOutBackdrop} style={{ zIndex }} role="presentation" onMouseDown={(event) => {
      if (event.target === event.currentTarget && !busy) onClose();
    }}>
      <form
        ref={dialog}
        className={styles.pendingSignOutDialog}
        role="dialog"
        aria-modal="true"
        aria-labelledby="add-server-account-title"
        noValidate
        onSubmit={(event) => void submit(event)}
      >
        <h2 id="add-server-account-title">{t("profiles.addTitle")}</h2>
        <p>{t("profiles.addDescription")}</p>
        <label className={styles.addServerLabel} htmlFor="add-server-address">{t("profiles.serverAddress")}</label>
        <input
          ref={input}
          autoFocus
          id="add-server-address"
          className={styles.addServerInput}
          type="url"
          inputMode="url"
          autoComplete="url"
          placeholder="https://leafen.example.com"
          value={address}
          onChange={(event) => { setAddress(event.target.value); setError(""); }}
          disabled={busy}
          required
          aria-invalid={error ? true : undefined}
          aria-describedby={error ? "add-server-error" : undefined}
        />
        {error ? <p id="add-server-error" className={styles.addServerError} role="alert">{error}</p> : null}
        <div className={styles.pendingSignOutActions}>
          <button type="button" className={styles.pendingSignOutCancel} onClick={onClose} disabled={busy}>
            {t("profiles.cancel")}
          </button>
          <button type="submit" className={styles.addServerContinue} disabled={busy}>
            {t("profiles.continue")}
          </button>
        </div>
      </form>
    </div>
  );
}
