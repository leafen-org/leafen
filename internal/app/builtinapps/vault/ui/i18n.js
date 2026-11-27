// The app's words in the person's language (Korean or English).
const words = {
  ko: {
    reasons: {
      "give an email address": "이메일 주소를 입력하세요.",
      "your vault is ready; log in to it from your Bitwarden app": "금고가 준비되었습니다. Bitwarden 앱에서 로그인하세요.",
      "that email has a vault here; choose another": "이 이메일에는 이미 금고가 있습니다. 다른 이메일을 선택하세요.",
      "that email is someone else's in Leafen; use your own": "다른 사람이 리펜에서 쓰는 이메일입니다. 내 이메일을 쓰세요.",
      "you have no vault here": "이 서버에 내 금고가 없습니다.",
      "open the Vault app in an organization": "조직에서 금고 앱을 여세요.",
      "only the organization's owners and people admins see its shared vaults": "조직의 소유자와 멤버 관리자만 공유 금고를 볼 수 있습니다.",
      "make your vault from your Bitwarden app first": "먼저 Bitwarden 앱에서 금고를 만드세요.",
      "that shared vault isn't here": "이 공유 금고를 찾을 수 없습니다.",
      "only the shared vault's owners and administrators link groups": "공유 금고의 소유자와 관리자만 그룹을 연결할 수 있습니다.",
      "that group isn't linked to the shared vault": "이 그룹은 공유 금고에 연결되어 있지 않습니다.",
      "the vault isn't set up in this organization": "이 조직에 금고가 설정되어 있지 않습니다.",
      "too many people to follow while someone waits; the schedule follows them": "지금 맞추기에는 사람이 너무 많습니다. 예약된 작업이 맞춥니다.",
      "the organization's groups couldn't be read": "조직의 그룹을 읽지 못했습니다.",
      "the groups' people couldn't be read": "그룹의 사람을 읽지 못했습니다.",
      "the app's people couldn't be read": "앱의 사람을 읽지 못했습니다.",
    },
    title: "금고",
    lead: "Bitwarden 앱(브라우저 확장·휴대폰·데스크톱)이 리펜을 서버로 씁니다. 금고는 그 앱에서 암호화되어 리펜·관리자·에이전트도 내용을 볼 수 없습니다.",
    server: "서버 주소", copy: "복사", copied: "복사했습니다.",
    serverHint: "Bitwarden 앱 로그인 화면의 '자체 호스팅'에 넣습니다.",
    serverHttps: "Bitwarden 앱(브라우저 확장 등)은 https 주소만 받습니다. 지금 주소는 http라 '자체 호스팅'에 넣어도 저장되지 않습니다.",
    email: "Bitwarden에서 쓸 이메일", prepare: "금고 준비",
    masterNote: "마스터 비밀번호는 리펜 비밀번호와 따로이고, 잊으면 아무도 되찾지 못합니다.",
    prepared: (email, until) => `${email}로 준비했습니다 · ${until}까지`,
    steps: [
      "Bitwarden 앱 로그인 화면에서 '자체 호스팅'을 선택하고 위 서버 주소를 넣습니다.",
      "'계정 만들기'에서 이 이메일과 새 마스터 비밀번호를 정합니다.",
      "그 이메일과 마스터 비밀번호로 로그인합니다.",
    ],
    otherEmail: "다른 이메일로",
    made: (email, date, items) => `${email} · ${date}에 만듦 · 항목 ${items}개`,
    devices: "기기", noDevices: "로그인한 기기가 없습니다.", seen: (when) => `마지막 사용 ${when}`,
    signout: "모든 기기 로그아웃", signedOut: "모든 기기를 로그아웃했습니다.",
    reset: "금고 지우고 새로 시작",
    resetWarning: "금고의 모든 항목이 지워지고 되돌릴 수 없습니다. 마스터 비밀번호를 잊었을 때만 쓰세요.",
    resetConfirm: "지우기", keep: "그대로 두기",
    // Said to everyone alike: what has to be on and where, and who turns it on, not "ask an administrator",
    // which a server administrator read too (2026-11-06).
    footnote: "Bitwarden 앱이 접속하지 못하면 설정 › 서버 › 앱 › 금고의 사용량에서 '로그인 없이 열기'가 켜져 있어야 합니다. 서버 관리자가 켤 수 있습니다.",
    loading: "불러오는 중…", noOrganization: "금고는 조직 안에서 씁니다. 조직의 워크스페이스에서 여세요.",
    shared: "공유 금고", beta: "베타",
    noShared: "아직 공유 금고가 없습니다. 웹 금고에서 조직을 만들고 리펜 그룹을 연결하면 그 그룹 사람이 들어옵니다.",
    roles: ["소유자", "관리자", "멤버", "멤버", "멤버"], waitingMe: "확인 대기",
    members: (count) => `멤버 ${count}명`, waiting: (count) => `확인 대기 ${count}명 — 웹 금고의 멤버에서 확인`,
    chooseGroup: "그룹 선택", link: "그룹 연결", unlink: (name) => `${name} 연결 끊기`, linked: "그룹을 연결했습니다.", unlinked: "그룹 연결을 끊었습니다.",
    sharedNote: "멤버가 빠지면 남은 사람에게 그 사람이 볼 수 있던 비밀번호를 바꾸라고 알립니다(빠진 기기의 사본은 지울 수 없음).",
    webVault: "웹 금고 열기", webVaultHttps: "웹 금고도 https 주소에서만 열립니다. 위 서버 주소가 https가 되면 여기에서 엽니다.",
    webVaultNotice: (version) => `웹 금고: Bitwarden 웹 금고(Vaultwarden 빌드) ${version}, GPL-3.0 — `, source: "소스",
    overview: "조직의 공유 금고", overviewBadge: "관리",
    overviewItems: (count) => `항목 ${count}개`, overviewNoMembers: "멤버가 없습니다.",
    overviewNone: "이 조직에는 아직 공유 금고가 없습니다.",
    overviewRoles: { owner: "소유자", admin: "관리자", user: "멤버", manager: "멤버", custom: "멤버" },
    overviewStates: { accepted: "확인 대기", invited: "초대됨", revoked: "액세스 해제됨" },
    overviewNote: "조직 관리자는 공유 금고의 멤버와 연결 그룹만 봅니다. 항목의 내용과 컬렉션 이름은 그 금고 멤버의 Bitwarden 앱에서만 열립니다.",
  },
  en: {
    title: "Vault",
    lead: "Bitwarden's apps — the browser extension, phone and desktop — use Leafen as their server. Your vault is encrypted in those apps, so Leafen, administrators and agents can't read it.",
    server: "Server address", copy: "Copy", copied: "Copied.",
    serverHint: "Enter it under 'Self-hosted' on the Bitwarden app's login screen.",
    serverHttps: "Bitwarden's apps, such as the browser extension, take only an https address: this one is http, so 'Self-hosted' won't save it.",
    email: "Email for Bitwarden", prepare: "Prepare my vault",
    masterNote: "Your master password is separate from your Leafen password, and if you forget it no one can recover it.",
    prepared: (email, until) => `Prepared for ${email} · until ${until}`,
    steps: [
      "On the Bitwarden app's login screen, choose 'Self-hosted' and enter the server address above.",
      "Under 'Create account', use this email and choose a new master password.",
      "Log in with that email and master password.",
    ],
    otherEmail: "Use another email",
    made: (email, date, items) => `${email} · made ${date} · ${items} items`,
    devices: "Devices", noDevices: "No device is logged in.", seen: (when) => `Last used ${when}`,
    signout: "Log out every device", signedOut: "Every device is logged out.",
    reset: "Delete my vault and start over",
    resetWarning: "Everything in the vault is deleted, for good. Use this only if you forgot your master password.",
    resetConfirm: "Delete", keep: "Keep it",
    footnote: "If Bitwarden's apps can't connect, 'Open without logging in' needs to be on in Settings › Server › Apps › Vault's usage. A server administrator can turn it on.",
    loading: "Loading…", noOrganization: "The vault is used inside an organization; open it from one of its workspaces.",
    shared: "Shared vaults", beta: "Beta",
    noShared: "No shared vault yet. Make an organization in the web vault and link a Leafen group: the group's people join it.",
    roles: ["Owner", "Admin", "Member", "Member", "Member"], waitingMe: "Waiting to be confirmed",
    members: (count) => `${count} ${count === 1 ? "member" : "members"}`, waiting: (count) => `${count} waiting — confirm them under Members in the web vault`,
    chooseGroup: "Choose a group", link: "Link group", unlink: (name) => `Unlink ${name}`, linked: "The group is linked.", unlinked: "The group is unlinked.",
    sharedNote: "When someone leaves, the others are asked to change the passwords they could see (copies on their devices can't be wiped).",
    webVault: "Open web vault", webVaultHttps: "The web vault, too, opens only at an https address: once the server address above is https, it opens from here.",
    webVaultNotice: (version) => `Web vault: Bitwarden's web vault (Vaultwarden build) ${version}, GPL-3.0 — `, source: "source",
    overview: "The organization's shared vaults", overviewBadge: "Admin",
    overviewItems: (count) => `${count} ${count === 1 ? "item" : "items"}`, overviewNoMembers: "No members.",
    overviewNone: "This organization has no shared vault yet.",
    overviewRoles: { owner: "Owner", admin: "Admin", user: "Member", manager: "Member", custom: "Member" },
    overviewStates: { accepted: "waiting to be confirmed", invited: "invited", revoked: "access revoked" },
    overviewNote: "An organization's administrators see a shared vault's members and linked groups only. Its items and collection names open only in its members' Bitwarden apps.",
  },
};

export function wordsFor(locale) {
  return String(locale || "").startsWith("ko") ? words.ko : words.en;
}

export function applyTheme(app) {
  document.documentElement.dataset.theme = app.context.theme;
  app.onTheme((theme) => { document.documentElement.dataset.theme = theme; });
}

export function element(tag, attributes = {}, ...children) {
  const node = document.createElement(tag);
  for (const [name, value] of Object.entries(attributes)) {
    if (name === "on") for (const [event, handler] of Object.entries(value)) node.addEventListener(event, handler);
    else if (value !== undefined && value !== null && value !== false) node.setAttribute(name, value === true ? "" : String(value));
  }
  node.append(...children.flat().filter((child) => child !== null && child !== undefined && child !== false));
  return node;
}
