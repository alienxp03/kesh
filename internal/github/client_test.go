package github

import (
	"reflect"
	"testing"

	"github.com/alienxp03/dotfiles/apps/kesh/internal/domain"
	"github.com/alienxp03/dotfiles/apps/kesh/internal/system"
)

type fakeRunner struct {
	specs []system.Spec
	out   []byte
}

func (f *fakeRunner) Output(spec system.Spec) ([]byte, error) {
	f.specs = append(f.specs, spec)
	return f.out, nil
}

func (f *fakeRunner) CombinedOutput(spec system.Spec) ([]byte, error) {
	f.specs = append(f.specs, spec)
	return f.out, nil
}

func TestPullRequestHeadCommand(t *testing.T) {
	runner := &fakeRunner{out: []byte(`{"headRefName":"feature/widgets"}`)}
	restore := system.SetRunner(runner)
	t.Cleanup(restore)
	branch, err := (Client{Executable: "/bin/gh"}).PullRequestHead("owner", "repo", 42, "/repo")
	if err != nil || branch != "feature/widgets" {
		t.Fatalf("head = %q, %v", branch, err)
	}
	want := system.Spec{
		Name: "/bin/gh",
		Args: []string{"pr", "view", "42", "--repo", "owner/repo", "--json", "headRefName"},
		Dir:  "/repo",
	}
	if !reflect.DeepEqual(runner.specs, []system.Spec{want}) {
		t.Fatalf("specs = %#v", runner.specs)
	}
}

func TestPullRequestsParsesMergedStatus(t *testing.T) {
	runner := &fakeRunner{out: []byte(`[{"headRefName":"feature","headRefOid":"abc","state":"CLOSED","mergedAt":"2026-07-20T10:00:00Z","number":9,"url":"https://example.test/9"}]`)}
	restore := system.SetRunner(runner)
	t.Cleanup(restore)
	pullRequests, err := (Client{Executable: "gh"}).PullRequests("/repo")
	if err != nil {
		t.Fatal(err)
	}
	got := pullRequests[domain.PullRequestKey("feature", "abc")]
	if got.Status != "merged" || got.Number != 9 {
		t.Fatalf("pull request = %#v", got)
	}
}

func TestParsePullRequestReference(t *testing.T) {
	tests := []struct {
		value string
		want  PullRequestReference
	}{
		{"https://github.com/owner/repo/pull/12/files", PullRequestReference{Owner: "owner", Repository: "repo", Number: 12}},
		{"owner/repo#7", PullRequestReference{Owner: "owner", Repository: "repo", Number: 7}},
		{"42", PullRequestReference{Number: 42, UseSelected: true}},
	}
	for _, test := range tests {
		got, err := ParsePullRequestReference(test.value)
		if err != nil || got != test.want {
			t.Errorf("ParsePullRequestReference(%q) = %#v, %v, want %#v", test.value, got, err, test.want)
		}
	}
	for _, value := range []string{"", "git@github.com:owner/repo.git", "owner/repo#nope"} {
		if _, err := ParsePullRequestReference(value); err == nil {
			t.Errorf("ParsePullRequestReference(%q) succeeded", value)
		}
	}
}
