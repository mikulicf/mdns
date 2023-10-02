package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mikulicf/mdns"
	"github.com/spf13/cobra"
)

func NewClientCommand() *cobra.Command {
	clientCommand := &cobra.Command{
		Use:   "client",
		Short: "start mdns client query",
		Run: func(_ *cobra.Command, _ []string) {
			entriesCh := make(chan *mdns.ServiceEntry, 4)

			go func() {
				for entry := range entriesCh {

					cleanedStr := strings.Trim(entry.InfoFields[0], "\"")
					cleanedStr = strings.Replace(cleanedStr, `\"`, `"`, -1)

					var info HostInfo
					err := json.Unmarshal([]byte(cleanedStr), &info)
					if err != nil {
						fmt.Println("Error unmarshalling:", err)
						return
					}

					fmt.Printf("Hostname: %s, IP: %s\n", info.Hostname, info.IP)

				}
			}()

			mdns.Query(&mdns.QueryParam{Service: "_foobar._tcp", Domain: "", Timeout: time.Second * 5, DisableIPv6: true, Entries: entriesCh})
			close(entriesCh)
		},
	}
	return clientCommand
}
