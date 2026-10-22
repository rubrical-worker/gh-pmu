package api

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestIsNotFound_WithNotFoundError(t *testing.T) {
	err := ErrNotFound
	if !IsNotFound(err) {
		t.Error("Expected IsNotFound to return true for ErrNotFound")
	}
}

func TestIsNotFound_WithGraphQLNotFound(t *testing.T) {
	err := errors.New("GraphQL: Could not resolve to a User")
	if !IsNotFound(err) {
		t.Error("Expected IsNotFound to return true for GraphQL not found")
	}
}

func TestIsNotFound_WithOtherError(t *testing.T) {
	err := errors.New("some other error")
	if IsNotFound(err) {
		t.Error("Expected IsNotFound to return false for other errors")
	}
}

func TestIsNotFound_WithNil(t *testing.T) {
	if IsNotFound(nil) {
		t.Error("Expected IsNotFound to return false for nil")
	}
}

func TestPartialSuccessError_NamesResourceURLAndSteps(t *testing.T) {
	err := &PartialSuccessError{
		Resource: "issue #42",
		URL:      "https://github.com/o/r/issues/42",
		Steps:    []string{"set status", "set priority"},
		Err:      errors.New("field update failed"),
	}
	msg := err.Error()
	for _, want := range []string{"issue #42", "https://github.com/o/r/issues/42", "set status", "set priority", "field update failed"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Expected %q in error message, got: %s", want, msg)
		}
	}
}

// Commands that did not create anything — a branch close, a multi-issue view —
// report their own completed work instead of "created … do not retry the
// create" (#938).
func TestPartialSuccessError_DoneReplacesCreateWording(t *testing.T) {
	err := &PartialSuccessError{
		Done:  "closed branch release/v1",
		Steps: []string{"move #7 to backlog"},
		Err:   errors.New("field update failed"),
	}
	msg := err.Error()
	for _, want := range []string{"partial success", "closed branch release/v1", "move #7 to backlog", "field update failed"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Expected %q in error message, got: %s", want, msg)
		}
	}
	if strings.Contains(msg, "created") || strings.Contains(msg, "retry the create") {
		t.Errorf("non-create partial success must not use the create wording, got: %s", msg)
	}
}

func TestPartialSuccessError_DetectableThroughWrapping(t *testing.T) {
	inner := errors.New("add failed")
	err := fmt.Errorf("create: %w", &PartialSuccessError{Resource: "issue #1", Steps: []string{"add to project"}, Err: inner})
	if !IsPartialSuccess(err) {
		t.Error("Expected IsPartialSuccess to see a wrapped PartialSuccessError")
	}
	if !errors.Is(err, inner) {
		t.Error("Expected PartialSuccessError to unwrap to its cause")
	}
	if IsPartialSuccess(inner) || IsPartialSuccess(nil) {
		t.Error("Expected IsPartialSuccess false for a plain error and for nil")
	}
}

func TestExitCodePartialSuccess_IsSix(t *testing.T) {
	if ExitCodePartialSuccess != 6 {
		t.Errorf("ExitCodePartialSuccess = %d, want 6 (#914 tranche A numbering)", ExitCodePartialSuccess)
	}
}

func TestIsRateLimited_WithRateLimitError(t *testing.T) {
	err := ErrRateLimited
	if !IsRateLimited(err) {
		t.Error("Expected IsRateLimited to return true for ErrRateLimited")
	}
}

func TestIsRateLimited_WithRateLimitMessage(t *testing.T) {
	err := errors.New("API rate limit exceeded")
	if !IsRateLimited(err) {
		t.Error("Expected IsRateLimited to return true for rate limit message")
	}
}

func TestIsAuthError_WithAuthError(t *testing.T) {
	err := ErrNotAuthenticated
	if !IsAuthError(err) {
		t.Error("Expected IsAuthError to return true for ErrNotAuthenticated")
	}
}

