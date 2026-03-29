// Omnipus - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 Omnipus contributors

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dapicom-ai/omnipus/cmd/omnipus/internal"
	"github.com/dapicom-ai/omnipus/cmd/omnipus/internal/agent"
	"github.com/dapicom-ai/omnipus/cmd/omnipus/internal/auth"
	"github.com/dapicom-ai/omnipus/cmd/omnipus/internal/cron"
	"github.com/dapicom-ai/omnipus/cmd/omnipus/internal/gateway"
	"github.com/dapicom-ai/omnipus/cmd/omnipus/internal/migrate"
	"github.com/dapicom-ai/omnipus/cmd/omnipus/internal/model"
	"github.com/dapicom-ai/omnipus/cmd/omnipus/internal/onboard"
	"github.com/dapicom-ai/omnipus/cmd/omnipus/internal/skills"
	"github.com/dapicom-ai/omnipus/cmd/omnipus/internal/status"
	"github.com/dapicom-ai/omnipus/cmd/omnipus/internal/version"
	"github.com/dapicom-ai/omnipus/pkg/config"
)

func NewOmnipusCommand() *cobra.Command {
	short := fmt.Sprintf("%s omnipus - Personal AI Assistant v%s\n\n", internal.Logo, config.GetVersion())

	cmd := &cobra.Command{
		Use:     "omnipus",
		Short:   short,
		Example: "omnipus version",
	}

	cmd.AddCommand(
		onboard.NewOnboardCommand(),
		agent.NewAgentCommand(),
		auth.NewAuthCommand(),
		gateway.NewGatewayCommand(),
		status.NewStatusCommand(),
		cron.NewCronCommand(),
		migrate.NewMigrateCommand(),
		skills.NewSkillsCommand(),
		model.NewModelCommand(),
		version.NewVersionCommand(),
	)

	return cmd
}

const (
	colorBlue = "\033[1;38;2;62;93;185m"
	colorRed  = "\033[1;38;2;213;70;70m"
	banner    = "\r\n" +
		colorBlue + "██████╗ ██╗ ██████╗ ██████╗ " + colorRed + " ██████╗██╗      █████╗ ██╗    ██╗\n" +
		colorBlue + "██╔══██╗██║██╔════╝██╔═══██╗" + colorRed + "██╔════╝██║     ██╔══██╗██║    ██║\n" +
		colorBlue + "██████╔╝██║██║     ██║   ██║" + colorRed + "██║     ██║     ███████║██║ █╗ ██║\n" +
		colorBlue + "██╔═══╝ ██║██║     ██║   ██║" + colorRed + "██║     ██║     ██╔══██║██║███╗██║\n" +
		colorBlue + "██║     ██║╚██████╗╚██████╔╝" + colorRed + "╚██████╗███████╗██║  ██║╚███╔███╔╝\n" +
		colorBlue + "╚═╝     ╚═╝ ╚═════╝ ╚═════╝ " + colorRed + " ╚═════╝╚══════╝╚═╝  ╚═╝ ╚══╝╚══╝\n " +
		"\033[0m\r\n"
)

func main() {
	fmt.Printf("%s", banner)
	cmd := NewOmnipusCommand()
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
