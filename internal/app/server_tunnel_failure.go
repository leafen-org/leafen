package app

// Why the server's outside way isn't open: its kind, which the screens say
// in the person's own language with what they can do, and the words it came
// with — cloudflared's or Tailscale's own, or the server's — which the
// screens keep folded under "자세히" for whoever looks into it (2026-11-25:
// Settings › Server › Remote access showed them, in English, as its line;
// web/src/components/remoteAccess/tunnelFailureLine.ts).
type tunnelFailure struct {
	kind string
	text string
}

const (
	// cloudflared or Tailscale couldn't be found, fetched or started.
	tunnelFailureProgram = "program"
	// The tunnels' door on this server couldn't be opened (server_tunnel_door.go).
	tunnelFailureDoor = "door"
	// The program ran and stopped, or never connected: often a network that
	// blocks it.
	tunnelFailureStopped = "stopped"
	// The administrator's own tunnel's token can't be read: it is entered again.
	tunnelFailureToken = "token"
	// Tailscale's public address didn't open, or couldn't be kept.
	tunnelFailurePublicAddress = "public-address"
	// A certificate for the address came from elsewhere: the address stays
	// off until an administrator says it is theirs (server_address_ct.go).
	tunnelFailureCertificateElsewhere = "certificate-elsewhere"
	// The administrator's own address: its certificate can't be read, or
	// HTTPS on it stopped (server_address_tls.go).
	tunnelFailureOwnAddress = "own-address"
	// Tailscale isn't built into this server (a phone's).
	tunnelFailureNoTailscale = "no-tailscale"
	// Remote access is held back here, not failing (replica_remote_access.go):
	// the data moved to another server, which answers at the address; a
	// paired primary waits to confirm it still holds its data; a primary
	// that can't reach its replica let the address go to its standby.
	tunnelHeldMoved              = "held-moved"
	tunnelHeldWaiting            = "held-waiting"
	tunnelHeldReplicaUnreachable = "held-replica-unreachable"
)

// noTunnelFailure is an outside way that is fine, or was just turned off.
var noTunnelFailure = tunnelFailure{}

func failedTunnel(kind, text string) tunnelFailure {
	return tunnelFailure{kind: kind, text: text}
}

func (failure tunnelFailure) empty() bool {
	return failure.text == "" && failure.kind == ""
}
