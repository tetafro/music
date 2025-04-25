// Backup script for online music services.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	configFile := flag.String("config", "config.yaml", "Configuration file")
	flag.Parse()

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
