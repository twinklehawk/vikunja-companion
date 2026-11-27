// vikunja-companion executes jobs against a vikunja server.
//
// Configuration is loaded from config.yaml in the current directory and values can be overridden
// by setting environment variables. See [getDefaultEnvConfigEntries] for the configuration format
// and supported environment variable overrides.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/twinklehawk/vikunja-client-go"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("run failed", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	slog.Info("starting")
	config, err := loadConfig("", []envConfigEntry{})
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	requestTimeout, err := parseDuration(
		config.Vikunja.RequestTimeout,
		time.Duration(30)*time.Second,
	)
	if err != nil {
		return fmt.Errorf("invalid request timeout: %w", err)
	}
	client := vikunja.NewClient(
		vikunja.WithBaseURL(config.Vikunja.BaseURL),
		vikunja.WithAPIToken(config.Vikunja.APIToken),
		vikunja.WithHTTPClient(&http.Client{Timeout: requestTimeout}),
	)
	jobHandlers := []jobHandler{
		&createTaskHandler{client: client},
		&applyTodayLabelHandler{client: client},
	}

	now := time.Now()
	lastRun, err := getLastRunTime(config, now)
	if err != nil {
		return fmt.Errorf("determining last run time: %w", err)
	}

	slog.Info("running jobs")
	for _, job := range config.Jobs {
		cron, err := cron.ParseStandard(job.Schedule)
		if err != nil {
			return fmt.Errorf("parsing job '%s' schedule: %w", job.Name, err)
		}
		nextRun := cron.Next(lastRun)
		if !nextRun.IsZero() && !nextRun.After(now) {
			slog.Info("running ready job", slog.String("job", job.Name))
			i := slices.IndexFunc(
				jobHandlers,
				func(h jobHandler) bool { return h.canHandleJob(&job) },
			)
			if i == -1 {
				return fmt.Errorf("no handler found for job type '%s'", job.JobType)
			}
			err = jobHandlers[i].handleJob(ctx, &job)
			if err != nil {
				return fmt.Errorf("running job '%s': %w", job.Name, err)
			}
			slog.Info("job completed", slog.String("job", job.Name))
		}
	}

	slog.Info("execution complete")

	return nil
}

func getLastRunTime(config *Config, now time.Time) (time.Time, error) {
	runInterval, err := parseDuration(config.RunInterval, 0)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid run internal: %w", err)
	}
	if runInterval == 0 {
		return time.Time{}, fmt.Errorf("run interval must be set to a positive duration")
	}
	return now.Add(runInterval * -1), nil
}
