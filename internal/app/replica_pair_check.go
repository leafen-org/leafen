package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Two of three (decided 2026-10-18, the standby's arbiter): the place is
// the pair's judge, and the two servers also ask each other directly, at the
// addresses the connection code and the standby's beat carry, signed with a
// secret only the pair holds. A primary pauses only when it can reach
// neither the place nor its standby, so the place failing alone doesn't stop
// it; a standby takes over only when it reads the place, the primary's beat
// stood still, and the primary doesn't answer directly or answers that it
// takes no changes. Two servers that can't reach each other directly judge
// by the place alone, as before.
//
// What each counts as the other heard keeps the two from ever both taking
// changes (2026-11-14), whichever way a check is lost: a primary counts its
// standby heard only when its own check came back answered — never at a
// standby's check arriving, whose answer may not reach the standby before
// it takes over — and a check of a primary taking changes says so, signed,
// so the standby that answers it doesn't take over for replicaTakeOverAfter.
// A primary goes on through the place's outage at most replicaPauseAfter past
// its last answered check, which went out at most the far timeout before, so
// past 18 seconds after the standby last answered it, short of the 30 the
// standby waits. Hearing its standby only keeps a primary from pausing: once
// paused, only its beat at the place, the replica still its own, starts it
// again, so a standby that took over meanwhile is never met by a primary
// going on.
//
// The place itself must show each server's marks to the other at once: a
// cloud sync folder shows them minutes late and makes conflicting copies,
// and a standby judging by it could take over a primary that is there. A
// pair refuses such a folder by its name, and joins only after a mark the
// standby writes reaches the primary and the primary's answer comes back in
// time.

const (
	replicaPairProbeName    = "pair-probe.json"
	replicaPairCheckPath    = "/replica/pair-check"
	replicaPairCheckTimeout = 1500 * time.Millisecond
	// replicaPairCheckFarTimeout is for the other server at an address on
	// the internet — a Tailscale Funnel, a tunnel, a domain — where one
	// request takes one to two seconds, more under load (2026-11-11: every
	// check of a primary at its Funnel address ran out at 1.5 seconds).
	replicaPairCheckFarTimeout = 8 * time.Second
	// replicaPairRecheckEvery: a direct check that went unanswered is asked
	// again this often, so the screen follows once the pair reach each other.
	replicaPairRecheckEvery  = 30 * time.Second
	replicaPairVisibleWithin = 12 * time.Second
	replicaPairRoleNone      = "none"
	replicaPairRolePrimary   = "primary"
	replicaPairRoleStandby   = "standby"
)

// replicaPairProbe is a joining standby's mark at the place.
type replicaPairProbe struct {
	Holder string `json:"holder"`
	Nonce  string `json:"nonce"`
	// Proof signs the nonce with the code's join token: the primary answers
	// only its newest code's.
	Proof string `json:"proof,omitempty"`
}

// replicaPairAnswer is a server's signed answer to a direct check.
type replicaPairAnswer struct {
	Holder  string `json:"holder"`
	Role    string `json:"role"`
	Serving bool   `json:"serving"`
	MAC     string `json:"mac"`
}

func randomHex(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func replicaPairMAC(secret string, parts ...string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(mac.Sum(nil))
}

func (answer replicaPairAnswer) signedWith(secret, nonce string) string {
	return replicaPairMAC(secret, "leafen-pair-answer", nonce, answer.Holder, answer.Role, strconv.FormatBool(answer.Serving))
}

// ensureReplicaPairSecret is a primary's pair secret, made the first time
// it gives a connection code.
// rotateReplicaJoinToken makes a primary's join token anew for a new
// connection code: the codes made before it no longer join.
func rotateReplicaJoinToken(dataDir string) (string, error) {
	holder, err := loadReplicaHolder(dataDir)
	if err != nil {
		return "", err
	}
	if holder.JoinToken, err = randomHex(32); err != nil {
		return "", err
	}
	return holder.JoinToken, saveReplicaHolder(dataDir, holder)
}

// replicaJoinTokenNow is the token a joining standby's mark must carry now;
// while the primary's file can't be read, one no code carries, so none joins.
func replicaJoinTokenNow(dataDir string) string {
	holder, err := loadReplicaHolder(dataDir)
	if err != nil {
		closed, _ := randomHex(32)
		return "closed:" + closed
	}
	return holder.JoinToken
}

// replicaJoinProofValid says whether a joining standby's mark carries the
// primary's newest code: any, while the primary has made none since this
// rule came (its codes before carry no token).
func replicaJoinProofValid(joinToken string, probe replicaPairProbe) bool {
	if joinToken == "" {
		return true
	}
	return probe.Proof != "" && hmac.Equal([]byte(probe.Proof), []byte(replicaPairMAC(joinToken, "leafen-pair-join", probe.Nonce)))
}

func ensureReplicaPairSecret(dataDir string) (string, error) {
	holder, err := loadReplicaHolder(dataDir)
	if err != nil {
		return "", err
	}
	if holder.PairSecret != "" {
		return holder.PairSecret, nil
	}
	if holder.PairSecret, err = randomHex(32); err != nil {
		return "", err
	}
	return holder.PairSecret, saveReplicaHolder(dataDir, holder)
}

// replicaPairServingClaim signs what a check says of its asker: "1" when a
// primary asking takes changes.
func replicaPairServingClaim(secret, nonce, serving string) string {
	return replicaPairMAC(secret, "leafen-pair-serving", nonce, serving)
}

// readReplicaPairCheck answers the pair's direct check: who this server is,
// whether it is the primary or the standby, and whether it takes changes.
// A request not signed with this server's pair secret finds nothing. A
// standby asked by a primary that says, signed, it takes changes notes it
// heard one (primaryServingHeard); a primary asked by its standby notes
// nothing — it counts its standby heard only by its own check answered.
func readReplicaPairCheck(event *core.RequestEvent) error {
	query := event.Request.URL.Query()
	nonce, signed := query.Get("nonce"), query.Get("mac")
	holder, err := loadReplicaHolder(event.App.DataDir())
	if err != nil || holder.PairSecret == "" || len(nonce) != 32 ||
		!hmac.Equal([]byte(signed), []byte(replicaPairMAC(holder.PairSecret, "leafen-pair-check", nonce))) {
		return event.NotFoundError("", nil)
	}
	answer := replicaPairAnswer{Holder: holder.Holder, Role: replicaPairRoleNone}
	stored, err := loadScheduledBackup(event.App)
	runner, ok := event.App.Store().Get(replicationRunnerStoreKey).(*replicationRunner)
	switch {
	case err != nil || !ok:
	case stored.StandbyEnabled:
		answer.Role = replicaPairRoleStandby
		if serving := query.Get("serving"); serving == "1" &&
			hmac.Equal([]byte(query.Get("smac")), []byte(replicaPairServingClaim(holder.PairSecret, nonce, serving))) {
			runner.notePrimaryServing(time.Now())
		}
	case replicationWanted(stored):
		answer.Role = replicaPairRolePrimary
		answer.Serving = readReplicaFence(event.App.DataDir()) == nil && !replicationPaused(event.App)
	}
	answer.MAC = answer.signedWith(holder.PairSecret, nonce)
	return event.JSON(http.StatusOK, answer)
}

var errReplicaPairUnanswered = errors.New("the other server didn't answer the pair's check")

// replicaPairClient keeps its connections between checks, so a check after
// the first skips a far address's handshakes; made at first use, after the
// network proxy is installed in the default transport.
var replicaPairClient = sync.OnceValue(func() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.IdleConnTimeout = 2 * time.Minute
	return &http.Client{Transport: transport}
})

// replicaPairCheckTimeoutFor is how long a check of the server at origin
// waits: a little on this computer or network, longer across the internet.
func replicaPairCheckTimeoutFor(origin string) time.Duration {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return replicaPairCheckFarTimeout
	}
	// This computer's and its network's hosts are the one table's (network_reach.go).
	if _, nearby := hostNamedInNetwork(parsed.Host); nearby {
		return replicaPairCheckTimeout
	}
	return replicaPairCheckFarTimeout
}

