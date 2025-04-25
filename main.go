// Backup script for online music services.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

// Version is the current git tag, injected on build.
var Version = "devel"

func main() {
	configFile := flag.String("config", "config.yaml", "Configuration file")
	showVersion := flag.Bool("version", false, "Show version")
	flag.Parse()

	if *showVersion {
		fmt.Println(Version)
		return
	}

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()

	conf, err := ReadConfig(*configFile)
	if err != nil {
		log.Fatalf("Failed to read config: %v", err)
	}

	log.Print("Init client")
	client, err := InitClient(ctx, conf)
	if err != nil {
		log.Fatalf("Failed to init client: %v", err)
	}

	log.Print("Start downloading")
	if err = client.Download(ctx); err != nil {
		log.Fatalf("Failed to download playlists: %v", err)
	}
	log.Print("Done")
}