func TestIsAuthError_With401StringOnly(t *testing.T) {
	// Plain string "401 Unauthorized" without httpStatusCoder is NOT detected
	// as an auth error — this is intentional to prevent false positives.
	err := errors.New("401 Unauthorized")
	if IsAuthError(err) {
		t.Error("Expected IsAuthError to return false for plain string '401' without HTTP status code")
	}
}

func TestIsAuthError_With401HTTPError(t *testing.T) {
	err := &ghHTTPError{statusCode: 401, message: "Unauthorized"}
	if !IsAuthError(err) {
		t.Error("Expected IsAuthError to return true for HTTP 401")
	}
}

func TestIsAuthError_FalsePositive_IssueNumber(t *testing.T) {
	err := errors.New("issue #1401 not found")
	if IsAuthError(err) {
		t.Error("Expected IsAuthError to return false for issue #1401 (false positive)")
	}
}

func TestIsAuthError_FalsePositive_Timestamp(t *testing.T) {
	err := errors.New("request failed at 14:01:23")
	if IsAuthError(err) {
		t.Error("Expected IsAuthError to return false for timestamp containing 1401")
	}
}

func TestIsAuthError_StringFallback_Authentication(t *testing.T) {
	err := errors.New("authentication required")
	if !IsAuthError(err) {
		t.Error("Expected IsAuthError to return true for 'authentication' message")
	}
}

func TestIsAuthError_NonAuthHTTPError(t *testing.T) {
	err := &ghHTTPError{statusCode: 404, message: "Not Found"}
	if IsAuthError(err) {
		t.Error("Expected IsAuthError to return false for HTTP 404")
	}
}

func TestWrapError_WithNil(t *testing.T) {
	err := WrapError("get", "project", nil)
	if err != nil {
		t.Error("Expected WrapError to return nil for nil error")
	}
}

func TestWrapError_WithRateLimitError(t *testing.T) {
	original := errors.New("rate limit exceeded")
	err := WrapError("get", "project", original)

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatal("Expected wrapped error to be APIError")
	}

	if apiErr.Operation != "get" {
		t.Errorf("Expected operation 'get', got '%s'", apiErr.Operation)
	}

	if !errors.Is(apiErr.Err, ErrRateLimited) {
		t.Error("Expected wrapped error to contain ErrRateLimited")
	}
}

func TestWrapError_WithNotFoundError(t *testing.T) {
	original := errors.New("Could not resolve to a User")
	err := WrapError("get", "user", original)

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatal("Expected wrapped error to be APIError")
	}

	if !errors.Is(apiErr.Err, ErrNotFound) {
		t.Error("Expected wrapped error to contain ErrNotFound")
	}
}

func TestAPIError_Error(t *testing.T) {
	err := &APIError{
		Operation: "get",
		Resource:  "project",
		Err:       ErrNotFound,
	}

	expected := "get project: resource not found"
	if err.Error() != expected {
		t.Errorf("Expected '%s', got '%s'", expected, err.Error())
	}
}

func TestAPIError_Unwrap(t *testing.T) {
	inner := ErrNotFound
	err := &APIError{
		Operation: "get",
		Resource:  "project",
		Err:       inner,
	}

	if !errors.Is(err, ErrNotFound) {
		t.Error("Expected errors.Is to find ErrNotFound")
	}
}

func TestIsRateLimited_With403RateLimitHTTPError(t *testing.T) {
	err := &ghHTTPError{statusCode: 403, message: "API rate limit exceeded"}
	if !IsRateLimited(err) {
		t.Error("Expected IsRateLimited to return true for 403 with rate limit message")
	}
}

func TestIsRateLimited_With429HTTPError(t *testing.T) {
	err := &ghHTTPError{statusCode: 429, message: "Too Many Requests"}
	if !IsRateLimited(err) {
		t.Error("Expected IsRateLimited to return true for 429")
	}
}

func TestIsRateLimited_With403PermissionDenied(t *testing.T) {
	err := &ghHTTPError{statusCode: 403, message: "Resource not accessible by integration"}
	if IsRateLimited(err) {
		t.Error("Expected IsRateLimited to return false for 403 permission denied")
	}
}

