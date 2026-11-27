package app

import (
	"context"
	"errors"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// leafen_remote_access lets an agent of the server's administrator see how
// remote access stands, turn on the built-in Tailscale (answering where the
// person signs in and allows it), and make an https address the server's own
// once the agent and the person made it reach the server — for an install
// whose screen hasn't been opened. Only an admin connection of a server
// administrator may use it.

type leafenMCPRemoteAccessInput struct {
	Action string `json:"action,omitempty" jsonschema:"status (the default) to see how remote access stands; tailscale to give the server a free public address with the person's own Tailscale account (answers where they log in and allow it); set_address, once you and the person have made an https address reach this server, to make it the server's address (with certificate and key when the server should take HTTPS itself) — the server checks it."`
	// For set_address.
	Address     string `json:"address,omitempty" jsonschema:"For set_address: the https address, such as https://wiki.example.com."`
	Certificate string `json:"certificate,omitempty" jsonschema:"For set_address: the address's certificate chain (PEM) when the server takes HTTPS itself; leave it out when your proxy does."`
	Key         string `json:"key,omitempty" jsonschema:"For set_address: the certificate's private key (PEM)."`
	Port        int    `json:"port,omitzero" jsonschema:"For set_address with a certificate: the port the server takes HTTPS on, 443 by default."`
}

type leafenMCPRemoteAccessOutput struct {
	Mode  string `json:"mode"`
	State string `json:"state"`
	// What remote access waits for now, as the screen says it (server_tunnel_stage.go).
	Stage   string `json:"stage"`
	Address string `json:"address,omitempty"`
	// Off: the outside addresses the server was opened at all the same.
	ReachedFrom []serverReachedFrom `json:"reachedFrom,omitempty"`
	// Tailscale: where the person signs in and allows the public address
	// while the server waits, and the address once it's up.
	AddressCheck     *serverAddressCheck `json:"addressCheck,omitempty"`
	TailscaleLogin   string              `json:"tailscaleLogin,omitempty"`
	TailscaleAllow   string              `json:"tailscaleAllow,omitempty"`
	TailscaleAddress string              `json:"tailscaleAddress,omitempty"`
	Instructions     string              `json:"instructions"`
}

func registerMCPRemoteAccessTool(server *mcp.Server, app core.App) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_remote_access", Title: "See and set remote access",
		Description: "For the server's administrator, on an agent connection as all the server's administration is (not an MCP token): how " +
			"remote access stands; the built-in Tailscale's free public address, with the addresses " +
			"to show the person to log in and allow it; or an https address you and the person made reach this server, which the server then checks.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPRemoteAccessInput) (*mcp.CallToolResult, leafenMCPRemoteAccessOutput, error) {
		authority, err := mcpAuthorityFromContext(ctx)
		if err != nil {
			return nil, leafenMCPRemoteAccessOutput{}, err
		}
		administrator, err := isAdministrator(app, authority.owner())
		if err != nil || !administrator || authority.Scope < userKeyScopeAdmin {
			return nil, leafenMCPRemoteAccessOutput{}, errors.New("remote access is for the server's administrator, on a connection whose tools and organizations leave it everything")
		}
		return leafenMCPRemoteAccess(ctx, app, input)
	})
}

