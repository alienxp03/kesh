package main

import "testing"

func TestVersionRequested(t *testing.T) {
	for _, args := range [][]string{{"-v"}, {"--version"}} {
		if !versionRequested(args) {
			t.Errorf("versionRequested(%q) = false", args)
		}
	}
	for _, args := range [][]string{nil, {}, {"version"}, {"--version", "extra"}} {
		if versionRequested(args) {
			t.Errorf("versionRequested(%q) = true", args)
		}
	}
}
