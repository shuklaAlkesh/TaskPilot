// Package config parses environment variables into an immutable Config.
// Variable reference: backend/.env.example and docs/12-DEPLOYMENT-RUNBOOK.md §3.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

// Config holds every setting the API process needs. Build it once with Load.
type Config struct {
	Env                  string
	LogLevel             slog.Level
	DatabaseURL          string
	HTTPAddr             string
	WorkerAPIAddr        string
	WorkerTokens         []string // current token first, then the previous one during rotation
	EnableTestTasks      bool
	DBMaxConns           int32
	LeaseDuration        time.Duration
	HeartbeatInterval    time.Duration
	ReaperInterval       time.Duration
	AttemptDeadlineGrace time.Duration
	RetryBase            time.Duration
	RetryCap             time.Duration
	DefaultMaxAttempts   int
	MaxScheduleAhead     time.Duration
	IdempotencyTTL       time.Duration
	ShutdownReadyDelay   time.Duration
	ShutdownTimeout      time.Duration
}

// LogValue redacts secrets so a Config can be logged safely.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("env", c.Env),
		slog.String("http_addr", c.HTTPAddr),
		slog.String("worker_api_addr", c.WorkerAPIAddr),
		slog.Bool("enable_test_tasks", c.EnableTestTasks),
		slog.Duration("lease", c.LeaseDuration),
	)
}

// Load reads the environment. Errors name the variable, never its value.
func Load() (Config, error) {
	p := parser{}
	c := Config{
		Env:                  p.str("TASKPILOT_ENV", "development"),
		DatabaseURL:          p.required("DATABASE_URL"),
		HTTPAddr:             p.str("TASKPILOT_HTTP_ADDR", ":8080"),
		WorkerAPIAddr:        p.str("TASKPILOT_WORKER_API_ADDR", ":8081"),
		EnableTestTasks:      p.boolean("TASKPILOT_ENABLE_TEST_TASKS", false),
		DBMaxConns:           int32(p.integer("TASKPILOT_DB_MAX_CONNS", 10)),
		LeaseDuration:        p.duration("TASKPILOT_LEASE_DURATION", 30*time.Second),
		HeartbeatInterval:    p.duration("TASKPILOT_HEARTBEAT_INTERVAL", 10*time.Second),
		ReaperInterval:       p.duration("TASKPILOT_REAPER_INTERVAL", 15*time.Second),
		AttemptDeadlineGrace: p.duration("TASKPILOT_ATTEMPT_DEADLINE_GRACE", 30*time.Second),
		RetryBase:            p.duration("TASKPILOT_RETRY_BASE", 5*time.Second),
		RetryCap:             p.duration("TASKPILOT_RETRY_CAP", 10*time.Minute),
		DefaultMaxAttempts:   p.integer("TASKPILOT_DEFAULT_MAX_ATTEMPTS", 3),
		MaxScheduleAhead:     p.duration("TASKPILOT_MAX_SCHEDULE_AHEAD", 168*time.Hour),
		IdempotencyTTL:       p.duration("TASKPILOT_IDEMPOTENCY_TTL", 24*time.Hour),
		ShutdownReadyDelay:   p.duration("TASKPILOT_SHUTDOWN_READY_DELAY", 5*time.Second),
		ShutdownTimeout:      p.duration("TASKPILOT_SHUTDOWN_TIMEOUT", 20*time.Second),
	}
	c.WorkerTokens = []string{p.required("TASKPILOT_WORKER_TOKEN")}
	if prev := p.str("TASKPILOT_WORKER_TOKEN_PREVIOUS", ""); prev != "" {
		c.WorkerTokens = append(c.WorkerTokens, prev)
	}
	if err := c.LogLevel.UnmarshalText([]byte(p.str("TASKPILOT_LOG_LEVEL", "info"))); err != nil {
		p.errs = append(p.errs, errors.New("TASKPILOT_LOG_LEVEL: invalid level"))
	}
	if c.DefaultMaxAttempts < 1 || c.DefaultMaxAttempts > 10 {
		p.errs = append(p.errs, errors.New("TASKPILOT_DEFAULT_MAX_ATTEMPTS: must be 1-10"))
	}
	if c.HeartbeatInterval >= c.LeaseDuration {
		p.errs = append(p.errs, errors.New("TASKPILOT_HEARTBEAT_INTERVAL: must be shorter than TASKPILOT_LEASE_DURATION"))
	}
	return c, errors.Join(p.errs...)
}

// parser collects every bad variable so startup reports them all at once.
type parser struct{ errs []error }

func (p *parser) str(name, def string) string {
	if v, ok := os.LookupEnv(name); ok && v != "" {
		return v
	}
	return def
}

func (p *parser) required(name string) string {
	v, ok := os.LookupEnv(name)
	if !ok || v == "" {
		p.errs = append(p.errs, fmt.Errorf("%s: required", name))
	}
	return v
}

func (p *parser) integer(name string, def int) int {
	v, ok := os.LookupEnv(name)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		p.errs = append(p.errs, fmt.Errorf("%s: not an integer", name))
	}
	return n
}

func (p *parser) boolean(name string, def bool) bool {
	v, ok := os.LookupEnv(name)
	if !ok || v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		p.errs = append(p.errs, fmt.Errorf("%s: not a boolean", name))
	}
	return b
}

func (p *parser) duration(name string, def time.Duration) time.Duration {
	v, ok := os.LookupEnv(name)
	if !ok || v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		p.errs = append(p.errs, fmt.Errorf("%s: not a positive duration", name))
	}
	return d
}
