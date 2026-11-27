// The app's words in the person's language (Korean or English).
const words = {
  ko: {
    title: "웹 클립",
    lead: "클리퍼가 보낸 웹 페이지가 내 \"웹 클립\" DB의 페이지로 저장됩니다. 같은 주소를 다시 보내면 새 페이지가 됩니다.",
    connect: "클리퍼 연결",
    stepPassword: "\"앱\" 화면의 \"앱 비밀번호\"에서 이 앱의 비밀번호를 만듭니다.",
    openApps: "앱 비밀번호 만들기",
    stepAddress: "클리퍼의 웹훅 주소에 넣습니다.",
    stepHeader: "머리(header) Authorization에 넣습니다. 〈앱 비밀번호〉 자리에 만든 비밀번호를 씁니다.",
    headerValue: "Bearer 〈앱 비밀번호〉",
    template: "MarkSnip 본문 템플릿",
    templateHint: "MarkSnip 설정의 웹훅 본문(JSON)에 붙여넣습니다. 다른 도구는 같은 이름(title·url·content)으로 보내면 됩니다.",
    copy: "복사", copied: "복사했습니다.",
    notPublic: "MarkSnip은 https의 공개 주소에만 보냅니다. 지금 리펜 주소(http, 사설 IP, .local 같은 이름)로는 MarkSnip이 보내지 못합니다. 휴대폰 단축어처럼 주소를 가리지 않는 도구는 됩니다.",
    recent: "최근 클립", none: "아직 받은 클립이 없습니다.", openDatabase: "웹 클립 DB 열기",
    loading: "불러오는 중…", noOrganization: "웹 클립은 조직 안에서 씁니다. 조직의 워크스페이스에서 여세요.",
  },
  en: {
    title: "Web Clips",
    lead: "Web pages your clipper sends are kept as pages in your own Web Clips database. Sending the same address again makes a new page.",
    connect: "Connect a clipper",
    stepPassword: "Make a password for this app under \"App passwords\" on Leafen's Apps page.",
    openApps: "Make an app password",
    stepAddress: "Enter this as the clipper's webhook address.",
    stepHeader: "Add it as the Authorization header, with the password you made in place of <app password>.",
    headerValue: "Bearer <app password>",
    template: "MarkSnip body template",
    templateHint: "Paste it as the webhook's JSON body in MarkSnip's settings. Other tools can send the same names (title, url, content).",
    copy: "Copy", copied: "Copied.",
    notPublic: "MarkSnip sends only to public https addresses, so it can't send to Leafen's address as it is now (http, a private IP or a name such as .local). Tools that take any address, such as a phone shortcut, can.",
    recent: "Recent clips", none: "No clips yet.", openDatabase: "Open the Web Clips database",
    loading: "Loading…", noOrganization: "Web Clips works inside an organization; open it from one of its workspaces.",
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
