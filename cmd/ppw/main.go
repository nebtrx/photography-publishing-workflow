package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "ppw",
		Short: "Photography Publishing Workflow",
		Long:  "Automated pipeline: scan → validate → enrich → review → publish → archive.\n\nRun without a subcommand to launch the interactive TUI.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDefault()
		},
	}

	root.AddCommand(scanCmd())
	root.AddCommand(validateCmd())
	root.AddCommand(enrichCmd())
	root.AddCommand(scrapeCmd())
	root.AddCommand(reviewCmd())
	root.AddCommand(publishCmd())
	root.AddCommand(archiveCmd())
	root.AddCommand(scheduleCmd())
	root.AddCommand(pipelineCmd())
	root.AddCommand(watchCmd())
	root.AddCommand(metaCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
