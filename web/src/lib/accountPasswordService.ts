import { api } from "@/api";
import type { AccountPasswordChangeInput } from "@/authWire.generated";
import type { Session } from "@/types";
import { saveSessionIfCurrent } from "./authSessionStore";
import { handOverSignIn } from "./signedOutResponse";

export type AccountPasswordTransport = Pick<typeof api, "changeAccountPassword">;

export const apiAccountPasswordTransport: AccountPasswordTransport = {
  changeAccountPassword: (session, input) => api.changeAccountPassword(session, input),
};

export function createAccountPasswordService(
  transport: AccountPasswordTransport = apiAccountPasswordTransport,
) {
  return {
    // The change ends every sign-in of the account, this device's too, and
    // answers with this device's next one: what the old one is refused
    // meanwhile waits for it (lib/signedOutResponse.ts).
    change(session: Session, input: AccountPasswordChangeInput) {
      return handOverSignIn(async () => {
        const nextSession = await transport.changeAccountPassword(session, input);
        if (!await saveSessionIfCurrent(session, nextSession)) throw new DOMException("Login changed.", "AbortError");
        return nextSession;
      });
    },
  };
}

export const accountPasswordService = createAccountPasswordService();
