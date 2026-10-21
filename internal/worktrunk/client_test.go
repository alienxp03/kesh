package worktrunk

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestSwitchArgumentsAndCustomPaths(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "existing"}[existing], func(t *testing.T) {
			var calls [][]string
			c := Client{Path: "/repo", Execute: func(dir string, args []string) (string, string, error) {
				if dir != "/repo" {
					t.Fatalf("dir=%s", dir)
				}
				calls = append(calls, args)
				switch args[0] {
				case "--config-set":
					if !reflect.DeepEqual(args, []string{"--config-set", "list.json-schema=2", "list", "--format=json", "--branches", "--remotes"}) {
						t.Fatalf("list args %v", args)
					}
					if existing {
						return `{"schema":2,"items":[{"branch":"fix/topic","remote":"upstream"}]}`, "", nil
					}
					return `{"schema":2,"items":[]}`, "", nil
				case "config":
					return `{"state":"approved"}`, "", nil
				case "switch":
					return `{"path":"/custom path/topic"}`, "", nil
				}
				t.Fatalf("unexpected %v", args)
				return "", "", nil
			}}
			path, err := c.Switch("fix/topic", "main")
			if err != nil || path != "/custom path/topic" {
				t.Fatalf("path=%q err=%v", path, err)
			}
			want := []string{"switch", "--no-cd", "--format=json"}
			if !existing {
				want = append(want, "--create", "--base", "main")
			}
			want = append(want, "fix/topic")
			if !reflect.DeepEqual(calls[len(calls)-1], want) {
				t.Fatalf("args=%v", calls)
			}
		})
	}
}

func TestApprovalBlocksEveryLifecycleAction(t *testing.T) {
	for _, state := range []string{"approval_required", "unknown", ""} {
		t.Run(state, func(t *testing.T) {
			c := Client{Path: "/repo", Execute: func(_ string, args []string) (string, string, error) {
				if args[0] != "config" {
					t.Fatalf("action ran without approval: %v", args)
				}
				return `{"state":"` + state + `"}`, "", nil
			}}
			for _, action := range []func() error{
				func() error { _, err := c.SwitchTarget("/tree"); return err },
				func() error { return c.Remove("/tree", true, true) },
				func() error { return c.Merge("main") },
			} {
				if err := action(); err == nil {
					t.Fatal("expected trust error")
				}
			}
		})
	}
}

func TestRemoveAndMergeArgumentsAndHookFailure(t *testing.T) {
	var last []string
	c := Client{Path: "/repo", Execute: func(_ string, args []string) (string, string, error) {
		if args[0] == "config" {
			return `{"state":"no_commands"}`, "", nil
		}
		last = args
		for _, arg := range args {
			if arg == "--yes" || arg == "-y" || arg == "--no-hooks" {
				t.Fatalf("unsafe flag %s", arg)
			}
		}
		return "", "hook failed", errors.New("exit 1")
	}}
	if err := c.Remove("/custom path/tree", true, true); err == nil || !strings.Contains(err.Error(), "hook failed") {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(last, []string{"remove", "--foreground", "--config-set", "switch.cd=false", "--force", "--force-delete", "--", "/custom path/tree"}) {
		t.Fatal(last)
	}
	if err := c.Merge("main"); err == nil {
		t.Fatal("expected error")
	}
	if !reflect.DeepEqual(last, []string{"merge", "--no-commit", "--no-squash", "--no-rebase", "--format=json", "--config-set", "merge.verify=true", "--config-set", "merge.remove=true", "--config-set", "switch.cd=false", "--", "main"}) {
		t.Fatal(last)
	}
}

func TestSchemaAndListFallback(t *testing.T) {
	c := Client{Execute: func(_ string, args []string) (string, string, error) {
		switch args[0] {
		case "config":
			return `{"state":"approved"}`, "", nil
		case "switch":
			return `{}`, "", nil
		default:
			return `{"schema":2,"items":[{"branch":"fix/topic","worktree":{"path":"/not-the-default/path"}}]}`, "", nil
		}
	}}
	path, err := c.Switch("fix/topic", "")
	if err != nil || path != "/not-the-default/path" {
		t.Fatalf("%q %v", path, err)
	}
	for _, data := range []string{`[]`, `{"schema":1,"items":[]}`, `no JSON`} {
		c.Execute = func(string, []string) (string, string, error) { return data, "", nil }
		if _, err := c.List(false); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
}

func TestMergeReportsRetainedOrUnknownCleanup(t *testing.T) {
	for _, data := range []string{`{"removed":false}`, `{}`, `not JSON`} {
		c := Client{Execute: func(_ string, args []string) (string, string, error) {
			if args[0] == "config" {
				return `{"state":"approved"}`, "", nil
			}
			return data, "", nil
		}}
		if err := c.Merge("main"); err == nil {
			t.Fatalf("accepted cleanup result %s", data)
		}
	}
}

func TestApprovalIsCheckedAgainAfterHookChanges(t *testing.T) {
	approved := true
	c := Client{Path: "/repo", Execute: func(_ string, args []string) (string, string, error) {
		if args[0] == "config" {
			if approved {
				return `{"state":"approved"}`, "", nil
			}
			return `{"state":"approval_required"}`, "", nil
		}
		if !approved {
			t.Fatal("ran changed hooks without approval")
		}
		return `{"path":"/custom/tree"}`, "", nil
	}}
	if _, err := c.SwitchTarget("/custom/tree"); err != nil {
		t.Fatal(err)
	}
	approved = false
	if _, err := c.SwitchTarget("/custom/tree"); err == nil || !strings.Contains(err.Error(), "config approvals add") {
		t.Fatalf("err=%v", err)
	}
}

func TestMissingWorktrunk(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := (Client{Path: t.TempDir()}).Switch("fix/topic", "")
	if err == nil || !strings.Contains(err.Error(), "Worktrunk is required") {
		t.Fatalf("err=%v", err)
	}
}
