package main

import (
	"log"

	"github.com/mikulicf/mdns/cli"
)

func main() {
	cmd := cli.NewRootCommand()
	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
