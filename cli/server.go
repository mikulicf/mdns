package cli

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mikulicf/mdns"
	"github.com/spf13/cobra"
)

type HostInfo struct {
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
}

func NewServerCommand() *cobra.Command {
	timeout := 0
	interfaces := make([]string, 0)
	domain := "local."
	service := "_foobar._tcp"
	port := 8080
	ipv6 := false

	serverCommand := &cobra.Command{
		Use:   "server",
		Short: "start mdns server",
		Run: func(_ *cobra.Command, _ []string) {
			host, err := os.Hostname()
			if err != nil {
				log.Println(err)
				return
			}
			addr, err := mdns.GetIPv4Addresses()
			if err != nil {
				log.Println(err)
				return
			}

			info := HostInfo{
				Hostname: host,
				IP:       addr[0],
			}

			data, err := json.Marshal(info)
			if err != nil {
				fmt.Println("Error:", err)
				return
			}

			service, err := mdns.NewMDNSService(host, service, "", "", port, nil, []string{string(data)})
			if err != nil {
				log.Println(err)
				return
			}

			iface, err := mdns.GetDefaultInterface()
			if err != nil {
				log.Println(err)
				return
			}

			err = mdns.NewServerWithTicker(&mdns.Config{Zone: service, Ipv6: false, Iface: iface}, time.Minute*2)
			if err != nil {
				log.Println(err)
				return
			}

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			<-sigCh
		},
	}

	serverCommand.Flags().StringVarP(&domain, "domain", "d", domain, "mDNS domain")
	serverCommand.Flags().StringVarP(&service, "service", "s", service, "service name")
	serverCommand.Flags().IntVarP(&port, "port", "p", port, "service port")
	serverCommand.Flags().StringArrayVarP(&interfaces, "interface", "i", interfaces, "interface")
	serverCommand.Flags().IntVarP(&timeout, "timeout", "t", timeout, "timeout in seconds")
	serverCommand.Flags().BoolVar(&ipv6, "ipv6", ipv6, "enable ipv6")

	return serverCommand
}
