package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"

	"github.com/foundry/registry/internal/adapters/auth"
	"github.com/foundry/registry/internal/adapters/metadata"
	"github.com/foundry/registry/internal/adapters/storage"
	"github.com/foundry/registry/internal/api/handlers"
	"github.com/foundry/registry/internal/config"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	logger := zerolog.New(os.Stdout).With().Timestamp().Str("service", "foundry-registry").Logger()

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to load config")
	}

	// Initialize blob storage.
	blobs, err := storage.NewDiskBlobStorage(cfg.Storage.DataDir)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to initialize blob storage")
	}

	// Initialize metadata store.
	meta, err := metadata.NewSQLiteStore(cfg.Storage.DataDir)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to initialize metadata store")
	}
	defer meta.Close()

	// Initialize authenticator.
	authenticator := auth.NewTokenAuth(cfg.Auth.Tokens)

	// Initialize HTTP handlers.
	handler := handlers.New(blobs, meta, authenticator, logger)

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: handler.Router(),
	}

	// Graceful shutdown.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		logger.Info().Msg("shutting down server")
		srv.Close()
	}()

	logger.Info().Str("addr", addr).Msg("starting Foundry Registry server")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatal().Err(err).Msg("server error")
	}
}
