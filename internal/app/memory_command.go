package app

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const (
	memoryLimitEnvironment       = "LEAFEN_MEMORY_LIMIT"
	errorCodeMemoryPolicyInvalid = "LEAFEN_MEMORY_POLICY_INVALID"
	defaultHostMemoryInformation = "/proc/meminfo"
)

func memoryLimitCommand() *cobra.Command {
	var printJSON bool
	command := &cobra.Command{
		Use:   "memory-limit",
		Short: "Resolve the finite self-host container memory limit",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			source, err := os.ReadFile(defaultHostMemoryInformation)
			if err != nil && os.Getenv(memoryLimitEnvironment) == "" {
				return fmt.Errorf("%s: read host memory: %w", errorCodeMemoryPolicyInvalid, err)
			}
			totalMemoryBytes, _ := totalMemoryBytesFromMeminfo(string(source))
			policy, err := resolveSelfHostMemoryPolicy(totalMemoryBytes, os.Getenv(memoryLimitEnvironment))
			if err != nil {
				return fmt.Errorf("%s: %w", errorCodeMemoryPolicyInvalid, err)
			}
			if printJSON {
				return json.NewEncoder(command.OutOrStdout()).Encode(policy)
			}
			_, err = fmt.Fprintln(command.OutOrStdout(), policy.DockerValue)
			return err
		},
	}
	command.Flags().BoolVar(&printJSON, "json", false, "print the resolved policy as JSON")
	return command
}
