package app

import "runtime"

// Why this server's build has no app platform, so the screens say it where
// the apps would be (the multi-server decision's 2026-10-20 "폰 앱의 자기
// 서버는 체험판", back 2026-11-01): "phone" — a phone app's own server, a
// trial that moves to an always-on server where apps run; "store" — the Mac
// App Store edition, whose apps come with the app downloaded from Leafen;
// "" — it has the platform, or was built without it for another reason.
func appPlatformMissingReason() string {
	switch {
	case appPlatformAvailable:
		return ""
	case runtime.GOOS == "ios" || runtime.GOOS == "android":
		return "phone"
	case appStoreEdition:
		return "store"
	}
	return ""
}
