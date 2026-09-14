package api

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// Tests for the cached-metadata fallback path engaged when
// ErrFieldResolverUnavailable surfaces from the bulk fetch. See issue #853.

func TestGetProjectFields_Fallback_EngagesOnResolverUnavailable(t *testing.T) {
	resetFieldsCacheWarningForTesting()
	cached := []ProjectField{
		{ID: "f1", Name: "Status", DataType: "SINGLE_SELECT", Options: []FieldOption{
			{ID: "o1", Name: "Backlog"},
			{ID: "o2", Name: "Done"},
		}},
		{ID: "f2", Name: "Branch", DataType: "TEXT"},
	}
	restore := SetCachedFieldsLoaderForTesting(func() ([]ProjectField, error) {
		return cached, nil
	})
	defer restore()

	var stderr bytes.Buffer
	restoreW := SetFieldsCacheWarningWriterForTesting(&stderr)
	defer restoreW()

	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			return errors.New("GraphQL: Something went wrong while executing your query on 2026-05-14T19:00:00Z. Please include `ABCD:1234:ABCD:1234:6A060000` when reporting this issue.")
		},
	}

	client := NewClientWithGraphQL(mock)
	fields, err := client.GetProjectFields("proj-id")

	if err != nil {
		t.Fatalf("Expected fallback to succeed; got error: %v", err)
	}
	if len(fields) != 2 {
		t.Fatalf("Expected 2 cached fields; got %d", len(fields))
	}
	if fields[0].Name != "Status" || fields[1].Name != "Branch" {
		t.Errorf("Expected cached fields preserved in order; got %v", fields)
	}
	if len(fields[0].Options) != 2 {
		t.Errorf("Expected single-select options preserved; got %d options", len(fields[0].Options))
	}
	if !strings.Contains(stderr.String(), "ProjectV2 field resolver unavailable") {
		t.Errorf("Expected stderr warning emitted; got: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "2 fields") {
		t.Errorf("Expected warning to mention field count; got: %q", stderr.String())
	}
}

// TestGetProjectFields_Fallback_MarksFieldsFromCache (#917): callers that would
// mutate options must be able to tell cached fields (ID + name only, possibly
// stale) from live ones.
func TestGetProjectFields_Fallback_MarksFieldsFromCache(t *testing.T) {
	resetFieldsCacheWarningForTesting()
	restore := SetCachedFieldsLoaderForTesting(func() ([]ProjectField, error) {
		return []ProjectField{{ID: "f1", Name: "Status", DataType: "SINGLE_SELECT"}, {ID: "f2", Name: "Branch", DataType: "TEXT"}}, nil
	})
	defer restore()
	restoreW := SetFieldsCacheWarningWriterForTesting(&bytes.Buffer{})
	defer restoreW()

	failing := NewClientWithGraphQL(&queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			return errors.New("GraphQL: Something went wrong while executing your query on 2026-05-14T19:00:00Z. Please include `ABCD:1234:ABCD:1234:6A060000` when reporting this issue.")
		},
	})
	fields, err := failing.GetProjectFields("proj-id")
	if err != nil {
		t.Fatalf("expected fallback, got %v", err)
	}
	for _, f := range fields {
		if !f.FromCache {
			t.Errorf("field %q served from the cache should be marked FromCache", f.Name)
		}
	}

	live := NewClientWithGraphQL(&queryMockClient{})
	liveFields, err := live.GetProjectFields("proj-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, f := range liveFields {
		if f.FromCache {
			t.Errorf("live field %q must not be marked FromCache", f.Name)
		}
	}
}

func TestRefreshCachedFields_RefreshedFieldsAreLive(t *testing.T) {
	cached := []ProjectField{
		{ID: "f1", Name: "Status", DataType: "SINGLE_SELECT", FromCache: true},
		{ID: "f2", Name: "Branch", DataType: "TEXT", FromCache: true},
	}
	fetcher := func(_ string, name string) (*ProjectField, error) {
		if name == "Status" {
			return &ProjectField{ID: "f1", Name: "Status", DataType: "SINGLE_SELECT"}, nil
		}
		return nil, errors.New("still failing")
	}
	refreshed, _, _ := refreshCachedFields(fetcher, "proj", cached)
	if refreshed[0].FromCache {
		t.Error("a field re-fetched via field(name:) is live and must not stay marked FromCache")
	}
	if !refreshed[1].FromCache {
		t.Error("a field whose refresh failed keeps its cached entry and its FromCache mark")
	}
}

