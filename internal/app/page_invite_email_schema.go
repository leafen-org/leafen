package app

// An invitation to a page for an address no account has verified goes out
// by email, once for its share, from a queue the email runner sends. It
// leaves with its workspace or its inviter's account.

const (
	pageInviteEmailsVersion = 325
	pageInviteEmailsName    = "native_page_invite_emails"
)
