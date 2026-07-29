package workspace

import (
	"os"
	"testing"
)

// gitHookEnvVars are exported by git into the environment when it invokes hooks
// (e.g. the pre-push hook during `git push`). When a `go test` process
// inherits them, every subprocess git call — both the mustRun/mustOutput
// helpers and the production run.DefaultRunner under test — operates on the
// wrong repository, surfacing as cryptic "index file open failed: Not a
// directory" failures. TestMain strips them so these real-git tests are immune
// to a polluted parent environment, regardless of how the suite is launched.
var gitHookEnvVars = []string{
	"GIT_DIR",
	"GIT_WORK_TREE",
	"GIT_INDEX_FILE",
	"GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_QUARANTINE_PATH",
}

func TestMain(m *testing.M) {
	for _, name := range gitHookEnvVars {
		os.Unsetenv(name)
	}
	os.Exit(m.Run())
}