func leafenMCPRemoteAccess(ctx context.Context, app core.App, input leafenMCPRemoteAccessInput) (*mcp.CallToolResult, leafenMCPRemoteAccessOutput, error) {
	settings, err := loadServerTunnelSettings(app)
	if err != nil {
		return nil, leafenMCPRemoteAccessOutput{}, err
	}
	if input.Action == "tailscale" {
		return leafenMCPTailscale(ctx, app)
	}
	if input.Action == "set_address" {
		return leafenMCPSetAddress(ctx, app, input)
	}
	status := serverTunnelOf(app).status(settings)
	if status.Mode == serverTunnelTailscale {
		return nil, tailscaleMCPOutput(status), nil
	}
	output := leafenMCPRemoteAccessOutput{Mode: status.Mode, State: status.State, Stage: status.Stage, Address: status.URL, AddressCheck: status.AddressCheck,
		ReachedFrom: status.ReachedFrom, Instructions: "Remote access is set in " + mcpScreen(mcpCallScreenLanguage(ctx, app), "settings", "server", "serverRemoteAccess") + "."}
	if status.Mode == serverTunnelOff {
		output.Instructions = remoteAccessOffInstructions(status.ReachedFrom) + " " + output.Instructions
	}
	// The address agents anywhere are given, as the screens show it.
	published := configuredPublicOrigin()
	if published == "" {
		published = status.URL
	}
	output.Instructions = withClaudeConnectorPortNote(output.Instructions, published)
	return nil, output, nil
}

// withClaudeConnectorPortNote adds to an answer what a Claude connector
// can't do at the address, if anything (connector_reach.go).
func withClaudeConnectorPortNote(instructions, address string) string {
	if note := claudeConnectorPortNote(address); note != "" {
		return instructions + " " + note
	}
	return instructions
}

