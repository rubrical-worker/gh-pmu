// Package ratelimit paces and guards the live GitHub traffic the E2E suite
// sends (#920).
//
// It is deliberately untagged and free of exec/network code so its tests run
// under plain `go test ./...` in CI; test/e2e only wires it in. The E2E suite
// itself is `//go:build e2e` and CI never runs it (#876, #890).
//
// Retry is layered on the gh pmu binary's own: since #710/#849 the binary
// retries 429 and rate-limited 403 internally, so a rate-limit error seen from
// `gh pmu` output has already survived those retries. Callers therefore run
// gh pmu with retry disabled (detect, report, trip the breaker) and enable
// retry only for direct `gh` calls, which have none of their own.
package ratelimit

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Kind classifies a failed command's output.
type Kind int

const (
	// None means the failure is not a rate limit.
	None Kind = iota
	// Primary is the hourly API rate limit (HTTP 403 "API rate limit exceeded" or 429).
	Primary
	// Secondary is GitHub's secondary rate limit / abuse detection.
	Secondary
)

func (k Kind) String() string {
	switch k {
	case Primary:
		return "primary rate limit"
	case Secondary:
		return "secondary rate limit / abuse detection"
	default:
		return "not a rate limit"
	}
}

var (
	secondaryPattern = regexp.MustCompile(`(?i)secondary rate limit|abuse detection`)
	// A bare 403 is a permission error, so only rate-limit wording or a 429
	// counts as primary.
	primaryPattern   = regexp.MustCompile(`(?i)rate limit exceeded|\b429\b|too many requests`)
	retryAfterHeader = regexp.MustCompile(`(?i)retry-after:\s*(\d+)`)
	retryAfterProse  = regexp.MustCompile(`(?i)retry after (\d+) seconds?`)
	readVerbs        = map[string]bool{"view": true, "list": true, "board": true, "filter": true, "history": true, "current": true}
)

// ErrCircuitOpen is returned by Do once a secondary rate limit has tripped the
// breaker. Wrapped with the trip reason.
var ErrCircuitOpen = errors.New("circuit breaker open")

// Classify reports whether command output describes a rate limit, and which.
func Classify(output string) Kind {
	if secondaryPattern.MatchString(output) {
		return Secondary
	}
	if primaryPattern.MatchString(output) {
		return Primary
	}
	return None
}

// RetryAfter extracts a server-provided wait from command output, when the
// output exposes one.
func RetryAfter(output string) (time.Duration, bool) {
	for _, re := range []*regexp.Regexp{retryAfterHeader, retryAfterProse} {
		if m := re.FindStringSubmatch(output); m != nil {
			if s, err := strconv.Atoi(m[1]); err == nil && s > 0 {
				return time.Duration(s) * time.Second, true
			}
		}
	}
	return 0, false
}

// IsMutating reports whether a gh pmu or gh invocation writes. The first two
// non-flag tokens are checked against read verbs (`view`, `list`, `branch
// current`, `issue view`, `project list`, ...); anything else is treated as a
// write, so an unknown command gets the longer, safer interval.
func IsMutating(args []string) bool {
	seen := 0
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if readVerbs[a] {
			return false
		}
		seen++
		if seen == 2 {
			break
		}
	}
	return true
}

// Config holds pacing and retry settings.
type Config struct {
	// MinInterval is the minimum gap before any live call.
	MinInterval time.Duration
	// MutatingInterval is the minimum gap before a call that writes.
	MutatingInterval time.Duration
	// MaxRetries bounds rate-limit retries when retry is enabled.
	MaxRetries int
	// BaseDelay is the first backoff delay; each retry doubles it.
	BaseDelay time.Duration
	// MaxDelay caps any single wait, including a server Retry-After.
	MaxDelay time.Duration
}

// Environment variables read by ConfigFromEnv. Interval and delay values are
// milliseconds.
const (
	EnvMinInterval      = "E2E_MIN_INTERVAL_MS"
	EnvMutatingInterval = "E2E_MUTATING_INTERVAL_MS"
	EnvMaxRetries       = "E2E_MAX_RATE_LIMIT_RETRIES"
	EnvBaseDelay        = "E2E_RATE_LIMIT_BASE_DELAY_MS"
	EnvMaxDelay         = "E2E_RATE_LIMIT_MAX_DELAY_MS"
)

// DefaultConfig returns the settings used when no environment override is set.
func DefaultConfig() Config {
	return Config{
		MinInterval:      250 * time.Millisecond,
		MutatingInterval: 1 * time.Second,
		MaxRetries:       3,
		BaseDelay:        5 * time.Second,
		MaxDelay:         60 * time.Second,
	}
}

