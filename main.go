// vikunja-companion executes jobs against a vikunja server.
//
// Configuration is loaded from config.yaml in the current directory and values can be overridden
// by setting environment variables. See [getDefaultEnvConfigEntries] for the configuration format
// and supported environment variable overrides.
package main

import (
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
	slog.Info("starting")
	config, err := loadConfig("", []envConfigEntry{})
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	requestTimeout, err := parseDuration(config.vikunja.requestTimeout, time.Duration(30)*time.Second)
	if err != nil {
		return fmt.Errorf("invalid request timeout: %w", err)
	}
	client := vikunja.NewClient(
		vikunja.WithBaseURL(config.vikunja.baseURL),
		vikunja.WithAPIToken(config.vikunja.apiToken),
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
	for _, job := range config.jobs {
		cron, err := cron.ParseStandard(job.schedule)
		if err != nil {
			return fmt.Errorf("parsing job '%s' schedule: %w", job.name, err)
		}
		nextRun := cron.Next(lastRun)
		if !nextRun.IsZero() && !nextRun.After(now) {
			slog.Info("running ready job", slog.String("job", job.name))
			i := slices.IndexFunc(jobHandlers, func(h jobHandler) bool { return h.canHandleJob(&job) })
			if i == -1 {
				return fmt.Errorf("no handler found for job type '%s'", job.jobType)
			}
			err = jobHandlers[i].handleJob(&job)
			if err != nil {
				return fmt.Errorf("running job '%s': %w", job.name, err)
			}
			slog.Info("job completed", slog.String("job", job.name))
		}
	}

	slog.Info("execution complete")

	return nil
}

func getLastRunTime(config *config, now time.Time) (time.Time, error) {
	runInterval, err := parseDuration(config.runInterval, 0)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid run internal: %w", err)
	}
	if runInterval == 0 {
		return time.Time{}, fmt.Errorf("run interval must be set to a positive duration")
	}
	return now.Add(runInterval * -1), nil
}
