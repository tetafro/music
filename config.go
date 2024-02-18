package main

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents application configuration.
type Config struct {
	Token        string `yaml:"token"`
	FavID        int    `yaml:"fav_id"`
	PlaylistsDir string `yaml:"playlists_dir"`
	TracksDir    string `yaml:"tracks_dir"`
}

// ReadConfig returns configuration populated from the config file.
func ReadConfig(file string) (Config, error) {
	data, err := os.ReadFile(file) //nolint:gosec
	if err != nil {
		return Config{}, fmt.Errorf("read file: %w", err)
	}
	var conf Config
	if err := yaml.Unmarshal(data, &conf); err != nil {
		return Config{}, fmt.Errorf("unmarshal file: %w", err)
	}

	// Validation
	if conf.Token == "" {
		return Config{}, errors.New("missing token")
	}
	if conf.FavID == 0 {
		return Config{}, errors.New("missing favorites playlist id")
	}
	if conf.PlaylistsDir == "" {
		conf.PlaylistsDir = "."
	}
	if conf.TracksDir == "" {
		conf.TracksDir = "."
	}

	return conf, nil
}
