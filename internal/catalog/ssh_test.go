package catalog

import (
	"reflect"
	"testing"
)

func TestParseSSHHosts(t *testing.T) {
	content := "Host *.internal\n  User ignored\nHost prod production\n  HostName %h.example.com\n  User deploy\n  Port 2222\nHost dev\n  HostName dev.example.com\n"
	got := ParseSSHHosts(content, "stan")
	want := []SSHHost{
		{Name: "dev", Target: "stan@dev.example.com:22"},
		{Name: "prod", Target: "deploy@prod.example.com:2222"},
		{Name: "production", Target: "deploy@production.example.com:2222"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hosts = %#v, want %#v", got, want)
	}
}
