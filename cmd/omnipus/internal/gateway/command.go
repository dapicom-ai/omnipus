//go:build !cgo

package gateway

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dapicom-ai/omnipus/cmd/omnipus/internal"
	"github.com/dapicom-ai/omnipus/pkg/gateway"
	"github.com/dapicom-ai/omnipus/pkg/logger"
	"github.com/dapicom-ai/omnipus/pkg/sandbox"
	"github.com/dapicom-ai/omnipus/pkg/utils"
)

func NewGatewayCommand() *cobra.Command {
	var debug bool
	var noTruncate bool
	var allowEmpty bool
	var sandboxMode string

	cmd := &cobra.Command{
		Use:     "gateway",
		Aliases: []string{"g"},
		Short:   "Start omnipus gateway",
		Long: "Start omnipus gateway.\n\n" +
			"Exit codes:\n" +
			"  0   clean shutdown\n" +
			"  1   generic boot failure (credential/config/provider error)\n" +
			"  2   usage error (invalid flag value)\n" +
			"  78  sandbox apply/install failure on a capable kernel (EX_CONFIG)\n",
		Args: cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if noTruncate && !debug {
				return fmt.Errorf("the --no-truncate option can only be used in conjunction with --debug (-d)")
			}
			if noTruncate {
				utils.SetDisableTruncation(true)
				logger.Info("String truncation is globally disabled via 'no-truncate' flag")
			}
			// Validate --sandbox up front so typos (--sandbox=of) exit
			// with code 2 (usage error) before any boot logic runs.
			// FR-J-006 second sentence.
			if sandboxMode != "" {
				if _, err := sandbox.ParseMode(sandboxMode); err != nil {
					// Cobra maps PreRunE errors to exit 1 by default.
					// We want exit 2 (usage error) for bad flag values,
					// which cobra reserves for argument parse errors via
					// its SilenceErrors/SilenceUsage + explicit os.Exit.
					fmt.Fprintln(os.Stderr, "Error:", err)
					os.Exit(2)
				}
			}
			return nil
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			runErr := gateway.RunWithOptions(gateway.RunOptions{
				Debug:             debug,
				HomePath:          internal.GetOmnipusHome(),
				ConfigPath:        internal.GetConfigPath(),
				AllowEmptyStartup: allowEmpty,
				SandboxMode:       sandboxMode,
			})
			if runErr == nil {
				return nil
			}
			// FR-J-004: sandbox apply/install failure on a capable kernel
			// must exit 78 (EX_CONFIG) rather than the generic 1 used by
			// the top-level main.go. Distinguish via a sentinel error.
			var sbErr *gateway.SandboxBootError
			if errors.As(runErr, &sbErr) {
				fmt.Fprintln(os.Stderr, "Error:", runErr)
				os.Exit(gateway.ExitSandboxConfig)
			}
			return runErr
		},
	}

	cmd.Flags().BoolVarP(&debug, "debug", "d", false, "Enable debug logging")
	cmd.Flags().BoolVarP(&noTruncate, "no-truncate", "T", false, "Disable string truncation in debug logs")
	cmd.Flags().BoolVarP(
		&allowEmpty,
		"allow-empty",
		"E",
		false,
		"Continue starting even when no default model is configured",
	)
	cmd.Flags().StringVar(
		&sandboxMode,
		"sandbox",
		"",
		"Sandbox mode: enforce (default on Linux 5.13+), permissive (audit-only), off (disabled). "+
			"Overrides the gateway.sandbox.mode config value.",
	)

	return cmd
}
