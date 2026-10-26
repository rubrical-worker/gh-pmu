package ratelimit

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeClock records sleeps instead of sleeping, and advances its own time by
// the amount slept so pacing arithmetic is observable without real delays.
type fakeClock struct {
	now    time.Time
	sleeps []time.Duration
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Sleep(d time.Duration) {
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
}

func (c *fakeClock) advance(d time.Duration) { c.now = c.now.Add(d) }

func testConfig() Config {
	return Config{
		MinInterval:      100 * time.Millisecond,
		MutatingInterval: 500 * time.Millisecond,
		MaxRetries:       3,
		BaseDelay:        1 * time.Second,
		MaxDelay:         5 * time.Second,
	}
}

// ---------------------------------------------------------------------------
// Classify
// ---------------------------------------------------------------------------

func TestClassify(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   Kind
	}{
		{"empty", "", None},
		{"ordinary failure", "Error: issue #12 not found", None},
		{"plain 403 permission is not a rate limit", "HTTP 403: Resource not accessible by integration", None},
		{"primary from gh", "HTTP 403: API rate limit exceeded for user ID 123.", Primary},
		{"primary from gh pmu", "Error: failed to add sub-issue: API rate limit exceeded", Primary},
		{"429", "HTTP 429: Too Many Requests", Primary},
		{"secondary", "HTTP 403: You have exceeded a secondary rate limit. Please wait a few minutes.", Secondary},
		{"abuse detection", "You have triggered an abuse detection mechanism.", Secondary},
		{"case insensitive", "SECONDARY RATE LIMIT", Secondary},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.output); got != tt.want {
				t.Errorf("Classify(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// RetryAfter
// ---------------------------------------------------------------------------

func TestRetryAfter(t *testing.T) {
	if d, ok := RetryAfter("Retry-After: 7"); !ok || d != 7*time.Second {
		t.Errorf("RetryAfter header form = %v, %v; want 7s, true", d, ok)
	}
	if d, ok := RetryAfter("please retry after 30 seconds"); !ok || d != 30*time.Second {
		t.Errorf("RetryAfter prose form = %v, %v; want 30s, true", d, ok)
	}
	if _, ok := RetryAfter("API rate limit exceeded"); ok {
		t.Error("RetryAfter with no hint should report ok=false")
	}
}

// ---------------------------------------------------------------------------
// IsMutating
// ---------------------------------------------------------------------------

func TestIsMutating(t *testing.T) {
	tests := []struct {
		args []string
		want bool
	}{
		{[]string{"view", "12", "--json=status"}, false},
		{[]string{"list", "--status", "backlog"}, false},
		{[]string{"board"}, false},
		{[]string{"branch", "current"}, false},
		{[]string{"sub", "list", "12"}, false},
		{[]string{"issue", "view", "--repo", "o/r", "12"}, false},
		{[]string{"project", "list", "--owner", "o"}, false},
		{[]string{"create", "--title", "x"}, true},
		{[]string{"move", "12", "--status", "done"}, true},
		{[]string{"branch", "start", "--name", "b"}, true},
		{[]string{"issue", "close", "--repo", "o/r", "12"}, true},
		{[]string{"project", "delete", "41"}, true},
		{nil, true},
	}
	for _, tt := range tests {
		if got := IsMutating(tt.args); got != tt.want {
			t.Errorf("IsMutating(%v) = %v, want %v", tt.args, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Pacing
// ---------------------------------------------------------------------------

func TestPace_FirstCallDoesNotSleep(t *testing.T) {
	clock := newFakeClock()
	l := New(testConfig(), clock)

	l.Pace(false)

	if len(clock.sleeps) != 0 {
		t.Errorf("first call slept %v, want no sleep", clock.sleeps)
	}
}

func TestPace_EnforcesMinimumIntervalForReads(t *testing.T) {
	clock := newFakeClock()
	l := New(testConfig(), clock)

	l.Pace(false)
	clock.advance(30 * time.Millisecond)
	l.Pace(false)

	if len(clock.sleeps) != 1 || clock.sleeps[0] != 70*time.Millisecond {
		t.Errorf("sleeps = %v, want [70ms]", clock.sleeps)
	}
}

func TestPace_MutatingUsesLongerInterval(t *testing.T) {
	clock := newFakeClock()
	l := New(testConfig(), clock)

	l.Pace(true)
	l.Pace(true)

	if len(clock.sleeps) != 1 || clock.sleeps[0] != 500*time.Millisecond {
		t.Errorf("sleeps = %v, want [500ms]", clock.sleeps)
	}
}

func TestPace_NoSleepWhenIntervalAlreadyElapsed(t *testing.T) {
	clock := newFakeClock()
	l := New(testConfig(), clock)

	l.Pace(true)
	clock.advance(2 * time.Second)
	l.Pace(true)

	if len(clock.sleeps) != 0 {
		t.Errorf("sleeps = %v, want none", clock.sleeps)
	}
}

// ---------------------------------------------------------------------------
// Do: retry, no-retry, breaker
// ---------------------------------------------------------------------------

// scripted returns a run func that replays outputs in order; an empty output
// means success.
func scripted(outputs ...string) (func() (string, error), *int) {
	calls := 0
	return func() (string, error) {
		out := outputs[calls]
		calls++
		if out == "" {
			return "", nil
		}
		return out, errors.New("exit status 1")
	}, &calls
}

func TestDo_SuccessIsNotRetried(t *testing.T) {
	l := New(testConfig(), newFakeClock())
	run, calls := scripted("")

	o := l.Do(false, true, run)

	if o.Err != nil || *calls != 1 {
		t.Errorf("err=%v calls=%d, want nil and 1", o.Err, *calls)
	}
}

func TestDo_NonRateLimitFailureIsNotRetried(t *testing.T) {
	l := New(testConfig(), newFakeClock())
	run, calls := scripted("Error: issue not found")

	o := l.Do(false, true, run)

	if *calls != 1 {
		t.Errorf("calls = %d, want 1 (non-rate-limit failures are never retried)", *calls)
	}
	if o.Err == nil || o.Kind != None {
		t.Errorf("Outcome = %+v, want the command's own error with Kind None", o)
	}
	var rl *RateLimitError
	if errors.As(o.Err, &rl) {
		t.Error("non-rate-limit failure must not be reported as a rate limit")
	}
}

func TestDo_RetryableRateLimitRecovers(t *testing.T) {
	clock := newFakeClock()
	l := New(testConfig(), clock)
	run, calls := scripted("API rate limit exceeded", "")

	o := l.Do(false, true, run)

	if o.Err != nil || *calls != 2 {
		t.Errorf("err=%v calls=%d, want nil and 2", o.Err, *calls)
	}
	if !containsDuration(clock.sleeps, 1*time.Second) {
		t.Errorf("sleeps = %v, want a 1s backoff", clock.sleeps)
	}
}

func TestDo_BackoffHonoursRetryAfterAndCap(t *testing.T) {
	clock := newFakeClock()
	l := New(testConfig(), clock)
	run, _ := scripted("HTTP 429\nRetry-After: 3", "HTTP 429\nRetry-After: 60", "")

	l.Do(false, true, run)

	if !containsDuration(clock.sleeps, 3*time.Second) {
		t.Errorf("sleeps = %v, want Retry-After 3s honoured", clock.sleeps)
	}
	if !containsDuration(clock.sleeps, 5*time.Second) {
		t.Errorf("sleeps = %v, want Retry-After 60s capped to MaxDelay 5s", clock.sleeps)
	}
}

func TestDo_ExponentialBackoffWithoutHint(t *testing.T) {
	clock := newFakeClock()
	l := New(testConfig(), clock)
	run, _ := scripted("API rate limit exceeded", "API rate limit exceeded", "API rate limit exceeded", "")

	l.Do(false, true, run)

	for _, want := range []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second} {
		if !containsDuration(clock.sleeps, want) {
			t.Errorf("sleeps = %v, want %v among them", clock.sleeps, want)
		}
	}
}

func TestDo_ExhaustedRetriesNameRateLimiting(t *testing.T) {
	l := New(testConfig(), newFakeClock())
	run, calls := scripted("API rate limit exceeded", "API rate limit exceeded", "API rate limit exceeded", "API rate limit exceeded")

	o := l.Do(false, true, run)

	if *calls != 4 {
		t.Errorf("calls = %d, want 4 (1 + MaxRetries 3)", *calls)
	}
	var rl *RateLimitError
	if !errors.As(o.Err, &rl) {
		t.Fatalf("Err = %v, want *RateLimitError", o.Err)
	}
	if !strings.Contains(strings.ToLower(o.Err.Error()), "rate limit") {
		t.Errorf("message %q does not name rate limiting", o.Err.Error())
	}
}

func TestDo_NoRetryModeReportsWithoutRetrying(t *testing.T) {
	l := New(testConfig(), newFakeClock())
	run, calls := scripted("API rate limit exceeded")

	o := l.Do(true, false, run)

	if *calls != 1 {
		t.Errorf("calls = %d, want 1 (runPMU is already retried inside gh pmu)", *calls)
	}
	var rl *RateLimitError
	if !errors.As(o.Err, &rl) {
		t.Fatalf("Err = %v, want *RateLimitError", o.Err)
	}
	if tripped, _ := l.Tripped(); tripped {
		t.Error("a primary rate limit must not trip the breaker")
	}
}

func TestDo_SecondaryLimitTripsBreakerAndBlocksTraffic(t *testing.T) {
	l := New(testConfig(), newFakeClock())
	run, _ := scripted("You have exceeded a secondary rate limit")

	l.Do(true, false, run)

	tripped, reason := l.Tripped()
	if !tripped || !strings.Contains(reason, "secondary") {
		t.Fatalf("Tripped() = %v, %q; want true with a reason naming the secondary limit", tripped, reason)
	}

	next, calls := scripted("")
	o := l.Do(false, true, next)
	if *calls != 0 {
		t.Errorf("breaker open but command ran %d time(s)", *calls)
	}
	if !errors.Is(o.Err, ErrCircuitOpen) {
		t.Errorf("Err = %v, want ErrCircuitOpen", o.Err)
	}
}

func TestDo_SecondaryLimitSurvivingRetriesTripsBreaker(t *testing.T) {
	cfg := testConfig()
	cfg.MaxRetries = 1
	l := New(cfg, newFakeClock())
	run, _ := scripted("secondary rate limit", "secondary rate limit")

	l.Do(false, true, run)

	if tripped, _ := l.Tripped(); !tripped {
		t.Error("secondary limit that survived backoff must trip the breaker")
	}
}

func TestDo_SecondaryLimitRecoveredByRetryDoesNotTrip(t *testing.T) {
	l := New(testConfig(), newFakeClock())
	run, _ := scripted("secondary rate limit", "")

	o := l.Do(false, true, run)

	if o.Err != nil {
		t.Errorf("Err = %v, want recovery", o.Err)
	}
	if tripped, _ := l.Tripped(); tripped {
		t.Error("a secondary limit cleared by backoff must not trip the breaker")
	}
}

// ---------------------------------------------------------------------------
// ConfigFromEnv
// ---------------------------------------------------------------------------

func TestConfigFromEnv_Defaults(t *testing.T) {
	cfg, err := ConfigFromEnv(func(string) string { return "" })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != DefaultConfig() {
		t.Errorf("cfg = %+v, want DefaultConfig %+v", cfg, DefaultConfig())
	}
}

func TestConfigFromEnv_Overrides(t *testing.T) {
	env := map[string]string{
		EnvMinInterval:      "50",
		EnvMutatingInterval: "2000",
		EnvMaxRetries:       "0",
	}
	cfg, err := ConfigFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MinInterval != 50*time.Millisecond || cfg.MutatingInterval != 2*time.Second || cfg.MaxRetries != 0 {
		t.Errorf("cfg = %+v, want overrides applied", cfg)
	}
}

func TestConfigFromEnv_RejectsInvalidValue(t *testing.T) {
	_, err := ConfigFromEnv(func(k string) string {
		if k == EnvMaxRetries {
			return "lots"
		}
		return ""
	})
	if err == nil || !strings.Contains(err.Error(), EnvMaxRetries) {
		t.Errorf("err = %v, want an error naming %s", err, EnvMaxRetries)
	}
}

func containsDuration(ds []time.Duration, want time.Duration) bool {
	for _, d := range ds {
		if d == want {
			return true
		}
	}
	return false
}