func TestGetProjectFields_Fallback_EmptyCacheReturnsActionableError(t *testing.T) {
	resetFieldsCacheWarningForTesting()
	restore := SetCachedFieldsLoaderForTesting(func() ([]ProjectField, error) {
		return nil, nil // cache loaded but empty
	})
	defer restore()

	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			return errors.New("GraphQL: Something went wrong while executing your query on 2026-05-14T19:00:00Z. Please include `WXYZ:9999:WXYZ:9999:6A060000` when reporting this issue.")
		},
	}

	client := NewClientWithGraphQL(mock)
	fields, err := client.GetProjectFields("proj-id")

	if err == nil {
		t.Fatal("Expected error when cache is empty and resolver is unavailable")
	}
	if fields != nil {
		t.Errorf("Expected nil fields when fallback can't recover; got %d entries", len(fields))
	}
	msg := err.Error()
	if !strings.Contains(msg, "field resolver unavailable") {
		t.Errorf("Expected actionable error to mention resolver unavailable; got: %v", err)
	}
	if !strings.Contains(msg, "gh pmu init") {
		t.Errorf("Expected actionable error to suggest 'gh pmu init'; got: %v", err)
	}
	// Sentinel must still be detectable for upstream callers that want to
	// distinguish "the resolver crashed AND we couldn't recover" from
	// "some other failure."
	if !errors.Is(err, ErrFieldResolverUnavailable) {
		t.Errorf("Expected sentinel preserved through empty-cache path; got: %v", err)
	}
}

func TestGetProjectFields_Fallback_NonResolverErrorPropagates(t *testing.T) {
	resetFieldsCacheWarningForTesting()
	cacheCalled := false
	restore := SetCachedFieldsLoaderForTesting(func() ([]ProjectField, error) {
		cacheCalled = true
		return []ProjectField{{ID: "x", Name: "x"}}, nil
	})
	defer restore()

	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			// Generic, non-resolver-shaped error (e.g., network failure).
			return errors.New("dial tcp: lookup api.github.com: no such host")
		},
	}

	client := NewClientWithGraphQL(mock)
	fields, err := client.GetProjectFields("proj-id")

	if err == nil {
		t.Fatal("Expected non-resolver error to propagate; got nil")
	}
	if fields != nil {
		t.Errorf("Expected nil fields for non-resolver failure; got %d", len(fields))
	}
	if cacheCalled {
		t.Error("Cache loader must NOT be called for non-resolver errors — that would mask real failures")
	}
	if errors.Is(err, ErrFieldResolverUnavailable) {
		t.Error("Non-resolver error must NOT be classified as resolver-unavailable")
	}
}

func TestGetProjectFields_Fallback_WarningEmittedOncePerProcess(t *testing.T) {
	resetFieldsCacheWarningForTesting()
	cached := []ProjectField{{ID: "f1", Name: "Status", DataType: "TEXT"}}
	restore := SetCachedFieldsLoaderForTesting(func() ([]ProjectField, error) {
		return cached, nil
	})
	defer restore()

	var stderr bytes.Buffer
	restoreW := SetFieldsCacheWarningWriterForTesting(&stderr)
	defer restoreW()

	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			return errors.New("GraphQL: Something went wrong while executing your query")
		},
	}
	client := NewClientWithGraphQL(mock)

	// Three calls in the same "process" (test invocation).
	for i := 0; i < 3; i++ {
		_, err := client.GetProjectFields("proj-id")
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
	}

	output := stderr.String()
	occurrences := strings.Count(output, "ProjectV2 field resolver unavailable")
	if occurrences != 1 {
		t.Errorf("Expected exactly 1 warning emission across 3 fallbacks; got %d. Output: %q", occurrences, output)
	}
}

func TestGetProjectFields_Fallback_LoaderErrorReturnsActionableError(t *testing.T) {
	resetFieldsCacheWarningForTesting()
	restore := SetCachedFieldsLoaderForTesting(func() ([]ProjectField, error) {
		return nil, errors.New("no .gh-pmu.json found in current directory or any parent")
	})
	defer restore()

	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			return errors.New("GraphQL: Something went wrong while executing your query")
		},
	}

	client := NewClientWithGraphQL(mock)
	fields, err := client.GetProjectFields("proj-id")

	if err == nil {
		t.Fatal("Expected error when cache loader itself fails")
	}
	if fields != nil {
		t.Errorf("Expected nil fields; got %d", len(fields))
	}
	if !errors.Is(err, ErrFieldResolverUnavailable) {
		t.Error("Sentinel must be preserved through cache-loader-failure path")
	}
	// The loader's own error should surface to the user so they can fix it.
	if !strings.Contains(err.Error(), "no .gh-pmu.json found") {
		t.Errorf("Expected loader error to surface; got: %v", err)
	}
}

