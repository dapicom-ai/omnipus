// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 Omnipus contributors

// Package onboard provides the `omnipus onboard` CLI stub. This is a
// minimal stub so cmd/omnipus compiles; onboarding is handled primarily
// through the gateway REST endpoint (pkg/onboarding.Manager) and the
// frontend /onboarding route.
package onboard

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewOnboardCommand returns the `omnipus onboard` Cobra command.
// Current implementation prints a pointer to the web UI since
// onboarding is driven from the browser.
func NewOnboardCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "onboard",
		Short: "Open the onboarding wizard",
		Long:  "Omnipus onboarding runs in the web UI. Start the gateway and visit http://<host>:<port>/onboarding.",
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Println("Omnipus onboarding runs in the web UI.")
			fmt.Println("Start the gateway with `omnipus gateway` and visit /onboarding in your browser.")
			return nil
		},
	}
}
