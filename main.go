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

// Command line arguments.
var (
	configFile  = flag.String("config", "config.yaml", "Configuration file")
	showVersion = flag.Bool("version", false, "Show version")
	debug       = flag.Bool("debug", false, "Show debug logs")
)

func main() {
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
		logFatal("Failed to read config: %v", err)
	}

	logInfo("Init client")
	client, err := InitClient(ctx, conf)
	if err != nil {
		logFatal("Failed to init client: %v", err)
	}

	logInfo("Start downloading")
	if err = client.Download(ctx); err != nil {
		logFatal("Failed to download playlists: %v", err)
	}
	logInfo("Done")
}

func logFatal(format string, args ...any) {
	log.Fatalf(format, args...)
}

func logInfo(format string, args ...any) {
	log.Printf(format, args...)
}

func logDebug(format string, args ...any) {
	if *debug {
		log.Printf(format, args...)
	}
}
