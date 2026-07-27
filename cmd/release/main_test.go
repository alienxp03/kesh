package main

import "testing"

func TestReleaseVersionPattern(t *testing.T) {
	for _, version := range []string{"v0.1.0", "v1.2.3-rc.1", "v1.2.3+build.7"} {
		if !releaseVersionPattern.MatchString(version) {
			t.Errorf("release version %q was rejected", version)
		}
	}
	for _, version := range []string{"0.1.0", "v1", "release", "v1.2"} {
		if releaseVersionPattern.MatchString(version) {
			t.Errorf("invalid release version %q was accepted", version)
		}
	}
}

func TestBumpReleaseVersion(t *testing.T) {
	tests := map[string]string{
		"patch": "v0.1.1",
		"minor": "v0.2.0",
		"major": "v1.0.0",
	}
	for bump, want := range tests {
		if got, err := bumpReleaseVersion("v0.1.0", bump); err != nil || got != want {
			t.Errorf("bumpReleaseVersion(%q) = %q, %v; want %q", bump, got, err, want)
		}
	}
	if _, err := bumpReleaseVersion("v0.1.0", "invalid"); err == nil {
		t.Fatal("invalid bump unexpectedly succeeded")
	}
}
