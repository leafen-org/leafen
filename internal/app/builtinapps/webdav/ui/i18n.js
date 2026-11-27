// The app's words in the person's language (Korean or English).
const words = {
  ko: {
    title: "드라이브",
    lead: "Finder, Windows 탐색기, Office에서 리펜의 페이지와 파일을 폴더처럼 엽니다. 파일을 저장하면 같은 자리에 새 버전이 들어가고, 이전 버전은 페이지 기록에 남습니다.",
    connect: "연결",
    serverAddress: "서버 주소",
    serverAddressHint: "Finder나 탐색기의 서버 주소 칸에 넣습니다.",
    appPassword: "앱 비밀번호",
    appPasswordHint: "연결할 때 넣는 사용자 이름과 비밀번호입니다. 만들 때 한 번만 보입니다.",
    openApps: "만들기",
    mac: "Mac: Finder › 이동 › 서버에 연결",
    windows: "Windows: 파일 탐색기 › 내 PC › 네트워크 드라이브 연결",
    // Finder asks macOS's question on http before its sign-in window (2026-11-11).
    notHttps: "Windows 탐색기는 https 주소에만 연결됩니다. 지금 주소는 https가 아니어서 Mac Finder와 다른 WebDAV 앱으로만 연결할 수 있습니다.",
    finderInsecure: "Mac Finder는 로그인 창보다 먼저 macOS의 \"안전하지 않은 연결\" 질문을 띄웁니다. https가 아닌 주소로 사용자 이름과 비밀번호를 보내기 전에 묻는 것으로, \"연결\"을 누르면 로그인 창이 나옵니다.",
    copy: "복사", copied: "복사됨",
    seen: "보이는 것",
    pagesTitle: "페이지",
    pages: "워크스페이스별 페이지 나무입니다. 볼 수 있는 페이지만 보이고, 편집할 수 있는 페이지에만 파일을 넣을 수 있습니다.",
    filesTitle: "파일",
    files: "페이지에 속하지 않는 파일의 자리입니다. \"내 파일\"은 나만, \"공유 파일\"은 조직 사람 모두 봅니다.",
    sync: "다른 앱과 동기화",
    obsidian: "옵시디언(Remotely Save)",
    obsidianSteps: [
      "옵시디언 커뮤니티 플러그인에서 Remotely Save를 설치하고 원격 서비스로 WebDAV를 고릅니다.",
      "서버 주소에 아래 주소를, 사용자 이름과 비밀번호에 앱 비밀번호를 넣고, 인증 방식은 basic, 깊이(depth)는 '1만 지원'으로 둡니다.",
      "연결을 확인하고 동기화하면 \"내 파일\" 안에 보관함 이름의 폴더가 생깁니다. 에이전트는 이 앱의 도구로 노트를 찾고 읽습니다.",
    ],
    rcloneTitle: "rclone",
    // A client that doesn't say Korean (rclone, most sync tools) sees the
    // English names; either name opens the same folder (2026-10-16).
    rclone: "WebDAV 원격의 vendor를 rclone으로 둡니다. 파일 시각을 지켜 바뀐 파일만 옮깁니다. 한국어를 쓰지 않는 앱에는 Pages, Files, My files, Shared files로 보이고, 어느 이름으로 가도 같은 폴더입니다.",
  },
  en: {
    title: "Drive",
    lead: "Open Leafen's pages and files as folders in Finder, Windows Explorer and Office. Saving a file puts a new version in the same place; the old one stays in the page's history.",
    connect: "Connect",
    serverAddress: "Server address",
    serverAddressHint: "Enter this as the server address in Finder or Explorer.",
    appPassword: "App password",
    appPasswordHint: "The user name and password you connect with. They show once, when you make them.",
    openApps: "Make one",
    mac: "Mac: Finder › Go › Connect to Server",
    windows: "Windows: File Explorer › This PC › Map network drive",
    notHttps: "Windows Explorer connects only to https addresses. This address isn't https, so only Mac Finder and other WebDAV apps can connect.",
    finderInsecure: "Before its login window, Mac Finder shows macOS's question about an insecure connection: macOS asks before sending a user name and password to an address that isn't https, and Connect goes on to the login window.",
    copy: "Copy", copied: "Copied",
    seen: "What you'll see",
    pagesTitle: "Pages",
    pages: "Each workspace's page tree. You see the pages you can read, and can put files only on pages you can edit.",
    filesTitle: "Files",
    files: "A place for files that belong to no page. \"My files\" is yours alone; everyone in the organization sees \"Shared files\".",
    sync: "Sync with other apps",
    obsidian: "Obsidian (Remotely Save)",
    obsidianSteps: [
      "Install Remotely Save from Obsidian's community plugins and choose WebDAV as the remote service.",
      "Enter the address below as the server address, the app password's user name and password, auth type basic, and depth 'only supports depth 1'.",
      "Check the connection and sync: a folder named after the vault appears in \"My files\". Agents find and read its notes with this app's tools.",
    ],
    rcloneTitle: "rclone",
    rclone: "Set the WebDAV remote's vendor to rclone. Leafen keeps the file times, so a sync moves only the files that changed.",
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