func TestGetRetryAfter_FromHTTPError(t *testing.T) {
	err := &ghHTTPError{
		statusCode: 429,
		message:    "Too Many Requests",
		retryAfter: "5",
	}
	d := GetRetryAfter(err)
	if d != 5*time.Second {
		t.Errorf("Expected 5s retry-after, got %v", d)
	}
}

func TestGetRetryAfter_NoHeader(t *testing.T) {
	err := &ghHTTPError{statusCode: 429, message: "Too Many Requests"}
	d := GetRetryAfter(err)
	if d != 0 {
		t.Errorf("Expected 0 (no Retry-After), got %v", d)
	}
}

func TestGetRetryAfter_NonHTTPError(t *testing.T) {
	err := errors.New("some error")
	d := GetRetryAfter(err)
	if d != 0 {
		t.Errorf("Expected 0 for non-HTTP error, got %v", d)
	}
}

func TestIsTransient5xx_With502(t *testing.T) {
	err := &ghHTTPError{statusCode: 502, message: "Bad Gateway"}
	if !IsTransient5xx(err) {
		t.Error("Expected IsTransient5xx to return true for 502")
	}
}

func TestIsTransient5xx_With503(t *testing.T) {
	err := &ghHTTPError{statusCode: 503, message: "Service Unavailable"}
	if !IsTransient5xx(err) {
		t.Error("Expected IsTransient5xx to return true for 503")
	}
}

func TestIsTransient5xx_With504(t *testing.T) {
	err := &ghHTTPError{statusCode: 504, message: "Gateway Timeout"}
	if !IsTransient5xx(err) {
		t.Error("Expected IsTransient5xx to return true for 504")
	}
}

func TestIsTransient5xx_With500(t *testing.T) {
	// 500 typically signals a real bug, not a transient — do not retry.
	err := &ghHTTPError{statusCode: 500, message: "Internal Server Error"}
	if IsTransient5xx(err) {
		t.Error("Expected IsTransient5xx to return false for 500")
	}
}

func TestIsTransient5xx_With501(t *testing.T) {
	err := &ghHTTPError{statusCode: 501, message: "Not Implemented"}
	if IsTransient5xx(err) {
		t.Error("Expected IsTransient5xx to return false for 501")
	}
}

func TestIsTransient5xx_With429(t *testing.T) {
	// 429 is rate-limit, distinct classifier domain.
	err := &ghHTTPError{statusCode: 429, message: "Too Many Requests"}
	if IsTransient5xx(err) {
		t.Error("Expected IsTransient5xx to return false for 429 (rate-limit, not transient 5xx)")
	}
}

func TestIsTransient5xx_WithNil(t *testing.T) {
	if IsTransient5xx(nil) {
		t.Error("Expected IsTransient5xx to return false for nil")
	}
}

func TestIsTransient5xx_WithNonHTTPError(t *testing.T) {
	err := errors.New("some error without HTTPStatusCode")
	if IsTransient5xx(err) {
		t.Error("Expected IsTransient5xx to return false for non-HTTP error")
	}
}

func TestIsTransient5xx_StringFallback_504(t *testing.T) {
	// shurcoolGraphQL (under go-gh's typed GraphQL client) produces this
	// exact format via fmt.Errorf("non-200 OK status code: %v body: %q", ...).
	// The error does not satisfy httpStatusCoder, so the classifier must
	// fall back to string parsing — same pattern IsRateLimited uses.
	err := errors.New(`non-200 OK status code: 504 Gateway Timeout body: "{\"message\":\"...\"}"`)
	if !IsTransient5xx(err) {
		t.Error("Expected IsTransient5xx to return true for shurcoolGraphQL 504 string error")
	}
}

func TestIsTransient5xx_StringFallback_502(t *testing.T) {
	err := errors.New(`non-200 OK status code: 502 Bad Gateway body: "..."`)
	if !IsTransient5xx(err) {
		t.Error("Expected IsTransient5xx to return true for shurcoolGraphQL 502 string error")
	}
}