// replicaPairRecheckDue is whether the screen's direct check is to be asked:
// never yet, or unanswered a while ago.
func replicaPairRecheckDue(direct string, asked, now time.Time) bool {
	return direct == "" || (direct == replicaDirectUnreachable && now.Sub(asked) >= replicaPairRecheckEvery)
}

// checkReplicaPair asks the other server of the pair directly; holder, if
// known, is who must answer. serving is what a primary asking says of
// itself ("1" taking changes, "0" not), "" for a standby.
func checkReplicaPair(ctx context.Context, origin, secret, holder, serving string) (replicaPairAnswer, error) {
	answer := replicaPairAnswer{}
	if origin == "" || secret == "" {
		return answer, errReplicaPairUnanswered
	}
	nonce, err := randomHex(16)
	if err != nil {
		return answer, err
	}
	values := url.Values{"nonce": {nonce}, "mac": {replicaPairMAC(secret, "leafen-pair-check", nonce)}}
	if serving != "" {
		values.Set("serving", serving)
		values.Set("smac", replicaPairServingClaim(secret, nonce, serving))
	}
	target := strings.TrimRight(origin, "/") + apiRoot + replicaPairCheckPath + "?" + values.Encode()
	ctx, cancel := context.WithTimeout(ctx, replicaPairCheckTimeoutFor(origin))
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return answer, err
	}
	response, err := replicaPairClient().Do(request)
	if err != nil {
		return answer, errReplicaPairUnanswered
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK ||
		json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&answer) != nil ||
		!hmac.Equal([]byte(answer.MAC), []byte(answer.signedWith(secret, nonce))) ||
		(holder != "" && answer.Holder != holder) {
		return replicaPairAnswer{}, errReplicaPairUnanswered
	}
	return answer, nil
}

