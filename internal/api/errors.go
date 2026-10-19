package api

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Common errors
var (
	ErrNotAuthenticated = errors.New("not authenticated - run 'gh auth login' first")
	ErrNotFound         = errors.New("resource not found")
	ErrRateLimited      = errors.New("API rate limit exceeded")

	// ErrFieldResolverUnavailable signals that GitHub's ProjectV2.fields GraphQL
	// resolver returned HTTP 500 ("Something went wrong while executing your
	// query"). This happens when project rows fail the ProjectV2FieldCommon
	// interface contract — e.g., the 2026-05-14 auto-rollout of Created/Closed/
	// Updated timestamp fields whose backing rows return null for required
	// (NON_NULL) name/dataType/id. Bulk fetches via `fields(first: N) { ... }`
	// crash; per-item reads and `field(name: ...)` lookups still work.
	//
	// Distinct from IsTransient5xx (502/503/504) which are retryable. 500 is
	// not retryable — the data shape causes the same crash each time. Callers
	// should detect this with IsFieldResolverUnavailable and fall back to
	// cached metadata rather than retrying.
	ErrFieldResolverUnavailable = errors.New("ProjectV2 field resolver unavailable")

	// ErrOwnerNotFound and ErrProjectNotFound keep two failures apart that a
	// joined error cannot distinguish: a login that resolves to no user or
	// organization, and an owner that exists but has no board at that number.
	// Collapsed, a typo in the owner reads as a missing project and sends the
	// user to fix the wrong argument — the misattribution fixed in #861.
	//
	// Neither covers "the board exists but has no Backlog view": that is not a
	// failure at all, and is reported as a false found flag (#901).
	ErrOwnerNotFound   = errors.New("project owner not found")
	ErrProjectNotFound = errors.New("project not found")
)

// APIError wraps GitHub API errors with additional context
type APIError struct {
	Operation string
	Resource  string
	Err       error
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s %s: %v", e.Operation, e.Resource, e.Err)
}

func (e *APIError) Unwrap() error {
	return e.Err
}

// ExitCodePartialSuccess is the process exit code for a command that created a
// resource and then failed a later step — the numbering #914 tranche A proposes,
// so that contract can absorb it rather than renumber it (#929).
const ExitCodePartialSuccess = 6

// PartialSuccessError reports that a resource was created but one or more later
// steps failed. It is distinct from total failure because a caller retrying the
// command would create a duplicate: the resource already exists (#929).
type PartialSuccessError struct {
	Resource string   // what was created, e.g. "issue #42"
	URL      string   // where it lives, so the caller can find it without a lookup
	Steps    []string // the steps that failed after creation, in order
	Err      error    // the first underlying failure
}

func (e *PartialSuccessError) Error() string {
	where := e.Resource
	if e.URL != "" {
		where = fmt.Sprintf("%s (%s)", e.Resource, e.URL)
	}
	return fmt.Sprintf("partial success: created %s, but %s failed: %v — do not retry the create",
		where, strings.Join(e.Steps, ", "), e.Err)
}

func (e *PartialSuccessError) Unwrap() error {
	return e.Err
}

// IsPartialSuccess reports whether err is, or wraps, a PartialSuccessError.
func IsPartialSuccess(err error) bool {
	var partial *PartialSuccessError
	return errors.As(err, &partial)
}

// IsNotFound checks if an error indicates a resource was not found
func IsNotFound(err error) bool {
	if errors.Is(err, ErrNotFound) {
		return true
	}
	if err == nil {
		return false
	}
	// Check for GraphQL "not found" patterns
	msg := err.Error()
	return strings.Contains(msg, "Could not resolve") ||
		strings.Contains(msg, "NOT_FOUND")
}

// httpStatusCoder is implemented by errors that carry an HTTP status code.
// go-gh's api.HTTPError satisfies this interface.
type httpStatusCoder interface {
	HTTPStatusCode() int
}

// retryAfterProvider is implemented by errors that carry a Retry-After value.
type retryAfterProvider interface {
	RetryAfterSeconds() string
}

// IsRateLimited checks if an error indicates rate limiting.
// Detects rate limits via:
//   - Sentinel ErrRateLimited
//   - HTTP 429 status code (any 429 is a rate limit)
//   - HTTP 403 with rate-limit messaging (GitHub secondary rate limits)
//   - Error message containing "rate limit" or "RATE_LIMITED"
//
// Non-rate-limit 403 errors (e.g., permission denied) are NOT retried.
func IsRateLimited(err error) bool {
	if errors.Is(err, ErrRateLimited) {
		return true
	}
	if err == nil {
		return false
	}

	// Check HTTP status code via interface
	var sc httpStatusCoder
	if errors.As(err, &sc) {
		code := sc.HTTPStatusCode()
		if code == 429 {
			return true
		}
		if code == 403 {
			msg := strings.ToLower(err.Error())
			return strings.Contains(msg, "rate limit") || strings.Contains(msg, "rate_limited")
		}
	}

	// Fallback: string-based detection for non-HTTP errors
	msg := err.Error()
	return strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "RATE_LIMITED")
}