// ConfigFromEnv builds a Config from DefaultConfig plus any environment
// overrides. A value that is not a non-negative integer is an error naming the
// variable, rather than a silent fallback to the default.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	cfg := DefaultConfig()
	ms := []struct {
		name string
		dst  *time.Duration
	}{
		{EnvMinInterval, &cfg.MinInterval},
		{EnvMutatingInterval, &cfg.MutatingInterval},
		{EnvBaseDelay, &cfg.BaseDelay},
		{EnvMaxDelay, &cfg.MaxDelay},
	}
	for _, m := range ms {
		n, set, err := nonNegativeInt(getenv, m.name)
		if err != nil {
			return Config{}, err
		}
		if set {
			*m.dst = time.Duration(n) * time.Millisecond
		}
	}
	n, set, err := nonNegativeInt(getenv, EnvMaxRetries)
	if err != nil {
		return Config{}, err
	}
	if set {
		cfg.MaxRetries = n
	}
	return cfg, nil
}

func nonNegativeInt(getenv func(string) string, name string) (int, bool, error) {
	raw := strings.TrimSpace(getenv(name))
	if raw == "" {
		return 0, false, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, false, fmt.Errorf("%s=%q: want a non-negative integer", name, raw)
	}
	return n, true, nil
}

// Clock abstracts time so pacing and backoff are testable without sleeping.
type Clock interface {
	Now() time.Time
	Sleep(time.Duration)
}

// RealClock is the wall clock.
type RealClock struct{}

// Now returns the current time.
func (RealClock) Now() time.Time { return time.Now() }

// Sleep pauses for d.
func (RealClock) Sleep(d time.Duration) { time.Sleep(d) }

// RateLimitError reports a rate limit that was not cleared — either retry was
// disabled for the call, or the retries ran out.
type RateLimitError struct {
	Kind     Kind
	Attempts int
	Output   string
}

func (e *RateLimitError) Error() string {
	first := strings.TrimSpace(e.Output)
	if i := strings.IndexByte(first, '\n'); i >= 0 {
		first = first[:i]
	}
	return fmt.Sprintf("rate limited (%s) after %d attempt(s): %s", e.Kind, e.Attempts, first)
}

// Outcome is the result of one Do call.
type Outcome struct {
	// Output is the last attempt's output.
	Output string
	// Err is nil on success, a *RateLimitError for an uncleared rate limit,
	// ErrCircuitOpen (wrapped) when the breaker blocked the call, or the
	// command's own error otherwise.
	Err error
	// Kind classifies the last attempt's output.
	Kind Kind
	// Attempts counts how many times the command ran.
	Attempts int
}

// Limiter is the single pacing point for live traffic, with a suite-wide
// circuit breaker. Safe for concurrent use.
type Limiter struct {
	cfg   Config
	clock Clock

	mu         sync.Mutex
	last       time.Time
	tripped    bool
	tripReason string
}

// New returns a Limiter using cfg and clock.
func New(cfg Config, clock Clock) *Limiter {
	return &Limiter{cfg: cfg, clock: clock}
}

// Pace blocks until the configured interval has passed since the previous
// call, using the longer interval for writes.
func (l *Limiter) Pace(mutating bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	interval := l.cfg.MinInterval
	if mutating && l.cfg.MutatingInterval > interval {
		interval = l.cfg.MutatingInterval
	}
	if !l.last.IsZero() {
		if wait := interval - l.clock.Now().Sub(l.last); wait > 0 {
			l.clock.Sleep(wait)
		}
	}
	l.last = l.clock.Now()
}

// Tripped reports whether the breaker is open, and why.
func (l *Limiter) Tripped() (bool, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.tripped, l.tripReason
}

func (l *Limiter) trip(reason string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.tripped {
		l.tripped, l.tripReason = true, reason
	}
}

// Do paces and runs a command. run returns the command's combined output and
// its error. With retry enabled, a rate-limited attempt is retried with
// bounded backoff (Retry-After when the output carries one, otherwise
// exponential from BaseDelay), each wait capped at MaxDelay. A secondary rate
// limit that is not cleared trips the breaker, after which Do runs nothing.
func (l *Limiter) Do(mutating, retry bool, run func() (string, error)) Outcome {
	if tripped, reason := l.Tripped(); tripped {
		return Outcome{Err: fmt.Errorf("%s; no further API traffic sent: %w", reason, ErrCircuitOpen)}
	}

	var o Outcome
	for attempt := 0; ; attempt++ {
		l.Pace(mutating)
		out, err := run()
		o = Outcome{Output: out, Attempts: attempt + 1}
		if err == nil {
			return o
		}
		o.Kind = Classify(out)
		if o.Kind == None {
			o.Err = err
			return o
		}
		if !retry || attempt >= l.cfg.MaxRetries {
			break
		}
		l.clock.Sleep(l.backoff(attempt, out))
	}

	if o.Kind == Secondary {
		l.trip(fmt.Sprintf("%s not cleared after %d attempt(s)", o.Kind, o.Attempts))
	}
	o.Err = &RateLimitError{Kind: o.Kind, Attempts: o.Attempts, Output: o.Output}
	return o
}

func (l *Limiter) backoff(attempt int, output string) time.Duration {
	d, ok := RetryAfter(output)
	if !ok {
		d = l.cfg.BaseDelay << attempt
	}
	if l.cfg.MaxDelay > 0 && d > l.cfg.MaxDelay {
		d = l.cfg.MaxDelay
	}
	return d
}