// askStandby is a primary whose beat is late asking its standby directly;
// it answers whether the standby answered.
func (session *replicationSession) askStandby(ctx context.Context) bool {
	return session.runner.askStandbyWith(ctx, session.pairSecretNow())
}

// lookAtStandby is the screen's check of whether the pair reach each other,
// asked beside the beat, so a far standby's slow answer never holds a beat
// back; one at a time.
func (session *replicationSession) lookAtStandby(ctx context.Context) {
	if !session.looking.CompareAndSwap(false, true) {
		return
	}
	secret := session.pairSecretNow()
	session.beating.Add(1)
	go func() {
		defer session.beating.Done()
		defer session.looking.Store(false)
		session.runner.askStandbyWith(ctx, secret)
	}()
}

// pairSecretNow is the secret the pair signs with; a code given after the
// session began made it since.
func (session *replicationSession) pairSecretNow() string {
	if session.pairSecret == "" {
		if holder, err := loadReplicaHolder(session.runner.app.DataDir()); err == nil {
			session.pairSecret = holder.PairSecret
		}
	}
	return session.pairSecret
}

func (runner *replicationRunner) askStandbyWith(ctx context.Context, secret string) bool {
	standby, _, _ := runner.lease.snapshot(time.Now())
	if standby == nil || standby.Origin == "" || secret == "" {
		runner.noteDirect(replicaDirectNone)
		return false
	}
	serving := "0"
	if readReplicaFence(runner.app.DataDir()) == nil && !replicationPaused(runner.app) {
		serving = "1"
	}
	answer, err := checkReplicaPair(ctx, standby.Origin, secret, standby.Holder, serving)
	if err != nil || answer.Role != replicaPairRoleStandby {
		runner.noteDirect(replicaDirectUnreachable)
		return false
	}
	runner.noteDirect(replicaDirectReached)
	return true
}

