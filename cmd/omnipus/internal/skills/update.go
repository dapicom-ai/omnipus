package skills

import (
	"github.com/spf13/cobra"

	"github.com/dapicom-ai/omnipus/pkg/skills"
)

func newUpdateCommand(installerFn func() (*skills.SkillInstaller, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "update <name>",
		Short: "Update an installed skill to the latest version",
		Example: `
omnipus skills update aws-cost-analyzer
`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			installer, err := installerFn()
			if err != nil {
				return err
			}
			return skillsUpdateCmd(installer, args[0])
		},
	}
}
