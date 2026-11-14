// Command seabird-archipelago-backend relays an Archipelago room into a
// seabird channel. It is configured entirely through environment variables;
// see the README.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/belak/x/slogx"
	"github.com/joho/godotenv"

	"github.com/seabird-chat/seabird-archipelago-backend/internal/archipelago"
	"github.com/seabird-chat/seabird-archipelago-backend/internal/backend"
)

func main() {
	// A .env file is optional.
	_ = godotenv.Load()

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	var format slogx.Format
	if err := format.Set(envDefault("LOG_FORMAT", "json")); err != nil {
		return fmt.Errorf("LOG_FORMAT: %w", err)
	}
	var level slogx.Level
	if err := level.Set(envDefault("LOG_LEVEL", "info")); err != nil {
		return fmt.Errorf("LOG_LEVEL: %w", err)
	}
	logger := slogx.New(format, level)

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	cfg.Logger = logger

	b, err := backend.New(cfg)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("starting", slogx.String("seabird_id", cfg.SeabirdID), slogx.String("slot", cfg.Slot))
	return b.Run(ctx)
}

func loadConfig() (backend.Config, error) {
	var errs []error
	required := func(key string) string {
		v := os.Getenv(key)
		if v == "" {
			errs = append(errs, fmt.Errorf("%s is required", key))
		}
		return v
	}

	cfg := backend.Config{
		SeabirdHost:   required("SEABIRD_HOST"),
		SeabirdToken:  required("SEABIRD_TOKEN"),
		SeabirdID:     envDefault("SEABIRD_ID", "archipelago"),
		URL:           os.Getenv("ARCHIPELAGO_URL"),
		Slot:          required("ARCHIPELAGO_SLOT"),
		Password:      os.Getenv("ARCHIPELAGO_PASSWORD"),
		Channel:       envDefault("ARCHIPELAGO_CHANNEL", "room"),
		CommandPrefix: envDefault("ARCHIPELAGO_COMMAND_PREFIX", "."),
	}

	if strings.HasPrefix(cfg.CommandPrefix, "!") {
		errs = append(errs, errors.New("ARCHIPELAGO_COMMAND_PREFIX cannot start with ! (reserved for archipelago server commands)"))
	}

	if room := os.Getenv("ARCHIPELAGO_ROOM"); room != "" {
		r, err := archipelago.ParseRoom(room)
		if err != nil {
			errs = append(errs, fmt.Errorf("ARCHIPELAGO_ROOM: %w", err))
		}
		cfg.Room = &r
	} else if cfg.URL == "" {
		errs = append(errs, errors.New("one of ARCHIPELAGO_ROOM or ARCHIPELAGO_URL is required"))
	}

	items, err := backend.ParseItemFilter(envDefault("ARCHIPELAGO_ITEM_FILTER", "progression,useful,filler,trap"))
	if err != nil {
		errs = append(errs, fmt.Errorf("ARCHIPELAGO_ITEM_FILTER: %w", err))
	}
	cfg.Items = items

	deathLink, err := strconv.ParseBool(envDefault("ARCHIPELAGO_DEATHLINK", "true"))
	if err != nil {
		errs = append(errs, fmt.Errorf("ARCHIPELAGO_DEATHLINK: %w", err))
	}
	cfg.DeathLink = deathLink

	return cfg, errors.Join(errs...)
}

func envDefault(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
