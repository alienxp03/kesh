package system

import (
	"errors"
	"reflect"
	"testing"
)

func TestOpenerSelectsPlatformCommandAndRunsTarget(t *testing.T) {
	available := map[string]string{"open": "/usr/bin/open", "xdg-open": "/usr/bin/xdg-open"}
	var invocation []string
	opener := Opener{
		GOOS: "darwin",
		LookPath: func(name string) (string, error) {
			if path := available[name]; path != "" {
				return path, nil
			}
			return "", errors.New("missing")
		},
		Run: func(name string, args ...string) error {
			invocation = append([]string{name}, args...)
			return nil
		},
	}
	if got := opener.Command(); got != "/usr/bin/open" {
		t.Fatalf("command = %q", got)
	}
	if err := opener.Open("https://example.com"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"/usr/bin/open", "https://example.com"}; !reflect.DeepEqual(invocation, want) {
		t.Fatalf("invocation = %#v, want %#v", invocation, want)
	}
}

func TestOpenerPrefersXDGOpenOnLinux(t *testing.T) {
	available := map[string]string{"open": "/usr/bin/open", "xdg-open": "/usr/bin/xdg-open"}
	opener := Opener{
		GOOS: "linux",
		LookPath: func(name string) (string, error) {
			if path := available[name]; path != "" {
				return path, nil
			}
			return "", errors.New("missing")
		},
	}
	if got := opener.Command(); got != "/usr/bin/xdg-open" {
		t.Fatalf("command = %q", got)
	}
}

func TestOpenerErrorsWithoutInstalledCommand(t *testing.T) {
	opener := Opener{
		GOOS: "linux",
		LookPath: func(string) (string, error) {
			return "", errors.New("missing")
		},
		Run: func(string, ...string) error { return nil },
	}
	if err := opener.Open("https://example.com"); err == nil {
		t.Fatal("missing opener was accepted")
	}
}