func TestGetProjectFields_NoCacheFallback_PropagatesResolverError(t *testing.T) {
	// With NoCacheFallback set, the resolver crash propagates unchanged —
	// callers in CI / strict-mode automation can rely on the prior fail-loud
	// behaviour rather than silent fallback. The cache loader must NOT be
	// called.
	resetFieldsCacheWarningForTesting()
	cacheCalled := false
	restoreL := SetCachedFieldsLoaderForTesting(func() ([]ProjectField, error) {
		cacheCalled = true
		return []ProjectField{{ID: "f1", Name: "Status", DataType: "TEXT"}}, nil
	})
	defer restoreL()

	restoreO := SetFallbackOptionsForTesting(FallbackOptions{NoCacheFallback: true})
	defer restoreO()

	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			return errors.New("GraphQL: Something went wrong while executing your query on 2026-05-14T19:00:00Z. Please include `STRICT:1:2:3:6A060000` when reporting this issue.")
		},
	}
	client := NewClientWithGraphQL(mock)
	fields, err := client.GetProjectFields("proj-id")

	if err == nil {
		t.Fatal("Expected error to propagate when NoCacheFallback is set")
	}
	if fields != nil {
		t.Errorf("Expected nil fields under strict mode; got %d", len(fields))
	}
	if cacheCalled {
		t.Error("Cache loader must NOT be called when NoCacheFallback is set")
	}
	if !errors.Is(err, ErrFieldResolverUnavailable) {
		t.Errorf("Sentinel must propagate through strict mode; got: %v", err)
	}
	if !strings.Contains(err.Error(), "STRICT:1:2:3") {
		t.Errorf("Original error chain (trace ID) must be preserved; got: %v", err)
	}
}

func TestRefreshCachedFields_AllSucceed(t *testing.T) {
	cached := []ProjectField{
		{ID: "old-1", Name: "Status", DataType: "SINGLE_SELECT", Options: []FieldOption{{ID: "stale", Name: "Backlog"}}},
		{ID: "old-2", Name: "Branch", DataType: "TEXT"},
	}
	fetcher := func(_ string, name string) (*ProjectField, error) {
		switch name {
		case "Status":
			return &ProjectField{ID: "fresh-1", Name: "Status", DataType: "SINGLE_SELECT", Options: []FieldOption{
				{ID: "new-bl", Name: "Backlog"},
				{ID: "new-rd", Name: "Ready"},
			}}, nil
		case "Branch":
			return &ProjectField{ID: "fresh-2", Name: "Branch", DataType: "TEXT"}, nil
		}
		return nil, errors.New("unexpected name")
	}

	refreshed, ok, fail := refreshCachedFields(fetcher, "proj", cached)
	if ok != 2 || fail != 0 {
		t.Errorf("Expected 2 successes, 0 failures; got ok=%d fail=%d", ok, fail)
	}
	if refreshed[0].ID != "fresh-1" || len(refreshed[0].Options) != 2 {
		t.Errorf("Status not refreshed correctly: %+v", refreshed[0])
	}
	if refreshed[1].ID != "fresh-2" {
		t.Errorf("Branch not refreshed correctly: %+v", refreshed[1])
	}
}

func TestRefreshCachedFields_AllFail_PreservesOriginal(t *testing.T) {
	// If every per-name fetch fails (e.g., transient network blip mid-refresh),
	// the cached entries must be returned unchanged — never zero out cache
	// just because the refresh-path itself had trouble.
	cached := []ProjectField{
		{ID: "keep-1", Name: "Status", DataType: "SINGLE_SELECT", Options: []FieldOption{{ID: "x", Name: "Y"}}},
		{ID: "keep-2", Name: "Branch", DataType: "TEXT"},
	}
	fetcher := func(_ string, _ string) (*ProjectField, error) {
		return nil, errors.New("per-name fetch failed")
	}

	refreshed, ok, fail := refreshCachedFields(fetcher, "proj", cached)
	if ok != 0 || fail != 2 {
		t.Errorf("Expected 0 successes, 2 failures; got ok=%d fail=%d", ok, fail)
	}
	if len(refreshed) != 2 {
		t.Fatalf("Original cache length must be preserved when refresh fails entirely; got %d", len(refreshed))
	}
	if refreshed[0].ID != "keep-1" || refreshed[1].ID != "keep-2" {
		t.Errorf("Original IDs must be preserved on full failure; got %+v", refreshed)
	}
}

func TestRefreshCachedFields_PartialFailure(t *testing.T) {
	// Mix of successes and failures — refreshed entries replace, failed
	// entries keep their cached value.
	cached := []ProjectField{
		{ID: "old-1", Name: "Status", DataType: "SINGLE_SELECT"},
		{ID: "old-2", Name: "Custom", DataType: "TEXT"},
		{ID: "old-3", Name: "Branch", DataType: "TEXT"},
	}
	fetcher := func(_ string, name string) (*ProjectField, error) {
		if name == "Custom" {
			return nil, errors.New("Custom field unreachable mid-refresh")
		}
		return &ProjectField{ID: "fresh-" + name, Name: name, DataType: "TEXT"}, nil
	}

	refreshed, ok, fail := refreshCachedFields(fetcher, "proj", cached)
	if ok != 2 || fail != 1 {
		t.Errorf("Expected ok=2 fail=1; got ok=%d fail=%d", ok, fail)
	}
	if refreshed[0].ID != "fresh-Status" {
		t.Errorf("Status should be refreshed; got %+v", refreshed[0])
	}
	if refreshed[1].ID != "old-2" {
		t.Errorf("Custom should retain cached value when refresh fails; got %+v", refreshed[1])
	}
	if refreshed[2].ID != "fresh-Branch" {
		t.Errorf("Branch should be refreshed; got %+v", refreshed[2])
	}
}