// leafenMCPTailscale turns remote access to Tailscale and answers, within a
// few seconds, where the person signs in or allows the public address.
func leafenMCPTailscale(ctx context.Context, app core.App) (*mcp.CallToolResult, leafenMCPRemoteAccessOutput, error) {
	if !tailscaleBuiltIn {
		return nil, leafenMCPRemoteAccessOutput{Instructions: "Tailscale isn't built into Leafen on phones; use it on an always-on server."}, nil
	}
	tunnel := serverTunnelOf(app)
	settings, err := loadServerTunnelSettings(app)
	if err != nil {
		return nil, leafenMCPRemoteAccessOutput{}, err
	}
	if settings.Mode != serverTunnelTailscale {
		settings.Mode, settings.Updated = serverTunnelTailscale, time.Now().UnixMilli()
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_server_tunnel}} ([[id]], [[mode]], [[updated]]) VALUES ('tunnel', 'tailscale', {:updated})
			ON CONFLICT ([[id]]) DO UPDATE SET [[mode]] = 'tailscale', [[updated]] = excluded.[[updated]]
		`).Bind(dbx.Params{"updated": settings.Updated}).Execute(); err != nil {
			return nil, leafenMCPRemoteAccessOutput{}, err
		}
		_ = tunnel.apply(settings)
	}
	status := tunnel.status(settings)
	for wait := 0; wait < 20 && status.TailscaleLogin == "" && status.TailscaleAllow == "" && status.State != serverTunnelStateOn; wait++ {
		select {
		case <-ctx.Done():
			return nil, tailscaleMCPOutput(status), nil
		case <-time.After(500 * time.Millisecond):
		}
		status = tunnel.status(settings)
	}
	return nil, tailscaleMCPOutput(status), nil
}

// tailscaleSeenByStatus is how the agent learns a Tailscale step is done:
// the server notices it, so the agent asks the server rather than the person.
const tailscaleSeenByStatus = "Don't wait for them to tell you: call this tool with status every few seconds, or read GET " +
	"/admin/tunnel through leafen_call with wait_seconds, which answers at the next stage — the server notices each step by itself — " +
	"and go on from what it answers."

func tailscaleMCPOutput(status serverTunnelStatus) leafenMCPRemoteAccessOutput {
	output := leafenMCPRemoteAccessOutput{Mode: status.Mode, State: status.State, Stage: status.Stage, Address: status.URL,
		TailscaleLogin: status.TailscaleLogin, TailscaleAllow: status.TailscaleAllow, TailscaleAddress: status.TailscaleAddress}
	switch {
	case status.State == serverTunnelStateOn:
		output.Instructions = "The server answers at its Tailscale address. Connections pass Tailscale but are decrypted on this server."
	case status.TailscaleLogin != "":
		// The agent opens Tailscale's pages itself and goes through them
		// (agent_outside_steps.go; 2026-11-22, the user: "mcp로 모든게 다
		// 되야되" — the answer had left a browserless agent asking them to
		// sign in and connect the server): one yes covers adding the server
		// and its public address, the next step's allowing; the person's
		// hand is signing in alone, said as that one step; and the agent
		// sees it done through status, the server noticing it, never
		// waiting to be told.
		output.Instructions = "Open tailscaleLogin " + agentInTheirBrowser + " — any browser works, their phone's too. " +
			"With a browser tool, left to do first: one question for a yes, in their language, carrying that this server joins their Tailscale " +
			"network and gets a public address (HTTPS and Funnel, the step after this one); then the one step left them is logging in to Tailscale " +
			"in your tab (or making an account there): a line carries that it is that one step, the page's buttons quoted as it names them, with " +
			"the place marked on a picture of it where you can, and everything after it you press, such as connecting this server. Without one, " +
			"the page you opened in their browser leaves them that same one step, then Tailscale's own button to connect this server — a line " +
			"carries just that, the same way. " +
			tailscaleSeenByStatus
	case status.TailscaleAllow != "":
		output.Instructions = "Open tailscaleAllow the same way: with a browser tool, allow this server a public address (HTTPS and Funnel) there " +
			"yourself once they've said yes to it (ask once if you haven't yet); without one, the page you opened in their browser leaves them " +
			"one step, its allow button, which you name as the page does. " + tailscaleSeenByStatus
	case status.Failure != "":
		output.Instructions = "Tailscale hasn't come up: " + status.Failure
	case status.Stage == tunnelStageCertificate:
		output.Instructions = "The public address is allowed; the server is getting its certificate from Tailscale. " + tailscaleSeenByStatus
	case status.Stage == tunnelStagePublishing:
		output.Instructions = "The address is open and its name is spreading through the public DNS, which takes a minute or two; " +
			"it is on once the name answers everywhere. " + tailscaleSeenByStatus
	default:
		output.Instructions = "Tailscale is starting. " + tailscaleSeenByStatus
	}
	return output
}

// leafenMCPSetAddress makes an https address the server's own — the last
// step an agent takes with the person when Tailscale can't be used — and
// answers what the server found there.
func leafenMCPSetAddress(ctx context.Context, app core.App, input leafenMCPRemoteAccessInput) (*mcp.CallToolResult, leafenMCPRemoteAccessOutput, error) {
	settings, err := setOwnAddress(app, input.Address, input.Certificate, input.Key, input.Port)
	if err != nil {
		return nil, leafenMCPRemoteAccessOutput{Instructions: "The address wasn't set: " + err.Error()}, nil
	}
	tunnel := serverTunnelOf(app)
	_ = tunnel.apply(settings)
	status := tunnel.status(settings)
	for wait := 0; wait < 30 && status.AddressCheck == nil; wait++ {
		select {
		case <-ctx.Done():
			wait = 30
		case <-time.After(300 * time.Millisecond):
		}
		status = tunnel.status(settings)
	}
	output := leafenMCPRemoteAccessOutput{Mode: status.Mode, State: status.State, Address: settings.Address, AddressCheck: status.AddressCheck,
		Instructions: "The address is the server's now; it opens from outside, as on their phone (Leafen shows a QR). Left to do: that to the person."}
	if status.Failure != "" {
		output.Instructions = "The address is set, but the server couldn't take HTTPS: " + status.Failure
	} else if status.AddressCheck != nil && status.AddressCheck.Problem != "" {
		output.Instructions = "The address is set, but the server's check found a problem (" + status.AddressCheck.Problem +
			"): see addressCheck, fix it with the person, and set it again."
	}
	output.Instructions = withClaudeConnectorPortNote(output.Instructions, settings.Address)
	return nil, output, nil
}
