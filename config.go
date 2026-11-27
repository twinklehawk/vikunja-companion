package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"go.yaml.in/yaml/v4"
)

// config is the configuration for this command.
type config struct {
	// vikunja is the configuration for accessing the vikunja API.
	vikunja vikunjaClientConfig
	// jobs is the configuration for the jobs to run.
	jobs []jobConfig
	// runInterval is how often this command runs as a duration formatted as described in
	// [time.ParseDuration].
	runInterval string
}

// vikunjaClientConfig contains configuration for accessing the vikunja API.
type vikunjaClientConfig struct {
	baseURL  string
	apiToken string
	// requestTimeout is the maximum time allowed for a request to complete formatted as described
	// in [time.ParseDuration].
	requestTimeout string
}

// jobConfig contains configuration for running a single job.
type jobConfig struct {
	name    string
	jobType string
	// schedule is when the job should run as a standard crontab string.
	// The time zone can be specified using a TZ=<time zone> or CRON_TZ=<time zone> prefix. If no
	// time zone is specified this way, the system time zone is used.
	schedule   string
	parameters map[string]string
}

// envValueHandler updates a value in the passed in [config] instance using the
// passed in environment variable value.
type envValueHandler func(value string, config *config)

// envConfigEntry contains an environment variable key and a handler to update the config with the
// env value.
type envConfigEntry struct {
	// env is the name of the environment variable.
	env string
	// handler is a function to update a [config] instance using the environment variable value
	// referenced by [envConfigEntry.env] if the environment variable is set.
	handler envValueHandler
}

const (
	defaultConfigFile = "./config.yaml"
)

// LoadConfig loads configuration into a [config] instance.
// The configuration is first loaded from a YAML file, then values are overridden from environment variables.
//
// If configFile is empty, the [defaultConfigFile] is used. If envEntries is empty,
// [getDefaultEnvConfigEntries] is used.
func loadConfig(configFile string, envEntries []envConfigEntry) (*config, error) {
	if configFile == "" {
		configFile = defaultConfigFile
	}
	if len(envEntries) == 0 {
		envEntries = getDefaultEnvConfigEntries()
	}

	config, err := loadConfigFromFile(configFile)
	if err != nil {
		return nil, err
	}
	overrideConfigFromEnv(config, envEntries)
	return config, nil
}

func loadConfigFromFile(configFile string) (*config, error) {
	slog.Debug("reading config file" + configFile)
	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", configFile, err)
	}
	c := config{}
	err = yaml.Unmarshal(data, &c)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", configFile, err)
	}
	return &c, nil
}

func overrideConfigFromEnv(config *config, envEntries []envConfigEntry) {
	for _, entry := range envEntries {
		val, isSet := os.LookupEnv(entry.env)
		if isSet {
			slog.Debug("overriding value for " + entry.env + " from env")
			entry.handler(val, config)
		}
	}
}

func getDefaultEnvConfigEntries() []envConfigEntry {
	return []envConfigEntry{
		{
			env:     "VIKUNJA_API_TOKEN",
			handler: func(s string, c *config) { c.vikunja.apiToken = s },
		},
		{
			env:     "VIKUNJA_BASE_URL",
			handler: func(s string, c *config) { c.vikunja.baseURL = s },
		},
		{
			env:     "VIKUNJA_REQUEST_TIMEOUT",
			handler: func(s string, c *config) { c.vikunja.requestTimeout = s },
		},
	}
}

// parseDuration parses a string matching the format in [time.ParseDuration] into a [time.Duration].
// If value is empty or 0, defaultValue is returned.
func parseDuration(value string, defaultValue time.Duration) (time.Duration, error) {
	if value == "" {
		return defaultValue, nil
	}

	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parsing duration: %w", err)
	}
	if d == 0 {
		return defaultValue, nil
	}
	return d, nil
}
