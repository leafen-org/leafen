package app

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/pocketbase/pocketbase/core"
)

// Why a backup, a restore or a backup file failed, for the people and agents
// who have to fix it (user 2026-11-22: a WebDAV server answering 301 left
// only LEAFEN_SCHEDULED_BACKUP_FAILED in the status and a log line with no
// words, and the cause was found by asking the server by hand). A failure
// has a code — the common causes each their own: refused credentials, the
// place missing, the place's address moved, no room, the place not
// answering — and a reason in the place's own words with no secret in it,
// shown with the code in the status and kept in the diagnostic log.

// scheduledBackupErrorCode names why a run failed as the screen tells it.
func scheduledBackupErrorCode(err error) string {
	status, s3Code, _ := s3ResponseOf(err)
	webdavStatus := webdavAnsweredStatus(err)
	switch {
	// A store that named the failure itself, as a phone's picked folder
	// that no longer opens (pick it again), is taken at its word.
	case backupFailureNamed(err, errorCodeScheduledBackupFolderUnavailable):
		return errorCodeScheduledBackupFolderUnavailable
	case errors.Is(err, errBackupProtectionRequired):
		return errorCodeBackupProtectionRequired
	case errors.Is(err, errScheduledBackupPlaceForeign):
		return errorCodeScheduledBackupPlaceForeign
	case errors.Is(err, errScheduledBackupPlaceOpened):
		return errorCodeScheduledBackupPlaceOpened
	case errors.Is(err, errWebDAVBackupUnauthorized):
		return errorCodeScheduledBackupWebDAVUnauthorized
	case errors.Is(err, errS3BackupUnauthorized):
		return errorCodeScheduledBackupS3Unauthorized
	case errors.Is(err, syscall.ENOSPC), errors.Is(err, syscall.EDQUOT), errors.Is(err, errWebDAVBackupNoSpace),
		s3Code == "EntityTooLarge", s3Code == "QuotaExceeded":
		return errorCodeScheduledBackupNoSpace
	// A server that sends the request elsewhere: a WebDAV folder's address
	// wanting its last slash or another path, storage in another region.
	case webdavStatus >= 300 && webdavStatus < 400, status >= 300 && status < 400,
		s3Code == "PermanentRedirect", s3Code == "TemporaryRedirect", s3Code == "AuthorizationHeaderMalformed":
		return errorCodeScheduledBackupPlaceMoved
	case s3Code == "NoSuchBucket", errors.Is(err, fs.ErrNotExist), webdavStatus == 404, webdavStatus == 409:
		return errorCodeScheduledBackupPlaceMissing
	case errors.Is(err, fs.ErrPermission), errors.Is(err, syscall.EROFS):
		return errorCodeScheduledBackupFolderUnavailable
	case backupPlaceUnreachable(err):
		return errorCodeScheduledBackupPlaceUnreachable
	default:
		return errorCodeScheduledBackupFailed
	}
}

// backupFailureNamed is whether a part of a failure is the code itself.
func backupFailureNamed(err error, code string) bool {
	queue := []error{err}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == nil {
			continue
		}
		if current.Error() == code {
			return true
		}
		switch wrapped := current.(type) {
		case interface{ Unwrap() []error }:
			queue = append(queue, wrapped.Unwrap()...)
		case interface{ Unwrap() error }:
			queue = append(queue, wrapped.Unwrap())
		}
	}
	return false
}

// backupPlaceUnreachable is a place that didn't answer at all: no such
// host, the connection refused or cut, no answer in time, a certificate
// that doesn't hold.
// Not any net.Error: a system's error number is one too, a folder's
// "permission denied" as well.
func backupPlaceUnreachable(err error) bool {
	var operation *net.OpError
	var address *url.Error
	var dns *net.DNSError
	// An answer cut off midway is a place that stopped answering too.
	return errors.As(err, &operation) || errors.As(err, &address) || errors.As(err, &dns) ||
		errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.ETIMEDOUT) || errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, io.ErrUnexpectedEOF)
}

var (
	// "WebDAV PROPFIND answered 401 Unauthorized": the status and the
	// server's own words for it (scheduled_backup_webdav.go).
	webdavAnsweredPattern = regexp.MustCompile(`WebDAV ([A-Z]+) answered ([0-9]{3})(?: [A-Za-z][A-Za-z '-]*[A-Za-z])?`)
	// An address's user and password, and what follows its path.
	reasonUserInfoPattern = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/@\s"']+@`)
	reasonQueryPattern    = regexp.MustCompile(`(://[^\s"'?]*)\?[^\s"']*`)
	reasonSpacePattern    = regexp.MustCompile(`\s+`)
)

// webdavAnsweredStatus is the status a WebDAV server answered that the
// store took for a failure, 0 for none.
func webdavAnsweredStatus(err error) int {
	if err == nil {
		return 0
	}
	if match := webdavAnsweredPattern.FindStringSubmatch(err.Error()); match != nil {
		status, _ := strconv.Atoi(match[2])
		return status
	}
	return 0
}

