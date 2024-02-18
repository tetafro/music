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

func main() {
	token := flag.String("token", "", "Yandex.Music auth token")
	favid := flag.Int("fav", 3, "'Favourites' playlist id")
	playlistsDir := flag.String("playlists", "playlists", "Directory for playlist files")
	tracksDir := flag.String("tracks", "tracks", "Directory for track files")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()

	log.Print("Init client")
	client, err := InitClient(
		ctx, *token, *favid,
		*playlistsDir, *tracksDir,
	)
	if err != nil {
		fatalf("Failed to init client: %v", err)
	}

	log.Print("Start downloading")
	if err = client.Download(ctx); err != nil {
		fatalf("Failed to get playlists: %v", err)
	}
	log.Print("Done")
}

func fatalf(format string, args ...interface{}) {
	fmt.Printf(format+"\n", args...)
	os.Exit(1)
}
