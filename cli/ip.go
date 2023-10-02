package cli

import (
	"log"

	"github.com/mikulicf/mdns"
	"github.com/spf13/cobra"
)

func NewIpCommand() *cobra.Command {
	ipCommand := &cobra.Command{
		Use:   "ip",
		Short: "get available ip addresses",
		Run: func(_ *cobra.Command, _ []string) {
			addr, err := mdns.GetIPv4Addresses()
			if err != nil {
				log.Println(err)
				return
			}
			log.Println(addr)
		},
	}
	return ipCommand
}