// IsTransient5xx checks if an error indicates a transient server-side failure
// from the upstream API. Detects via the httpStatusCoder interface for the
// canonical transient status codes: 502 Bad Gateway, 503 Service Unavailable,
// 504 Gateway Timeout. 500 (Internal Server Error) and 501 (Not Implemented)
// are intentionally excluded — they typically indicate a real bug rather than
// a recoverable transient condition. 429 is handled by IsRateLimited, not here.
//
// Falls back to string-pattern matching for errors that do not satisfy
// httpStatusCoder. shurcoolGraphQL (used by go-gh's typed GraphQL client)
// produces plain fmt.Errorf strings of the form "non-200 OK status code: 504
// Gateway Timeout body: ..." — the string fallback recovers retryability for
// those cases. Mirrors the existing string fallback in IsRateLimited.
func IsTransient5xx(err error) bool {
	if err == nil {
		return false
	}
	var sc httpStatusCoder
	if errors.As(err, &sc) {
		switch sc.HTTPStatusCode() {
		case 502, 503, 504:
			return true
		}
		return false
	}
	// String fallback for errors without HTTP status interface.
	msg := err.Error()
	return strings.Contains(msg, "non-200 OK status code: 502") ||
		strings.Contains(msg, "non-200 OK status code: 503") ||
		strings.Contains(msg, "non-200 OK status code: 504")
}

// IsRetryable returns true if an error should trigger automatic retry. It
// composes IsRateLimited (429, rate-limited 403) with IsTransient5xx
// (502/503/504). Callers that need rate-limit-only semantics should keep
// using IsRateLimited directly.
func IsRetryable(err error) bool {
	return IsRateLimited(err) || IsTransient5xx(err)
}

// GetRetryAfter extracts a Retry-After duration from an error, if available.
// Returns 0 if no Retry-After information is present.
func GetRetryAfter(err error) time.Duration {
	var rap retryAfterProvider
	if errors.As(err, &rap) {
		if s := rap.RetryAfterSeconds(); s != "" {
			if seconds, parseErr := strconv.Atoi(s); parseErr == nil && seconds > 0 {
				return time.Duration(seconds) * time.Second
			}
		}
	}
	return 0
}

// IsFieldResolverUnavailable returns true when err signals that GitHub's
// ProjectV2.fields resolver is failing. Detected via:
//   - errors.Is match against ErrFieldResolverUnavailable (already-wrapped)
//   - HTTP 500 via httpStatusCoder
//   - Error message containing "Something went wrong while executing"
//     (canonical GitHub GraphQL 5xx body phrasing — surfaces from
//     shurcoolGraphQL as a plain string when errors[] is non-empty and data
//     is null)
//   - Error message containing "non-200 OK status code: 500" (shurcoolGraphQL
//     wire format when go-gh's HTTPError interface isn't carried through)
//
// Excludes 502/503/504 — those are transient and retryable via WithRetry.
// Excludes 401/403/404/429 — those have their own classifiers.
func IsFieldResolverUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrFieldResolverUnavailable) {
		return true
	}

	// HTTP status code via interface (primary detection when go-gh's
	// HTTPError propagates through).
	var sc httpStatusCoder
	if errors.As(err, &sc) {
		return sc.HTTPStatusCode() == 500
	}

	// String fallback for shurcoolGraphQL's plain fmt.Errorf path.
	msg := err.Error()
	return strings.Contains(msg, "Something went wrong while executing") ||
		strings.Contains(msg, "non-200 OK status code: 500")
}

// IsAuthError checks if an error indicates authentication issues.
// Detects auth errors via:
//   - Sentinel ErrNotAuthenticated
//   - HTTP 401 status code (via httpStatusCoder interface)
//   - Error message containing "authentication" or "not authenticated" (fallback)
func IsAuthError(err error) bool {
	if errors.Is(err, ErrNotAuthenticated) {
		return true
	}
	if err == nil {
		return false
	}

	// Check HTTP status code via interface (primary detection)
	var sc httpStatusCoder
	if errors.As(err, &sc) {
		return sc.HTTPStatusCode() == 401
	}

	// Fallback: string-based detection for non-HTTP errors
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "authentication") ||
		strings.Contains(msg, "not authenticated")
}

// WrapError wraps an API error with operation context
func WrapError(operation, resource string, err error) error {
	if err == nil {
		return nil
	}

	// Check for specific error types and wrap accordingly
	if IsRateLimited(err) {
		return &APIError{
			Operation: operation,
			Resource:  resource,
			Err:       ErrRateLimited,
		}
	}

	if IsNotFound(err) {
		return &APIError{
			Operation: operation,
			Resource:  resource,
			Err:       ErrNotFound,
		}
	}

	return &APIError{
		Operation: operation,
		Resource:  resource,
		Err:       err,
	}
}
