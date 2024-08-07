package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	rhttp "github.com/hashicorp/go-retryablehttp"
	"github.com/ndrewnee/go-yamusic/yamusic"
	"gopkg.in/yaml.v3"
)

// List of internal errors.
var (
	errTrackNotAvailable = errors.New("track not available")
	errTrackExists       = errors.New("track already exists")
)

// pause is a time for sleep between track downloads.
const pause = 3 * time.Second

// YandexClient is a facade for exporting a list of tracks using YandexClient.Music API.
type YandexClient struct {
	api          *yamusic.Client
	http         *rhttp.Client
	favid        int
	tracksDir    string
	playlistsDir string
}

// Playlist is a set of tracks.
type Playlist struct {
	ID     int     `yaml:"id"`
	Name   string  `yaml:"name"`
	Tracks []Track `yaml:"tracks"`
}

// Track is a short information about a track.
type Track struct {
	ID      int      `yaml:"id"`
	Title   string   `yaml:"title"`
	Artists []string `yaml:"artists"`
}

// String returns string representation of the track.
func (t Track) String() string {
	artists := t.Artists
	if len(artists) > 3 {
		artists = artists[:3]
	}
	s := fmt.Sprintf("%s - %s", strings.Join(artists, ", "), t.Title)
	s = strings.ReplaceAll(s, "/", "-")
	return s
}

// InitClient creates new Yandex client and fetches current user profile.
func InitClient(ctx context.Context, conf Config) (*YandexClient, error) {
	client := yamusic.NewClient(yamusic.AccessToken(0, conf.Token))

	// Set user id to avoid passing it to each method
	status, _, err := client.Account().GetStatus(ctx) //nolint:bodyclose
	if err != nil {
		return nil, fmt.Errorf("get account status: %w", err)
	}
	client.SetUserID(status.Result.Account.UID)

	ya := &YandexClient{
		api:          client,
		http:         rhttp.NewClient(),
		favid:        conf.FavID,
		tracksDir:    conf.TracksDir,
		playlistsDir: conf.PlaylistsDir,
	}
	ya.http.RetryMax = 5
	ya.http.RetryWaitMin = 10 * time.Second
	ya.http.Logger = nil

	return ya, nil
}

// Download downloads all playlists YAML files, and all tracks as MP3 files.
func (c *YandexClient) Download(ctx context.Context) error {
	log.Print("Get playlists")
	playlists, err := c.getPlaylists(ctx)
	if err != nil {
		return fmt.Errorf("get playlists: %w", err)
	}

	log.Print("Save playlists to files")
	for _, p := range playlists {
		file := path.Join(c.playlistsDir, strings.ToLower(p.Name)+".yaml")
		if err := c.savePlaylist(p, file); err != nil {
			return fmt.Errorf("save playlist '%s': %w", p.Name, err)
		}
	}

	log.Print("Download tracks")
	if err := c.downloadTracks(ctx, playlists); err != nil {
		return fmt.Errorf("download tracks: %w", err)
	}

	return nil
}

func (c *YandexClient) getPlaylists(ctx context.Context) ([]Playlist, error) {
	// Get user's custom playlists
	resp, _, err := c.api.Playlists().List(ctx, 0) //nolint:bodyclose
	if err != nil {
		return nil, fmt.Errorf("list playlists: %w", err)
	}
	if resp.Error.Name != "" {
		return nil, fmt.Errorf(
			"get playlists: %s: %s",
			resp.Error.Name, resp.Error.Message,
		)
	}

	// Add user's favorites
	resp.Result = append(resp.Result, yamusic.PlaylistsResult{
		Kind:  c.favid,
		Title: "Favorites",
	})

	// Convert playlists to local format
	playlists := make([]Playlist, len(resp.Result))
	for i, r := range resp.Result {
		log.Printf("Playlist: %s", r.Title)
		p, err := c.getPlaylist(ctx, r.Kind)
		if err != nil {
			return nil, fmt.Errorf("get playlist '%s': %w", r.Title, err)
		}
		playlists[i] = p
	}

	return playlists, nil
}

func (c *YandexClient) getPlaylist(ctx context.Context, kind int) (Playlist, error) {
	resp, _, err := c.api.Playlists().Get(ctx, 0, kind) //nolint:bodyclose
	if err != nil {
		return Playlist{}, fmt.Errorf("get playlist: %w", err)
	}
	if resp.Error.Name != "" {
		return Playlist{}, fmt.Errorf(
			"get playlist %d: %s: %s",
			kind, resp.Error.Name, resp.Error.Message,
		)
	}

	playlist := Playlist{
		ID:     kind,
		Name:   resp.Result.Title,
		Tracks: make([]Track, len(resp.Result.Tracks)),
	}
	for i, track := range resp.Result.Tracks {
		artists := make([]string, 0, len(track.Track.Artists))
		for _, a := range track.Track.Artists {
			artists = append(artists, a.Name)
		}
		id, err := strconv.Atoi(track.Track.ID)
		if err != nil {
			return Playlist{}, fmt.Errorf("invalid track id: %s", track.Track.ID)
		}
		playlist.Tracks[i] = Track{
			ID:      id,
			Title:   track.Track.Title,
			Artists: artists,
		}
	}

	return playlist, nil
}

func (c *YandexClient) savePlaylist(p Playlist, file string) error {
	data, err := yaml.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal playlist: %w", err)
	}
	if err := os.WriteFile(file, data, 0o644); err != nil {
		return fmt.Errorf("save playlist to file: %w", err)
	}
	return nil
}

func (c *YandexClient) downloadTracks(ctx context.Context, playlists []Playlist) error {
	for _, playlist := range playlists {
		log.Printf("Playlist: %s", playlist.Name)
		for _, track := range playlist.Tracks {
			if ctx.Err() != nil {
				return nil
			}

			log.Printf("Track: %s", track.String())

			file := path.Join(c.tracksDir, track.String()+".mp3")
			err := c.downloadTrack(ctx, track, file)
			switch {
			case err == nil:
				log.Print("Downloaded")
			case errors.Is(err, errTrackNotAvailable):
				log.Print("Unavailable")
			case errors.Is(err, errTrackExists):
				log.Print("Skip")
			case errors.Is(err, context.Canceled):
				os.Remove(file) //nolint:errcheck,gosec
				return err
			default:
				return fmt.Errorf("download track '%s': %w", track.String(), err)
			}

			time.Sleep(pause)
		}
	}
	return nil
}

func (c *YandexClient) downloadTrack(ctx context.Context, track Track, file string) error {
	url, err := c.api.Tracks().GetDownloadURL(ctx, track.ID)
	if err != nil {
		return errTrackNotAvailable
	}

	req, err := rhttp.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("get data: %w", err)
	}
	defer resp.Body.Close()

	f, err := os.OpenFile(file, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec
	if os.IsExist(err) {
		return errTrackExists
	}
	defer f.Close() //nolint:errcheck

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("save to file: %w", err)
	}
	return nil
}