func TestIsTransient5xx_StringFallback_503(t *testing.T) {
	err := errors.New(`non-200 OK status code: 503 Service Unavailable body: "..."`)
	if !IsTransient5xx(err) {
		t.Error("Expected IsTransient5xx to return true for shurcoolGraphQL 503 string error")
	}
}

func TestIsTransient5xx_StringFallback_500NotRetryable(t *testing.T) {
	err := errors.New(`non-200 OK status code: 500 Internal Server Error body: "..."`)
	if IsTransient5xx(err) {
		t.Error("Expected IsTransient5xx to return false for 500 (real-bug, not transient)")
	}
}

func TestIsTransient5xx_StringFallback_404NotRetryable(t *testing.T) {
	err := errors.New(`non-200 OK status code: 404 Not Found body: "..."`)
	if IsTransient5xx(err) {
		t.Error("Expected IsTransient5xx to return false for 404")
	}
}

func TestIsRetryable_With429(t *testing.T) {
	err := &ghHTTPError{statusCode: 429, message: "Too Many Requests"}
	if !IsRetryable(err) {
		t.Error("Expected IsRetryable to return true for 429")
	}
}

func TestIsRetryable_With503(t *testing.T) {
	err := &ghHTTPError{statusCode: 503, message: "Service Unavailable"}
	if !IsRetryable(err) {
		t.Error("Expected IsRetryable to return true for 503")
	}
}

func TestIsRetryable_With504(t *testing.T) {
	err := &ghHTTPError{statusCode: 504, message: "Gateway Timeout"}
	if !IsRetryable(err) {
		t.Error("Expected IsRetryable to return true for 504")
	}
}

func TestIsRetryable_With403RateLimit(t *testing.T) {
	err := &ghHTTPError{statusCode: 403, message: "API rate limit exceeded"}
	if !IsRetryable(err) {
		t.Error("Expected IsRetryable to return true for 403 rate-limit")
	}
}

func TestIsRetryable_With403PermissionDenied(t *testing.T) {
	err := &ghHTTPError{statusCode: 403, message: "Resource not accessible by integration"}
	if IsRetryable(err) {
		t.Error("Expected IsRetryable to return false for 403 permission denied")
	}
}

func TestIsRetryable_With404(t *testing.T) {
	err := &ghHTTPError{statusCode: 404, message: "Not Found"}
	if IsRetryable(err) {
		t.Error("Expected IsRetryable to return false for 404")
	}
}

func TestIsRetryable_With500(t *testing.T) {
	err := &ghHTTPError{statusCode: 500, message: "Internal Server Error"}
	if IsRetryable(err) {
		t.Error("Expected IsRetryable to return false for 500")
	}
}

func TestIsRetryable_WithNil(t *testing.T) {
	if IsRetryable(nil) {
		t.Error("Expected IsRetryable to return false for nil")
	}
}

// IsFieldResolverUnavailable classifies the ProjectV2.fields connection 500s
// that GitHub returns when its resolver crashes on field rows that don't
// satisfy the ProjectV2FieldCommon interface contract. The classifier must
// recognise three signals: (1) errors that wrap ErrFieldResolverUnavailable,
// (2) HTTP 500 via httpStatusCoder, (3) the canonical phrases that go-gh's
// typed GraphQL client (shurcoolGraphQL) surfaces from GitHub's response body.
// 502/503/504 are explicitly NOT field-resolver crashes — those are transient
// and go through IsTransient5xx/WithRetry.

func TestIsFieldResolverUnavailable_WithSentinel(t *testing.T) {
	err := fmt.Errorf("failed to get project fields: %w", ErrFieldResolverUnavailable)
	if !IsFieldResolverUnavailable(err) {
		t.Error("Expected IsFieldResolverUnavailable to return true for wrapped sentinel")
	}
}

func TestIsFieldResolverUnavailable_WithHTTP500(t *testing.T) {
	err := &ghHTTPError{statusCode: 500, message: "Internal Server Error"}
	if !IsFieldResolverUnavailable(err) {
		t.Error("Expected IsFieldResolverUnavailable to return true for HTTP 500")
	}
}

