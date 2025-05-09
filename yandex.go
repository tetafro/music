package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	id3 "github.com/bogem/id3v2/v2"
	rhttp "github.com/hashicorp/go-retryablehttp"
	"github.com/ndrewnee/go-yamusic/yamusic"
	"gopkg.in/yaml.v3"
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
	ID        int      `yaml:"id"`
	Title     string   `yaml:"title"`
	Artists   []string `yaml:"artists"`
	Available bool     `yaml:"-"`
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
	logInfo("Get playlists")
	playlists, err := c.getPlaylists(ctx)
	if err != nil {
		return fmt.Errorf("get playlists: %w", err)
	}

	logInfo("Save playlists to files")
	for _, p := range playlists {
		file := path.Join(c.playlistsDir, strings.ToLower(p.Name)+".yaml")
		if err := c.savePlaylist(p, file); err != nil {
			return fmt.Errorf("save playlist '%s': %w", p.Name, err)
		}
	}
	logInfo("Downloaded %d playlists", len(playlists))

	logInfo("Download tracks")
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
			ID:        id,
			Title:     track.Track.Title,
			Artists:   artists,
			Available: track.Track.Available,
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

//nolint:cyclop
func (c *YandexClient) downloadTracks(ctx context.Context, playlists []Playlist) error {
	// Read existing tracks once to avoid checking if a file exist on every
	// iteration of the loop
	existingTracks, err := listFiles(c.tracksDir)
	if err != nil {
		return fmt.Errorf("list files: %w", err)
	}

	for _, playlist := range playlists {
		logInfo("Playlist: %s (%d tracks)", playlist.Name, len(playlist.Tracks))
		var downloaded, skipped, unavailable int
		for _, track := range playlist.Tracks {
			if ctx.Err() != nil {
				return nil
			}

			file := path.Join(c.tracksDir, track.String()+".mp3")
			if _, ok := existingTracks[file]; ok {
				logDebug("Skipped: %s", track.String())
				skipped++
				continue
			}

			if !track.Available {
				logDebug("Unavailable: %s", track.String())
				unavailable++
				continue
			}

			t := time.Now()
			err := c.downloadTrack(ctx, track, file)
			switch {
			case err == nil:
				logInfo("Downloaded: %s (%s)",
					track.String(),
					time.Since(t).Truncate(100*time.Millisecond).String())
				downloaded++
			case errors.Is(err, context.Canceled):
				return err
			default:
				return fmt.Errorf("download track '%s': %w", track.String(), err)
			}

			if err := setTags(track, file); err != nil {
				return fmt.Errorf("set tags for '%s': %w", track.String(), err)
			}

			time.Sleep(pause)
		}
		logInfo("Downloaded %d, skipped %d, unavailable %d",
			downloaded, skipped, unavailable)
	}
	return nil
}

func (c *YandexClient) downloadTrack(ctx context.Context, track Track, file string) error {
	url, err := c.api.Tracks().GetDownloadURL(ctx, track.ID)
	if err != nil {
		return fmt.Errorf("get track URL: %w", err)
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

	dir := filepath.Dir(file)
	tmp, err := os.CreateTemp(dir, "tmp-*")
	if err != nil {
		return fmt.Errorf("create tmp file : %w", err)
	}

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return fmt.Errorf("save to file: %w", err)
	}
	tmp.Close() //nolint:errcheck,gosec

	if err = os.Rename(tmp.Name(), file); err != nil {
		return fmt.Errorf("move tmp file to %s: %w", file, err)
	}
	return nil
}

func setTags(track Track, file string) error {
	tag, err := id3.Open(file, id3.Options{Parse: true})
	if err != nil {
		return fmt.Errorf("open file '%s': %w", file, err)
	}
	defer tag.Close() //nolint:errcheck

	tag.SetArtist(strings.Join(track.Artists, ", "))
	tag.SetTitle(track.Title)

	if err := tag.Save(); err != nil {
		return fmt.Errorf("save tags: %w", err)
	}
	return nil
}

func listFiles(dir string) (map[string]struct{}, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	result := make(map[string]struct{}, len(files))
	for _, f := range files {
		result[filepath.Join(dir, f.Name())] = struct{}{}
	}
	return result, nil
}
