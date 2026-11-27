// The app's words in the person's language (Korean or English).
const words = {
  ko: {
    title: "메일", inbox: "받은편지함", compose: "새 메일", refresh: "새로고침", account: "계정 바꾸기", disconnect: "연결 끊기", again: "계정 정보 다시 넣기",
    company: "회사 메일", gmail: "Gmail", useCompany: "회사 메일 쓰기", useGmail: "Gmail 쓰기",
    gmailTitle: "Gmail", connectGmail: "Gmail 연결",
    gmailLead: "Gmail 주소와 앱 비밀번호를 리펜 창에 넣으면 리펜이 암호화해 보관하고, 메일을 읽고 보낼 때만 씁니다. 앱 비밀번호는 Google 계정의 보안 → 2단계 인증 → 앱 비밀번호에서 만듭니다(Google 비밀번호가 아닙니다).",
    gmailAppPasswords: "Google 계정에서 앱 비밀번호 만들기",
    gmailSignInLead: "관리자가 서버에 Google 클라이언트를 준비했다면 Google 로그인으로도 연결할 수 있습니다.",
    gmailSignIn: "Google 로그인으로 연결", useGmailSignIn: "Google 로그인으로 쓰기",
    gmailNotReady: "관리자가 아직 Google 로그인 연결을 준비하지 않았습니다. 앱 비밀번호로 연결하거나, 관리자의 에이전트에게 Google 클라이언트 만들기 안내를 부탁하세요.",
    back: "목록으로", reply: "답장", send: "보내기", cancel: "취소", to: "받는 사람", cc: "참조", addCc: "참조 추가", subject: "제목", body: "내용",
    from: "보낸 사람", toLine: "받는 사람", ccLine: "참조", noSubject: "(제목 없음)", empty: "받은편지함이 비어 있습니다.", pick: "읽을 메일을 선택하세요.",
    tooLong: (limit) => `메일이 이 앱에서 한 번에 보낼 수 있는 크기(${limit} KB)를 넘었습니다. 내용을 줄이거나 나눠 보내세요.`,
    nearlyLong: (kb, limit) => `메일이 ${kb} KB로, 한 번에 보낼 수 있는 ${limit} KB에 가까워졌습니다.`,
    loading: "불러오는 중…", sent: "보냈습니다.", sending: "보내는 중…", attachments: "첨부", truncated: "메일이 커서 앞부분만 보입니다.",
    total: (count) => `모두 ${count}통 중 최근 메일`, unread: "읽지 않음", attachment: "첨부 있음",
    off: "관리자가 이 앱의 설정에서 '사람들이 이 앱에 자기 계정 정보를 줄 수 있게 하기'를 켜면 연결할 수 있습니다.",
    connectTitle: "회사 메일",
    connectLead: "조직 메일 서버의 내 계정을 리펜 창에 넣으면 리펜이 암호화해 보관하고, 메일을 읽고 보낼 때만 씁니다. 이 앱은 그 값을 볼 수 없습니다.",
    connect: "메일 계정 연결",
    errors: {
      settings: "관리자가 메일 서버 주소를 아직 정하지 않았습니다.",
      connect: "메일 서버에 연결할 수 없습니다. 잠시 뒤 다시 시도하세요.",
      login: "메일 서버가 로그인을 거부했습니다. 계정 정보를 다시 넣으세요.",
      credentials: "메일 계정이 연결되어 있지 않습니다.",
      server: "메일 서버가 예상과 다르게 답했습니다. 잠시 뒤 다시 시도하세요.",
      input: "받는 사람 주소나 제목을 확인하세요.",
      recipients: "메일 서버가 받는 사람을 받지 않았습니다. 주소를 확인하거나, 서버가 한 메일의 받는 사람 수를 제한한다면 나눠 보내세요.",
      declined: "보내지 않았습니다.",
    },
  },
  en: {
    title: "Mail", inbox: "Inbox", compose: "New message", refresh: "Refresh", account: "Switch account", disconnect: "Disconnect", again: "Enter your account again",
    company: "Company mail", gmail: "Gmail", useCompany: "Use company mail", useGmail: "Use Gmail",
    gmailTitle: "Gmail", connectGmail: "Connect Gmail",
    gmailLead: "Give Leafen your Gmail address and an app password in its window: Leafen stores them encrypted and uses them only to read and send your mail. You make an app password in your Google account under Security → 2-Step Verification → App passwords — it isn't your Google password.",
    gmailAppPasswords: "Make an app password in your Google account",
    gmailSignInLead: "If an administrator set up a Google client on this server, you can also connect by signing in with Google.",
    gmailSignIn: "Sign in with Google instead", useGmailSignIn: "Use the Google sign-in",
    gmailNotReady: "An administrator hasn't set up signing in with Google yet. Connect with an app password, or ask the administrator's agent for the guide to making a Google client.",
    back: "Back to inbox", reply: "Reply", send: "Send", cancel: "Cancel", to: "To", cc: "Cc", addCc: "Add Cc", subject: "Subject", body: "Message",
    from: "From", toLine: "To", ccLine: "Cc", noSubject: "(no subject)", empty: "Your inbox is empty.", pick: "Choose a message to read.",
    tooLong: (limit) => `This message is past the ${limit} KB this app sends at once. Shorten it or send it in parts.`,
    nearlyLong: (kb, limit) => `This message is ${kb} KB, near the ${limit} KB this app sends at once.`,
    loading: "Loading…", sent: "Sent.", sending: "Sending…", attachments: "Attachments", truncated: "The message is large; only its beginning is shown.",
    total: (count) => `Newest of ${count}`, unread: "Unread", attachment: "Has attachments",
    off: "You can connect once an administrator turns on 'Let people give this app their account details' in the app's settings.",
    connectTitle: "Company mail",
    connectLead: "Type your account on the organization's mail server into Leafen's window: Leafen stores it encrypted and uses it only to read and send your mail. This app never sees it.",
    connect: "Connect mail account",
    errors: {
      settings: "An administrator hasn't set the mail server's address yet.",
      connect: "Can't reach the mail server. Try again in a moment.",
      login: "The mail server refused the login. Enter your account again.",
      credentials: "Your mail account isn't connected.",
      server: "The mail server answered unexpectedly. Try again in a moment.",
      input: "Check the recipients and the subject.",
      recipients: "The mail server didn't take a recipient. Check the addresses, or send in parts if it limits how many people one message reaches.",
      declined: "Not sent.",
    },
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
