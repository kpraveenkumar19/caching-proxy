package cli

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

type Options struct {
	Port        int
	Origin      string
	CacheDir    string
	ShowVersion bool
	ClearCache  bool
	LogLevel    string
}

func DefaultCacheDir() string {
	// Prefer OS user cache directory; fall back to HOME/.cache
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, "caching-proxy")
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		return ".cache/caching-proxy"
	}
	return filepath.Join(home, ".cache", "caching-proxy")
}

func Parse(args []string) (Options, error) {
	fs := flag.NewFlagSet("caching-proxy", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var opts Options
	fs.IntVar(&opts.Port, "port", 3000, "Port for the caching proxy to listen on")
	fs.StringVar(&opts.Origin, "origin", "", "Origin server base URL (e.g. http://example.com)")
	fs.StringVar(&opts.CacheDir, "cache-dir", DefaultCacheDir(), "Directory for on-disk cache")
	fs.BoolVar(&opts.ShowVersion, "version", false, "Print version and exit")
	fs.BoolVar(&opts.ClearCache, "clear-cache", false, "Clear cache directory and exit")
	fs.StringVar(&opts.LogLevel, "log-level", "info", "Log level: info|debug")

	if err := fs.Parse(args); err != nil {
		return Options{}, err
	}

	// If just printing version or clearing cache, skip full validation below.
	if opts.ShowVersion {
		return opts, nil
	}
	if opts.ClearCache {
		// Allow user to override cache dir via flag; nothing else required here.
		return opts, nil
	}

	if err := Validate(opts); err != nil {
		return Options{}, err
	}
	return opts, nil
}

func Validate(opts Options) error {
	if opts.Port <= 0 || opts.Port > 65535 {
		return fmt.Errorf("invalid --port: %d", opts.Port)
	}
	if opts.Origin == "" {
		return errors.New("--origin is required")
	}
	_, err := url.Parse(opts.Origin)
	if err != nil {
		return fmt.Errorf("invalid --origin: %w", err)
	}
	switch opts.LogLevel {
	case "info", "debug":
	default:
		return fmt.Errorf("invalid --log-level: %s (expected info|debug)", opts.LogLevel)
	}
	return nil
}
