package cli

import (
	"log"

	"github.com/mikulicf/mdns"
	"github.com/spf13/cobra"
)

func NewIfaceCommand() *cobra.Command {
	ifaceCommand := &cobra.Command{
		Use:   "iface",
		Short: "get default iface",
		Run: func(_ *cobra.Command, _ []string) {
			iface, err := mdns.GetDefaultInterface()
			if err != nil {
				log.Println(err)
				return
			}
			log.Println(iface)
		},
	}
	return ifaceCommand
}