// askPrimary is a standby asking its primary directly: it answers whether
// the primary still takes changes, and so must not be taken over.
func (session *standbySession) askPrimary(ctx context.Context, fallbackOrigin string) bool {
	origin := firstNonEmpty(session.primary.Origin, fallbackOrigin)
	if origin == "" || session.pairSecret == "" {
		session.direct = replicaDirectNone
		return false
	}
	answer, err := checkReplicaPair(ctx, origin, session.pairSecret, session.primary.Holder, "")
	if err != nil {
		session.direct = replicaDirectUnreachable
		return false
	}
	session.direct = replicaDirectReached
	return answer.Role == replicaPairRolePrimary && answer.Serving
}

// notePrimaryServing keeps when a standby last answered its primary's check
// saying it takes changes.
func (runner *replicationRunner) notePrimaryServing(now time.Time) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	runner.primaryServing = now
}

// primaryServingHeard is whether the standby answered a check of its primary
// taking changes within replicaTakeOverAfter: that primary may go on, so it
// isn't taken over.
func (runner *replicationRunner) primaryServingHeard(now time.Time) bool {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return !runner.primaryServing.IsZero() && sinceBoth(runner.primaryServing, now) < replicaTakeOverAfter
}

// noteDirect keeps what a primary's last direct check found.
func (runner *replicationRunner) noteDirect(direct string) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	runner.direct = direct
}

func (runner *replicationRunner) directCheck() string {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return runner.direct
}

// syncFolderReplicaPlace is whether a folder is a cloud sync folder's, by
// the names their apps give them (the desktop app's folder chooser finds
// the same ones): Google Drive, iCloud Drive, Dropbox and OneDrive.
func syncFolderReplicaPlace(folder string) bool {
	parts := strings.FieldsFunc(filepath.ToSlash(strings.ReplaceAll(folder, `\`, "/")), func(r rune) bool { return r == '/' })
	for index, part := range parts {
		lower := strings.ToLower(part)
		switch {
		case lower == "cloudstorage" && index > 0 && strings.EqualFold(parts[index-1], "Library"),
			lower == "com~apple~clouddocs", lower == "icloud drive", lower == "iclouddrive",
			lower == "my drive", lower == "google drive",
			strings.HasPrefix(lower, "dropbox"), strings.HasPrefix(lower, "onedrive"):
			return true
		}
	}
	return false
}

var errReplicaPlaceSlow = errors.New("the backup place shows changes to the other server too late for a standby")

// errReplicaJoinRefused is a primary refusing a code it has made again since.
var errReplicaJoinRefused = errors.New("the primary has made a newer connection code")

// probeReplicaPlace is a joining standby's round trip through the place: its
// mark must reach the primary, and the primary's answer come back, in time.
func probeReplicaPlace(ctx context.Context, store scheduledBackupStore, holder, joinToken string) error {
	nonce, err := randomHex(16)
	if err != nil {
		return err
	}
	probe := replicaPairProbe{Holder: holder, Nonce: nonce}
	if joinToken != "" {
		probe.Proof = replicaPairMAC(joinToken, "leafen-pair-join", nonce)
	}
	if err := writeReplicaJSON(store, replicaPairProbeName, probe); err != nil {
		return err
	}
	defer func() { _ = store.RemoveAll(scheduledBackupStoreJoin(replicaRoot(), replicaPairProbeName)) }()
	deadline := time.Now().Add(replicaPairVisibleWithin)
	for time.Now().Before(deadline) {
		lease := replicaLease{}
		if present, err := readReplicaJSON(store, replicaLeaseName, &lease); err == nil && present && lease.Echo == nonce {
			return nil
		} else if err == nil && present && lease.Refused == nonce {
			return errReplicaJoinRefused
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return errReplicaPlaceSlow
}
