package cli

import (
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use: "mdns",
	}

	rootCmd.AddCommand(NewClientCommand())
	rootCmd.AddCommand(NewServerCommand())

	return rootCmd
}