func TestGetProjectFields_RefreshFlag_EngagesRefresh(t *testing.T) {
	// When RefreshCachedFields is set and fallback engages, GetProjectFields
	// must return the refreshed fields (not the raw cached ones).
	resetFieldsCacheWarningForTesting()
	restoreL := SetCachedFieldsLoaderForTesting(func() ([]ProjectField, error) {
		return []ProjectField{
			{ID: "stale-1", Name: "Status", DataType: "SINGLE_SELECT"},
			{ID: "stale-2", Name: "Branch", DataType: "TEXT"},
		}, nil
	})
	defer restoreL()
	restoreF := SetProjectFieldByNameFetcherForTesting(func(_ string, name string) (*ProjectField, error) {
		return &ProjectField{ID: "fresh-" + name, Name: name, DataType: "TEXT"}, nil
	})
	defer restoreF()
	restoreO := SetFallbackOptionsForTesting(FallbackOptions{RefreshCachedFields: true})
	defer restoreO()

	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			// Bulk fetch fails — only the refresh fetcher should succeed.
			return errors.New("GraphQL: Something went wrong while executing your query")
		},
	}
	client := NewClientWithGraphQL(mock)
	fields, err := client.GetProjectFields("proj-id")

	if err != nil {
		t.Fatalf("Expected refresh path to succeed; got error: %v", err)
	}
	if len(fields) != 2 {
		t.Fatalf("Expected 2 refreshed fields; got %d", len(fields))
	}
	if fields[0].ID != "fresh-Status" || fields[1].ID != "fresh-Branch" {
		t.Errorf("Expected refreshed IDs; got %+v", fields)
	}
}

func TestGetProjectFields_NoCacheFallback_Disabled_DefaultEngagesFallback(t *testing.T) {
	// Sanity test: when NoCacheFallback is FALSE (default), fallback engages
	// just like in the original AC2-4 tests. Ensures the flag is not
	// accidentally inverted.
	resetFieldsCacheWarningForTesting()
	restoreL := SetCachedFieldsLoaderForTesting(func() ([]ProjectField, error) {
		return []ProjectField{{ID: "f1", Name: "Status", DataType: "TEXT"}}, nil
	})
	defer restoreL()
	restoreO := SetFallbackOptionsForTesting(FallbackOptions{NoCacheFallback: false})
	defer restoreO()

	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			return errors.New("GraphQL: Something went wrong while executing your query")
		},
	}
	client := NewClientWithGraphQL(mock)
	fields, err := client.GetProjectFields("proj-id")

	if err != nil {
		t.Fatalf("Default mode should engage fallback; got error: %v", err)
	}
	if len(fields) != 1 {
		t.Errorf("Expected 1 cached field via fallback; got %d", len(fields))
	}
}

func TestProjectFieldsFromMetadata_PreservesOptions(t *testing.T) {
	// Pure converter test — no network, no config file.
	// Mirrors the shape config.FieldMetadata produces from .gh-pmu.json.
	fields := projectFieldsFromMetadataShape([]cachedFieldShape{
		{Name: "Status", ID: "f1", DataType: "SINGLE_SELECT", Options: []cachedOptionShape{
			{Name: "Backlog", ID: "o1"},
			{Name: "Done", ID: "o2"},
		}},
		{Name: "Branch", ID: "f2", DataType: "TEXT"},
	})

	if len(fields) != 2 {
		t.Fatalf("Expected 2 fields; got %d", len(fields))
	}
	if fields[0].ID != "f1" || fields[0].Name != "Status" || fields[0].DataType != "SINGLE_SELECT" {
		t.Errorf("Field 0 conversion mismatch: %+v", fields[0])
	}
	if len(fields[0].Options) != 2 {
		t.Fatalf("Expected 2 options on first field; got %d", len(fields[0].Options))
	}
	if fields[0].Options[0].ID != "o1" || fields[0].Options[0].Name != "Backlog" {
		t.Errorf("Option 0 conversion mismatch: %+v", fields[0].Options[0])
	}
	if fields[1].DataType != "TEXT" || len(fields[1].Options) != 0 {
		t.Errorf("TEXT field should have no options; got: %+v", fields[1])
	}
}
