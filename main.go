package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	tunnel "github.com/davidspek/go-iap-tunnel/pkg"
)

var (
	// these will be set by the goreleaser configuration
	// to appropriate values for the compiled binary.
	version string = "dev"

	// goreleaser can pass other information to the main package, such as the specific commit
	// https://goreleaser.com/cookbooks/using-main.version/
)

func main() {
	// CLI flags for TunnelTarget and listener
	var (
		project     = flag.String("project", "", "GCP project ID (required)")
		zone        = flag.String("zone", "", "GCP zone (required)")
		instance    = flag.String("instance", "", "GCE instance name (required)")
		iface       = flag.String("interface", "nic0", "Network interface (default: nic0)")
		port        = flag.Int("port", 22, "Remote port on the instance (default: 22)")
		network     = flag.String("network", "", "VPC network (optional)")
		region      = flag.String("region", "", "Region (optional)")
		host        = flag.String("host", "", "Host (optional)")
		destGroup   = flag.String("dest-group", "", "Destination group (optional)")
		urlOverride = flag.String("url-override", "", "Tunnel URL override (optional)")
		listenAddr  = flag.String("listen", "localhost:2222", "Local address to listen on (default: localhost:2222)")
		logLevel    = flag.String("log-level", "info", "Log level: debug, info, warn, error")
		versionFlag = flag.Bool("version", false, "Show version and exit")
	)
	flag.Parse()
	if *versionFlag {
		fmt.Println(version)
		os.Exit(0)
	}

	// Set up slog logger with chosen level
	var level slog.Level
	switch *logLevel {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		fmt.Fprintf(os.Stderr, "Unknown log level: %s\n", *logLevel)
		os.Exit(2)
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	logger := slog.New(handler)
	slog.SetDefault(logger)
	tunnel.SetLogger(logger)

	// Validate required flags
	if *project == "" || *zone == "" || *instance == "" {
		logger.Error("Missing required flags: --project, --zone, and --instance are required")
		flag.Usage()
		os.Exit(2)
	}

	target := tunnel.TunnelTarget{
		Project:     *project,
		Zone:        *zone,
		Instance:    *instance,
		Interface:   *iface,
		Port:        *port,
		Network:     *network,
		Region:      *region,
		Host:        *host,
		DestGroup:   *destGroup,
		URLOverride: *urlOverride,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle SIGINT/SIGTERM for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("Shutting down...")
		cancel()
	}()

	manager := tunnel.NewTunnelManager(target, nil)

	lis, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		logger.Error("Failed to listen", "address", *listenAddr, "err", err)
		os.Exit(1)
	}
	defer func() {
		if err := lis.Close(); err != nil {
			logger.Error("Failed to close listener", "err", err)
		}
	}()
	logger.Info("Tunnel listening", "listen_addr", *listenAddr)

	logger.Info("Listening and tunneling via IAP",
		"listen_addr", *listenAddr,
		"instance", *instance,
		"port", *port,
	)

	go func() {
		if err := manager.Serve(ctx, lis); err != nil {
			logger.Error("Tunnel serve error", "err", err)
		}
	}()

	// Block until context is cancelled
	<-ctx.Done()
	logger.Info("Tunnel server exited")
}