// s3ResponseOf is the S3 answer an error carries — its status, code and
// message (PocketBase's client keeps it in a ResponseError of its own
// package) — or nothing.
func s3ResponseOf(err error) (int, string, string) {
	queue := []error{err}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == nil {
			continue
		}
		value := reflect.ValueOf(current)
		if value.Kind() == reflect.Pointer && !value.IsNil() && value.Elem().Kind() == reflect.Struct &&
			value.Elem().Type().Name() == "ResponseError" {
			text := func(name string) string {
				if field := value.Elem().FieldByName(name); field.IsValid() && field.Kind() == reflect.String {
					return field.String()
				}
				return ""
			}
			status := 0
			if field := value.Elem().FieldByName("Status"); field.IsValid() && field.CanInt() {
				status = int(field.Int())
			}
			return status, text("Code"), text("Message")
		}
		switch wrapped := current.(type) {
		case interface{ Unwrap() []error }:
			queue = append(queue, wrapped.Unwrap()...)
		case interface{ Unwrap() error }:
			queue = append(queue, wrapped.Unwrap())
		}
	}
	return 0, "", ""
}

// backupFailureReason says why in the place's own words, at most a line:
// what a WebDAV server or S3 storage answered, the system's word for a
// folder or a connection, else the error's first line. An address keeps no
// user or password and nothing after its path; a secret given is never
// said.
func backupFailureReason(err error, secrets ...string) string {
	if err == nil {
		return ""
	}
	return cleanBackupFailureReason(strings.Join(backupFailureReasons(err), "; "), secrets...)
}

// backupFailureReasons are what each part of a failure says, leaving out
// Leafen's own names for it (a code, "the backup doesn't hold what its list
// says"), which the code says already, unless nothing else is said.
func backupFailureReasons(err error) []string {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		reasons := []string{}
		for _, part := range joined.Unwrap() {
			if part == nil || backupFailureOwnName(part) {
				continue
			}
			for _, reason := range backupFailureReasons(part) {
				if reason != "" && !slices.Contains(reasons, reason) {
					reasons = append(reasons, reason)
				}
			}
		}
		if len(reasons) > 0 {
			return reasons
		}
	}
	return []string{backupFailureReasonOf(err)}
}

// backupFailureOwnName is one of Leafen's own names for a failure.
func backupFailureOwnName(err error) bool {
	text := err.Error()
	return strings.HasPrefix(text, "LEAFEN_") || errors.Is(err, errRestoreAssemble) && text == errRestoreAssemble.Error()
}

func backupFailureReasonOf(err error) string {
	reason := ""
	if status, code, message := s3ResponseOf(err); status != 0 || code != "" {
		if code == "" {
			code = http.StatusText(status)
		}
		reason = strings.TrimSpace(fmt.Sprintf("S3 answered %d %s", status, code))
		if message != "" {
			reason += ": " + message
		}
	} else if match := webdavAnsweredPattern.FindString(err.Error()); match != "" {
		reason = match
	} else {
		switch {
		case errors.Is(err, errWebDAVBackupUnauthorized):
			reason = "the WebDAV server answered 401 or 403: it refused the user name or password"
		case errors.Is(err, errWebDAVBackupNoSpace):
			reason = "the WebDAV server answered 507 or 413: no room for the backup"
		default:
			var path *fs.PathError
			var address *url.Error
			switch {
			case errors.As(err, &address):
				reason = address.Op + " " + address.URL + ": " + address.Err.Error()
			case errors.As(err, &path):
				reason = path.Op + " " + path.Path + ": " + path.Err.Error()
			default:
				reason, _, _ = strings.Cut(err.Error(), "\n")
			}
		}
	}
	return reason
}

func cleanBackupFailureReason(reason string, secrets ...string) string {
	reason = reasonUserInfoPattern.ReplaceAllString(reason, "$1")
	reason = reasonQueryPattern.ReplaceAllString(reason, "$1")
	for _, secret := range secrets {
		if len(secret) >= 4 {
			reason = strings.ReplaceAll(reason, secret, "…")
		}
	}
	reason = strings.TrimSpace(reasonSpacePattern.ReplaceAllString(reason, " "))
	if runes := []rune(reason); len(runes) > 300 {
		reason = string(runes[:299]) + "…"
	}
	return reason
}

// logBackupFailure keeps a failure in the diagnostic log with its reason,
// as the status shows it.
func logBackupFailure(app core.App, code, operation, stage string, cause error, reason string) {
	logDurableJobDiagnosticAt(app, diagnosticSourcePosition(1), code, newDiagnosticReference(), "", operation, stage, "", cause,
		"causeReason", reason)
}

