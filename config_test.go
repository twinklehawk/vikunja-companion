package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testConfigYAML = `
vikunja:
  base-url: https://vikunja.example.com
  api-token: file-token
  request-timeout: 10s
run-interval: 5m
jobs:
  - name: daily task
    job-type: CreateTask
    schedule: "0 9 * * *"
    parameters:
      title: Water plants
      project: Inbox
`

func writeConfigFile(t *testing.T, dir, contents string) string {
	t.Helper()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing config file: %v", err)
	}
	return path
}

func TestLoadConfigFromFile(t *testing.T) {
	path := writeConfigFile(t, t.TempDir(), testConfigYAML)

	got, err := loadConfigFromFile(path)
	if err != nil {
		t.Fatalf("loadConfigFromFile() unexpected error: %v", err)
	}

	want := &Config{
		Vikunja: VikunjaClientConfig{
			BaseURL:        "https://vikunja.example.com",
			APIToken:       "file-token",
			RequestTimeout: "10s",
		},
		RunInterval: "5m",
		Jobs: []JobConfig{
			{
				Name:     "daily task",
				JobType:  "CreateTask",
				Schedule: "0 9 * * *",
				Parameters: map[string]string{
					"title":   "Water plants",
					"project": "Inbox",
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loadConfigFromFile() = %+v, want %+v", got, want)
	}
}

func TestLoadConfigFromFileEmpty(t *testing.T) {
	path := writeConfigFile(t, t.TempDir(), "")

	got, err := loadConfigFromFile(path)
	if err != nil {
		t.Fatalf("loadConfigFromFile() unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, &Config{}) {
		t.Errorf("loadConfigFromFile() = %+v, want empty config", got)
	}
}

func TestLoadConfigFromFileMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	got, err := loadConfigFromFile(path)
	if err == nil {
		t.Fatalf("loadConfigFromFile() expected error, got config %+v", got)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not mention config file path %q", err, path)
	}
}

func TestLoadConfigFromFileInvalidYAML(t *testing.T) {
	path := writeConfigFile(t, t.TempDir(), "vikunja: [unclosed")

	got, err := loadConfigFromFile(path)
	if err == nil {
		t.Fatalf("loadConfigFromFile() expected error, got config %+v", got)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not mention config file path %q", err, path)
	}
}

func TestOverrideConfigFromEnv(t *testing.T) {
	t.Setenv("TEST_CONFIG_SET", "from-env")
	t.Setenv("TEST_CONFIG_SET_EMPTY", "")
	// Ensure the unset variable really is unset, restoring any prior value after the test.
	t.Setenv("TEST_CONFIG_UNSET", "")
	if err := os.Unsetenv("TEST_CONFIG_UNSET"); err != nil {
		t.Fatalf("unsetting env: %v", err)
	}

	var unsetCalled bool
	entries := []envConfigEntry{
		{
			env:     "TEST_CONFIG_SET",
			handler: func(s string, c *Config) { c.Vikunja.APIToken = s },
		},
		{
			env:     "TEST_CONFIG_SET_EMPTY",
			handler: func(s string, c *Config) { c.Vikunja.BaseURL = s },
		},
		{
			env:     "TEST_CONFIG_UNSET",
			handler: func(string, *Config) { unsetCalled = true },
		},
	}
	c := &Config{Vikunja: VikunjaClientConfig{BaseURL: "original", RequestTimeout: "1s"}}

	overrideConfigFromEnv(c, entries)

	if c.Vikunja.APIToken != "from-env" {
		t.Errorf("APIToken = %q, want %q", c.Vikunja.APIToken, "from-env")
	}
	if c.Vikunja.BaseURL != "" {
		t.Errorf("BaseURL = %q, want empty value from set-but-empty env var", c.Vikunja.BaseURL)
	}
	if c.Vikunja.RequestTimeout != "1s" {
		t.Errorf("RequestTimeout = %q, want unchanged %q", c.Vikunja.RequestTimeout, "1s")
	}
	if unsetCalled {
		t.Error("handler called for unset environment variable")
	}
}

func TestGetDefaultEnvConfigEntries(t *testing.T) {
	t.Setenv("VIKUNJA_API_TOKEN", "env-token")
	t.Setenv("VIKUNJA_BASE_URL", "https://env.example.com")
	t.Setenv("VIKUNJA_REQUEST_TIMEOUT", "45s")

	c := &Config{}
	overrideConfigFromEnv(c, getDefaultEnvConfigEntries())

	want := VikunjaClientConfig{
		BaseURL:        "https://env.example.com",
		APIToken:       "env-token",
		RequestTimeout: "45s",
	}
	if c.Vikunja != want {
		t.Errorf("vikunja config = %+v, want %+v", c.Vikunja, want)
	}
}

func TestLoadConfig(t *testing.T) {
	path := writeConfigFile(t, t.TempDir(), testConfigYAML)
	t.Setenv("TEST_LOAD_CONFIG_TOKEN", "env-token")
	entries := []envConfigEntry{
		{
			env:     "TEST_LOAD_CONFIG_TOKEN",
			handler: func(s string, c *Config) { c.Vikunja.APIToken = s },
		},
	}

	got, err := loadConfig(path, entries)
	if err != nil {
		t.Fatalf("loadConfig() unexpected error: %v", err)
	}

	if got.Vikunja.APIToken != "env-token" {
		t.Errorf("APIToken = %q, want env override %q", got.Vikunja.APIToken, "env-token")
	}
	if got.Vikunja.BaseURL != "https://vikunja.example.com" {
		t.Errorf("BaseURL = %q, want value from file", got.Vikunja.BaseURL)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, testConfigYAML)
	t.Chdir(dir)
	t.Setenv("VIKUNJA_API_TOKEN", "default-env-token")

	got, err := loadConfig("", nil)
	if err != nil {
		t.Fatalf("loadConfig() unexpected error: %v", err)
	}

	if got.Vikunja.APIToken != "default-env-token" {
		t.Errorf(
			"APIToken = %q, want override from default env entries %q",
			got.Vikunja.APIToken,
			"default-env-token",
		)
	}
	if got.RunInterval != "5m" {
		t.Errorf("RunInterval = %q, want value from default config file", got.RunInterval)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	got, err := loadConfig(filepath.Join(t.TempDir(), "missing.yaml"), nil)
	if err == nil {
		t.Fatalf("loadConfig() expected error, got config %+v", got)
	}
	if got != nil {
		t.Errorf("loadConfig() = %+v, want nil config on error", got)
	}
}

func TestParseDuration(t *testing.T) {
	const defaultValue = 30 * time.Second
	tests := []struct {
		name    string
		value   string
		want    time.Duration
		wantErr bool
	}{
		{name: "empty uses default", value: "", want: defaultValue},
		{name: "zero uses default", value: "0", want: defaultValue},
		{name: "zero with unit uses default", value: "0s", want: defaultValue},
		{name: "seconds", value: "10s", want: 10 * time.Second},
		{name: "minutes", value: "5m", want: 5 * time.Minute},
		{name: "compound", value: "1h30m", want: 90 * time.Minute},
		{name: "negative", value: "-5s", want: -5 * time.Second},
		{name: "missing unit", value: "10", wantErr: true},
		{name: "invalid", value: "abc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDuration(tt.value, defaultValue)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseDuration(%q) expected error, got %v", tt.value, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDuration(%q) unexpected error: %v", tt.value, err)
			}
			if got != tt.want {
				t.Errorf("parseDuration(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
