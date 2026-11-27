package app

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

const (
	errorCodeMCPStdioConfig = "LEAFEN_MCP_STDIO_CONFIG"
	errorCodeMCPStdioBridge = "LEAFEN_MCP_STDIO_BRIDGE"
	maxMCPMessageBytes      = 4 << 20
)

func mcpCommand() *cobra.Command {
	confirmPublicDestination := false
	command := &cobra.Command{
		Use:          "mcp",
		Short:        "Run the stdio bridge to a Leafen server",
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			return runMCPStdioBridge(
				command.Context(), command.InOrStdin(), command.OutOrStdout(),
				strings.TrimSpace(os.Getenv("LEAFEN_MCP_CONNECTION_URI")),
				confirmPublicDestination,
			)
		},
	}
	command.AddCommand(mcpCallCommand())
	command.AddCommand(mcpSecretCommand())
	command.Flags().BoolVar(
		&confirmPublicDestination,
		"confirm-public-destination",
		false,
		"confirm the public HTTPS destination carried by LEAFEN_MCP_CONNECTION_URI",
	)
	return command
}

func runMCPStdioBridge(
	ctx context.Context,
	input io.Reader,
	output io.Writer,
	connectionURI string,
	confirmPublicDestination bool,
) error {
	endpoint, token, err := parseMCPConnectionURI(connectionURI, confirmPublicDestination)
	if err != nil {
		return err
	}
	client := newMCPStdioHTTPClient()
	sessionID := ""
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), maxMCPMessageBytes)
	for scanner.Scan() {
		message := bytes.TrimSpace(scanner.Bytes())
		if len(message) == 0 {
			continue
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(message))
		if err != nil {
			return fmt.Errorf("%s: %w", errorCodeMCPStdioBridge, err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		if sessionID != "" {
			request.Header.Set("Mcp-Session-Id", sessionID)
		}
		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("%s: %w", errorCodeMCPStdioBridge, err)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, maxMCPMessageBytes+1))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || len(body) > maxMCPMessageBytes {
			return fmt.Errorf("%s: response body", errorCodeMCPStdioBridge)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("%s: status %d", errorCodeMCPStdioBridge, response.StatusCode)
		}
		if value := strings.TrimSpace(response.Header.Get("Mcp-Session-Id")); value != "" {
			sessionID = value
		}
		body = bytes.TrimSpace(body)
		if len(body) > 0 {
			if _, err := output.Write(append(body, '\n')); err != nil {
				return fmt.Errorf("%s: %w", errorCodeMCPStdioBridge, err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("%s: %w", errorCodeMCPStdioBridge, err)
	}
	return nil
}
