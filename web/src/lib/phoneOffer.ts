// A first run's device step passed on an address only this computer opens
// offered no phone: once an outside address comes up and the account still
// has one device, the phone is offered once ("이제 휴대폰도 등록해 두세요").
// Kept on this device for the account.
const PHONE_KEY = (userId: string) => `leafen:v2:phone-offer:${userId}`;

export function holdPhoneOffer(userId: string) {
  try {
    window.localStorage.setItem(PHONE_KEY(userId), "1");
  } catch {
    // Without storage the phone is offered from Settings › 계정 only.
  }
}

export function phoneOfferHeld(userId: string): boolean {
  try {
    return window.localStorage.getItem(PHONE_KEY(userId)) === "1";
  } catch {
    return false;
  }
}

export function settlePhoneOffer(userId: string) {
  try {
    window.localStorage.removeItem(PHONE_KEY(userId));
  } catch {
    // Nothing kept there.
  }
}
