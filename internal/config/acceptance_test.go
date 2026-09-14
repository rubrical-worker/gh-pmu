package config

import "testing"

func TestRequiresReAcceptance_MajorBump_ReturnsTrue(t *testing.T) {
	// ARRANGE: Accepted on 0.15.0, current is 1.0.0
	accepted := "0.15.0"
	current := "1.0.0"

	// ACT
	result := RequiresReAcceptance(accepted, current)

	// ASSERT
	if !result {
		t.Error("Expected re-acceptance required for major version bump")
	}
}

func TestRequiresReAcceptance_MajorBumpAndDowngrade_ReturnTrue(t *testing.T) {
	// #919: any change to the major component requires re-acceptance, in either direction
	cases := []struct{ accepted, current string }{
		{"1.6.0", "2.0.0"},
		{"2.0.0", "1.9.0"},
	}
	for _, c := range cases {
		if !RequiresReAcceptance(c.accepted, c.current) {
			t.Errorf("RequiresReAcceptance(%q, %q) = false, want true for a major version change", c.accepted, c.current)
		}
	}
}

func TestRequiresReAcceptance_MinorBump_ReturnsFalse(t *testing.T) {
	// ARRANGE: #919 — accepted on 1.5.3, current is 1.6.0 (same major)
	accepted := "1.5.3"
	current := "1.6.0"

	// ACT
	result := RequiresReAcceptance(accepted, current)

	// ASSERT
	if result {
		t.Error("Expected no re-acceptance required for a minor version bump within the same major")
	}
}

func TestRequiresReAcceptance_PatchBump_ReturnsFalse(t *testing.T) {
	// ARRANGE: Accepted on 0.15.0, current is 0.15.1
	accepted := "0.15.0"
	current := "0.15.1"

	// ACT
	result := RequiresReAcceptance(accepted, current)

	// ASSERT
	if result {
		t.Error("Expected no re-acceptance required for patch-only bump")
	}
}

func TestRequiresReAcceptance_SameVersion_ReturnsFalse(t *testing.T) {
	// ARRANGE: Same version
	accepted := "0.15.2"
	current := "0.15.2"

	// ACT
	result := RequiresReAcceptance(accepted, current)

	// ASSERT
	if result {
		t.Error("Expected no re-acceptance required for same version")
	}
}

func TestRequiresReAcceptance_EmptyAccepted_ReturnsTrue(t *testing.T) {
	// ARRANGE: No prior acceptance
	accepted := ""
	current := "0.15.2"

	// ACT
	result := RequiresReAcceptance(accepted, current)

	// ASSERT
	if !result {
		t.Error("Expected re-acceptance required when no prior acceptance")
	}
}

func TestRequiresReAcceptance_DevVersion_ReturnsFalse(t *testing.T) {
	// ARRANGE: Current is dev build
	accepted := "0.15.0"
	current := "dev"

	// ACT
	result := RequiresReAcceptance(accepted, current)

	// ASSERT
	if result {
		t.Error("Expected no re-acceptance required for dev builds")
	}
}

func TestRequiresReAcceptance_AcceptedOnDev_ReturnsTrue(t *testing.T) {
	// ARRANGE: Accepted on dev, current is real version
	accepted := "dev"
	current := "0.15.0"

	// ACT
	result := RequiresReAcceptance(accepted, current)

	// ASSERT
	if !result {
		t.Error("Expected re-acceptance required when accepted on dev build")
	}
}

func TestRequiresReAcceptance_MinorBumpMultiple_ReturnsFalse(t *testing.T) {
	// ARRANGE: #919 — accepted on 1.4.3, current is 1.6.0 (same major)
	accepted := "1.4.3"
	current := "1.6.0"

	// ACT
	result := RequiresReAcceptance(accepted, current)

	// ASSERT
	if result {
		t.Error("Expected no re-acceptance required for a multi-minor bump within the same major")
	}
}

func TestRequiresReAcceptance_PatchBumpOnOne_ReturnsFalse(t *testing.T) {
	if RequiresReAcceptance("1.5.3", "1.5.4") {
		t.Error("Expected no re-acceptance required for a patch bump")
	}
}

func TestRequiresReAcceptance_UnparseableVersion_ReturnsTrue(t *testing.T) {
	for _, c := range []struct{ accepted, current string }{
		{"garbage", "1.6.0"},
		{"1.6.0", "not-a-version"},
		{"x.1.0", "1.1.0"},
	} {
		if !RequiresReAcceptance(c.accepted, c.current) {
			t.Errorf("RequiresReAcceptance(%q, %q) = false, want true when a version cannot be parsed", c.accepted, c.current)
		}
	}
}
