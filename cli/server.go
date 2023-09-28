package cli

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mikulicf/mdns/pkg/mdns"
	"github.com/spf13/cobra"
)

type HostInfo struct {
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
}

func NewServerCommand() *cobra.Command {
	serverCommand := &cobra.Command{
		Use:   "server",
		Short: "start mdns server",
		Run: func(_ *cobra.Command, _ []string) {
			host, _ := os.Hostname()
			addr, _ := mdns.GetIPv4Addresses()

			info := HostInfo{
				Hostname: host,
				IP:       addr[0],
			}

			data, err := json.Marshal(info)
			if err != nil {
				fmt.Println("Error:", err)
				return
			}
			service, _ := mdns.NewMDNSService(host, "_foobar._tcp", "", "", 8000, nil, []string{string(data)})

			err = mdns.NewServerWithTicker(&mdns.Config{Zone: service}, time.Minute*2)
			if err != nil {
				log.Println(err)
				return
			}

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			<-sigCh
		},
	}

	return serverCommand
}
