// The app's words in the person's language (Korean or English).
const words = {
  ko: {
    reasons: {
      "a request needs a title": "제목을 입력하세요.",
      "choose someone else to approve it": "다른 결재자를 선택하세요.",
      "the amount is 0 or more": "금액은 0 이상이어야 합니다.",
      "that isn't a page": "이 서버의 페이지가 아닙니다.",
      "only your own draft can be sent": "내 초안만 결재를 올릴 수 있습니다.",
      "no such request": "이 결재를 찾을 수 없습니다.",
      "only its approver decides it": "결재자만 결정할 수 있습니다.",
      "it was already decided": "이미 결정된 결재입니다.",
    },
    title: "결재", lead: "결재를 올리고, 내게 온 결재를 처리하고, 요청이 어디까지 왔는지 봅니다.",
    waiting: "내 결재함", mine: "내가 올린 결재", new: "새 결재",
    countWaiting: "대기", countApproved: "승인", countRejected: "반려",
    emptyWaiting: "결정할 결재가 없습니다.", emptyMine: "올린 결재가 없습니다.",
    from: (name) => `${name}님이 올림`, to: (name) => `결재자 ${name}`, amount: (value) => `${value}원`,
    note: "메모(선택)", approve: "승인", reject: "반려", openPage: "관련 페이지 열기", finish: "완성해서 올리기",
    formTitle: "제목", formApprover: "결재자", formAmount: "금액", formReason: "사유", formPage: "관련 페이지",
    choose: "결재자를 선택하세요", findApprover: "이름이나 이메일로 찾기", noMatch: "찾는 사람이 없습니다", nobodyElse: "결재할 다른 사람이 아직 없습니다. 멤버를 초대하면 여기서 고를 수 있습니다.", send: "결재 올리기", sent: "결재를 올렸습니다.", approved: "승인했습니다.", rejected: "반려했습니다.",
    draft: "초안", untitled: "제목 없음", loading: "불러오는 중…", failed: "불러오지 못했습니다.",
    blockWaiting: "내 결재 대기", blockMine: "진행 중인 내 결재", open: "결재 열기",
    statuses: { "초안": "초안", "대기": "대기", "승인": "승인", "반려": "반려" },
  },
  en: {
    title: "Approvals", lead: "Ask for approval, decide what waits for you and see where every request stands.",
    waiting: "Waiting for me", mine: "My requests", new: "New request",
    countWaiting: "Waiting", countApproved: "Approved", countRejected: "Rejected",
    emptyWaiting: "Nothing waits for you.", emptyMine: "You haven't asked for anything.",
    from: (name) => `From ${name}`, to: (name) => `To ${name}`, amount: (value) => value,
    note: "Note (optional)", approve: "Approve", reject: "Reject", openPage: "Open the page", finish: "Finish and send",
    formTitle: "Title", formApprover: "Approver", formAmount: "Amount", formReason: "Reason", formPage: "Page",
    choose: "Choose who approves it", findApprover: "Find by name or email", noMatch: "Nobody matches", nobodyElse: "There's nobody else to approve it yet. Invite a member to choose them here.", send: "Ask for approval", sent: "Your request is on its way.", approved: "Approved.", rejected: "Rejected.",
    draft: "Draft", untitled: "Untitled", loading: "Loading…", failed: "Couldn't load.",
    blockWaiting: "Waiting for me", blockMine: "My open requests", open: "Open Approvals",
    statuses: { "초안": "Draft", "대기": "Waiting", "승인": "Approved", "반려": "Rejected" },
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