// scheduledBackupSecrets are the secrets of a place a reason never says.
func scheduledBackupSecrets(app core.App, schedule storedScheduledBackup) []string {
	secrets := []string{os.Getenv(scheduledBackupWebDAVPasswordEnv), os.Getenv(scheduledBackupS3SecretKeyEnv)}
	for _, sealed := range []string{schedule.WebDAVPassword, schedule.S3Secret} {
		if sealed == "" {
			continue
		}
		if opened, err := decryptAdministratorSetting(app, sealed); err == nil && opened != "" {
			secrets = append(secrets, opened)
		}
	}
	return secrets
}

// The common cause of what a place answered, one line beside the failure's
// code and reason, at this one boundary for every place and channel (user
// 2026-11-22: a pCloud WebDAV folder answered only 401 — its account wanted
// the new device confirmed from a mail — and the cause was found by reading
// that mail): what the answer's status commonly means, for the screen to say
// in its language, agents in words (backupFailureCauseSay) and the mail.
const (
	backupFailureCauseSignIn     = "signIn"
	backupFailureCausePermission = "permission"
	backupFailureCauseAddress    = "address"
	backupFailureCauseSpace      = "space"
)

// S3's codes for keys it doesn't take, which it answers with 403 as it does
// keys it takes without the permission.
var s3SignInRefusals = map[string]bool{"InvalidAccessKeyId": true, "SignatureDoesNotMatch": true, "InvalidToken": true,
	"ExpiredToken": true, "TokenRefreshRequired": true, "InvalidSecurity": true}

// backupFailureCause is the common cause of what the place answered: 401
// its sign-in, 403 its permission, a redirect its address, 507 its space;
// none where it answered nothing that says. S3 answers keys it doesn't take
// with 403 too, by their code, or with no code where the request had no
// body to answer in (HEAD): those are its sign-in.
func backupFailureCause(err error) string {
	if err == nil {
		return ""
	}
	s3Status, s3Code, _ := s3ResponseOf(err)
	status := s3Status
	if webdav := webdavAnsweredStatus(err); webdav != 0 {
		status = webdav
	}
	switch {
	// S3 says a full quota with 403 too.
	case status == 507, status == 413, s3Code == "EntityTooLarge", s3Code == "QuotaExceeded",
		errors.Is(err, errWebDAVBackupNoSpace):
		return backupFailureCauseSpace
	case status == 401, s3Status == 403 && (s3Code == "" || s3SignInRefusals[s3Code]):
		return backupFailureCauseSignIn
	case status == 403:
		return backupFailureCausePermission
	case status >= 300 && status < 400, s3Code == "PermanentRedirect", s3Code == "TemporaryRedirect",
		s3Code == "AuthorizationHeaderMalformed":
		return backupFailureCauseAddress
	case errors.Is(err, errWebDAVBackupUnauthorized), errors.Is(err, errS3BackupUnauthorized):
		return backupFailureCauseSignIn
	}
	return ""
}

// backupFailureCauseSay is a cause's line in English, or in Korean for a
// Korean mail, as the settings screen says it (serverConsole
// schedule.cause.*).
func backupFailureCauseSay(cause string, korean bool) string {
	lines := map[string][2]string{
		backupFailureCauseSignIn: {"Common cause: a wrong user name and password (or keys), or the storage asking to confirm this new device " +
			"(usually by email) or for an app password.",
			"흔한 원인: 사용자 이름·비밀번호(또는 키)가 틀렸거나, 저장소가 새 기기 확인(보통 메일로 옵니다)이나 앱 비밀번호를 요구합니다."},
		backupFailureCausePermission: {"Common cause: the account has no permission for that folder or bucket.",
			"흔한 원인: 그 계정에 그 폴더나 버킷의 권한이 없습니다."},
		backupFailureCauseAddress: {"Common cause: the address is missing the / at its end, or the storage wants its own region’s address.",
			"흔한 원인: 주소 끝의 /가 빠졌거나, 저장소가 자기 지역의 주소를 요구합니다."},
		backupFailureCauseSpace: {"Common cause: the storage, or the account’s quota there, is full.",
			"흔한 원인: 저장소나 그곳 계정의 할당량이 가득 찼습니다."},
	}
	line, ok := lines[cause]
	if !ok {
		return ""
	}
	if korean {
		return line[1]
	}
	return line[0]
}

// backupFailureCauseNote is what an agent reads beside an answer that
// names a cause (errorCause in a status, cause in a refusal): each one's
// line, once.
func backupFailureCauseNote(body any) string {
	said := []string{}
	var walk func(value any, depth int)
	walk = func(value any, depth int) {
		object, ok := value.(map[string]any)
		if !ok || depth > 4 {
			return
		}
		for name, field := range object {
			if cause, ok := field.(string); ok && (name == "errorCause" || name == "cause") {
				if line := backupFailureCauseSay(cause, false); line != "" && !slices.Contains(said, cause+": "+line) {
					said = append(said, cause+": "+line)
				}
				continue
			}
			walk(field, depth+1)
		}
	}
	walk(body, 0)
	slices.Sort(said)
	return strings.Join(said, " ")
}