func TestIsFieldResolverUnavailable_WithSomethingWentWrongMessage(t *testing.T) {
	// Canonical GitHub GraphQL 5xx phrasing surfaced as a plain string error
	// (e.g., via shurcoolGraphQL when GitHub returns errors[] with no data).
	err := errors.New("GraphQL: Something went wrong while executing your query on 2026-05-14T18:07:30Z. Please include `E290:183153:9C0F68F:2723794C:6A060F61` when reporting this issue.")
	if !IsFieldResolverUnavailable(err) {
		t.Error("Expected IsFieldResolverUnavailable to return true for 'Something went wrong while executing' message")
	}
}

func TestIsFieldResolverUnavailable_With500StringFallback(t *testing.T) {
	// shurcoolGraphQL "non-200 OK status code: 500" wire format — no httpStatusCoder available.
	err := errors.New(`non-200 OK status code: 500 Internal Server Error body: "{\"errors\":[...]}"`)
	if !IsFieldResolverUnavailable(err) {
		t.Error("Expected IsFieldResolverUnavailable to return true for shurcoolGraphQL 500 string")
	}
}

func TestIsFieldResolverUnavailable_WithNil(t *testing.T) {
	if IsFieldResolverUnavailable(nil) {
		t.Error("Expected IsFieldResolverUnavailable to return false for nil")
	}
}

func TestIsFieldResolverUnavailable_With502NotMatched(t *testing.T) {
	// 502 is transient/retryable — distinct from the resolver crash class.
	err := &ghHTTPError{statusCode: 502, message: "Bad Gateway"}
	if IsFieldResolverUnavailable(err) {
		t.Error("Expected IsFieldResolverUnavailable to return false for 502 (transient, not resolver crash)")
	}
}

func TestIsFieldResolverUnavailable_With503NotMatched(t *testing.T) {
	err := &ghHTTPError{statusCode: 503, message: "Service Unavailable"}
	if IsFieldResolverUnavailable(err) {
		t.Error("Expected IsFieldResolverUnavailable to return false for 503 (transient, not resolver crash)")
	}
}

func TestIsFieldResolverUnavailable_With504NotMatched(t *testing.T) {
	err := &ghHTTPError{statusCode: 504, message: "Gateway Timeout"}
	if IsFieldResolverUnavailable(err) {
		t.Error("Expected IsFieldResolverUnavailable to return false for 504 (transient, not resolver crash)")
	}
}

func TestIsFieldResolverUnavailable_WithNotFound(t *testing.T) {
	err := &ghHTTPError{statusCode: 404, message: "Not Found"}
	if IsFieldResolverUnavailable(err) {
		t.Error("Expected IsFieldResolverUnavailable to return false for 404")
	}
}

func TestIsFieldResolverUnavailable_WithAuthError(t *testing.T) {
	err := &ghHTTPError{statusCode: 401, message: "Bad credentials"}
	if IsFieldResolverUnavailable(err) {
		t.Error("Expected IsFieldResolverUnavailable to return false for 401")
	}
}

func TestIsFieldResolverUnavailable_WithRandomError(t *testing.T) {
	err := errors.New("network unreachable")
	if IsFieldResolverUnavailable(err) {
		t.Error("Expected IsFieldResolverUnavailable to return false for unrelated errors")
	}
}

// ghHTTPError simulates go-gh's api.HTTPError for testing within this package.
// We can't import github.com/cli/go-gh/v2/pkg/api here (same package name),
// so we use a local type and test that IsRateLimited handles the StatusCode interface.
type ghHTTPError struct {
	statusCode int
	message    string
	retryAfter string
}

func (e *ghHTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.statusCode, e.message)
}

func (e *ghHTTPError) HTTPStatusCode() int {
	return e.statusCode
}

func (e *ghHTTPError) RetryAfterSeconds() string {
	return e.retryAfter
}
