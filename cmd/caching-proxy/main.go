package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"caching-proxy/internal/cache"
	"caching-proxy/internal/cli"
	"caching-proxy/internal/proxy"
	"caching-proxy/internal/version"
)

func main() {
	opts, err := cli.Parse(os.Args[1:])
	if err != nil {
		// flag package already wrote error to stderr
		os.Exit(2)
	}

	if opts.ShowVersion {
		fmt.Println(version.Version)
		return
	}

	if opts.ClearCache {
		// Perform clear-cache and exit
		dc, err := cache.NewDiskCache(opts.CacheDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		removed, err := dc.Clear()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("cache cleared: %d entries removed\n", removed)
		return
	}

	// For now, just confirm parsed options; server implementation follows in next steps
	fmt.Printf("starting caching-proxy on :%d forwarding to %s (cache-dir=%s)\n", opts.Port, opts.Origin, opts.CacheDir)

	// Context canceled on SIGINT/SIGTERM for graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Initialize disk cache
	dc, err := cache.NewDiskCache(opts.CacheDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	debug := opts.LogLevel == "debug"
	if err := proxy.Run(ctx, opts.Port, opts.Origin, dc, debug); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
