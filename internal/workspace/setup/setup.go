package setup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/alienxp03/kesh/internal/workspace/config"
	"github.com/alienxp03/kesh/internal/workspace/run"
)

type Plan struct {
	RepoRoot            string
	WorktreePath        string
	WorkspaceName       string
	Branch              string
	Copy                []string
	Symlink             []string
	RandomizePorts      []config.RandomizePort
	SetEnv              []config.SetEnv
	PreserveRandomPorts bool
	PostCreate          []string
	Context             Context
}

type Context struct {
	WorkspacePaths map[string]string
	PullRequest    *PullRequestContext
}

type PullRequestContext struct {
	Number  int
	URL     string
	HeadRef string
	HeadSHA string
}

type Logger struct {
	Stdout io.Writer
	Stderr io.Writer
	Prefix string
}

type ShellRunner interface {
	RunShell(ctx context.Context, command string, cwd string, inherit bool) run.Result
}

type ShellRunnerFunc func(ctx context.Context, command string, cwd string, inherit bool) run.Result

func (fn ShellRunnerFunc) RunShell(ctx context.Context, command string, cwd string, inherit bool) run.Result {
	return fn(ctx, command, cwd, inherit)
}

type DefaultShellRunner struct{}

func (DefaultShellRunner) RunShell(ctx context.Context, command string, cwd string, inherit bool) run.Result {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Dir = cwd
	if inherit {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		err := cmd.Run()
		return shellResult(err, "", "")
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return shellResult(err, stdout.String(), stderr.String())
}

func NewPlan(repoRoot string, worktreePath string, workspaceName string, branch string, files config.Files, hooks config.Hooks, randomizePorts []config.RandomizePort, setEnv []config.SetEnv, preserveRandomPorts bool, context Context) Plan {
	return Plan{
		RepoRoot:            repoRoot,
		WorktreePath:        worktreePath,
		WorkspaceName:       workspaceName,
		Branch:              branch,
		Copy:                append([]string(nil), files.Copy...),
		Symlink:             append([]string(nil), files.Symlink...),
		RandomizePorts:      append([]config.RandomizePort(nil), randomizePorts...),
		SetEnv:              cloneSetEnv(setEnv),
		PreserveRandomPorts: preserveRandomPorts,
		PostCreate:          append([]string(nil), hooks.PostCreate...),
		Context:             context,
	}
}

func Run(ctx context.Context, plan Plan, logger Logger, shellRunner ShellRunner) int {
	prepareStatus := RunPrepare(plan, logger)
	setEnvStatus := SetEnvFiles(plan, logger)
	contextStatus := WriteContextEnvLogged(plan, logger)
	commandStatus := RunPostCreate(ctx, plan, logger, shellRunner)
	if prepareStatus == 0 && setEnvStatus == 0 && contextStatus == 0 && commandStatus == 0 {
		return 0
	}
	return 1
}

func RunPrepare(plan Plan, logger Logger) int {
	existingRandomizeFiles := existingRandomizeDestinations(plan)
	copyStatus := CopyFiles(plan, logger)
	symlinkStatus := SymlinkFiles(plan, logger)
	randomizeStatus := randomizeEnvPorts(plan, logger, AllocateLocalPort, existingRandomizeFiles)
	if copyStatus == 0 && symlinkStatus == 0 && randomizeStatus == 0 {
		return 0
	}
	return 1
}

func WriteContextEnvLogged(plan Plan, logger Logger) int {
	if err := WriteContextEnv(plan); err != nil {
		logger.Warn("failed to write workspace env: %s", err)
		return 1
	}
	return 0
}

func cloneSetEnv(items []config.SetEnv) []config.SetEnv {
	cloned := make([]config.SetEnv, 0, len(items))
	for _, item := range items {
		vars := map[string]string{}
		for name, value := range item.Vars {
			vars[name] = value
		}
		cloned = append(cloned, config.SetEnv{
			File: item.File,
			Vars: vars,
		})
	}
	return cloned
}

func CopyFiles(plan Plan, logger Logger) int {
	status := 0
	for _, relativePath := range plan.Copy {
		sourcePath := filepath.Join(plan.RepoRoot, relativePath)
		destinationPath := filepath.Join(plan.WorktreePath, relativePath)
		if !isWithin(plan.RepoRoot, sourcePath) || !isWithin(plan.WorktreePath, destinationPath) {
			logger.Warn("skipping unsafe copy path: %s", relativePath)
			status = 1
			continue
		}
		stat, err := os.Stat(sourcePath)
		if os.IsNotExist(err) {
			logger.Warn("copy source not found, skipping: %s", relativePath)
			continue
		}
		if err != nil {
			logger.Warn("failed to stat copy source %s: %s", relativePath, err)
			status = 1
			continue
		}
		if !stat.Mode().IsRegular() {
			logger.Warn("copy source is not a regular file, skipping: %s", relativePath)
			continue
		}
		if pathExists(destinationPath) {
			logger.Warn("copy destination already exists, skipping: %s", relativePath)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
			logger.Warn("failed to create directory for %s: %s", relativePath, err)
			status = 1
			continue
		}
		data, err := os.ReadFile(sourcePath)
		if err != nil {
			logger.Warn("failed to copy %s: %s", relativePath, err)
			status = 1
			continue
		}
		if err := os.WriteFile(destinationPath, data, stat.Mode().Perm()); err != nil {
			logger.Warn("failed to copy %s: %s", relativePath, err)
			status = 1
			continue
		}
		logger.Info("copied %s", relativePath)
	}
	return status
}

func SymlinkFiles(plan Plan, logger Logger) int {
	status := 0
	for _, relativePath := range plan.Symlink {
		sourcePath := filepath.Join(plan.RepoRoot, relativePath)
		destinationPath := filepath.Join(plan.WorktreePath, relativePath)
		if !isWithin(plan.RepoRoot, sourcePath) || !isWithin(plan.WorktreePath, destinationPath) {
			logger.Warn("skipping unsafe symlink path: %s", relativePath)
			status = 1
			continue
		}
		stat, err := os.Stat(sourcePath)
		if os.IsNotExist(err) {
			logger.Warn("symlink source not found, skipping: %s", relativePath)
			continue
		}
		if err != nil {
			logger.Warn("failed to stat symlink source %s: %s", relativePath, err)
			status = 1
			continue
		}
		if !stat.Mode().IsRegular() {
			logger.Warn("symlink source is not a regular file, skipping: %s", relativePath)
			continue
		}
		if pathExists(destinationPath) {
			logger.Warn("symlink destination already exists, skipping: %s", relativePath)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
			logger.Warn("failed to create directory for %s: %s", relativePath, err)
			status = 1
			continue
		}
		if err := os.Symlink(sourcePath, destinationPath); err != nil {
			logger.Warn("failed to symlink %s: %s", relativePath, err)
			status = 1
			continue
		}
		logger.Info("symlinked %s", relativePath)
	}
	return status
}

type PortAllocator func() (int, error)

func RandomizeEnvPorts(plan Plan, logger Logger, allocate PortAllocator) int {
	return randomizeEnvPorts(plan, logger, allocate, nil)
}

func randomizeEnvPorts(plan Plan, logger Logger, allocate PortAllocator, existingFiles map[string]bool) int {
	if allocate == nil {
		allocate = AllocateLocalPort
	}
	status := 0
	used := map[int]bool{}
	for _, item := range plan.RandomizePorts {
		destinationPath := filepath.Join(plan.WorktreePath, item.File)
		if !isWithin(plan.WorktreePath, destinationPath) {
			logger.Warn("skipping unsafe randomize_ports path: %s", item.File)
			status = 1
			continue
		}
		source, err := os.ReadFile(destinationPath)
		if os.IsNotExist(err) {
			logger.Warn("randomize_ports file not found, skipping: %s", item.File)
			continue
		}
		if err != nil {
			logger.Warn("failed to read randomize_ports file %s: %s", item.File, err)
			status = 1
			continue
		}
		preserveExisting := plan.PreserveRandomPorts
		if existingFiles != nil {
			preserveExisting = preserveExisting && existingFiles[item.File]
		}
		updated, changed, err := renderRandomizedEnv(string(source), item.Vars, used, allocate, preserveExisting)
		if err != nil {
			logger.Warn("failed to randomize ports in %s: %s", item.File, err)
			status = 1
			continue
		}
		if !changed {
			continue
		}
		stat, err := os.Stat(destinationPath)
		if err != nil {
			logger.Warn("failed to stat randomize_ports file %s: %s", item.File, err)
			status = 1
			continue
		}
		if err := os.WriteFile(destinationPath, []byte(updated), stat.Mode().Perm()); err != nil {
			logger.Warn("failed to write randomized ports to %s: %s", item.File, err)
			status = 1
			continue
		}
		logger.Info("randomized ports in %s", item.File)
	}
	return status
}

func existingRandomizeDestinations(plan Plan) map[string]bool {
	existing := map[string]bool{}
	for _, item := range plan.RandomizePorts {
		destinationPath := filepath.Join(plan.WorktreePath, item.File)
		existing[item.File] = isWithin(plan.WorktreePath, destinationPath) && pathExists(destinationPath)
	}
	return existing
}

func SetEnvFiles(plan Plan, logger Logger) int {
	status := 0
	for _, item := range plan.SetEnv {
		destinationPath := filepath.Join(plan.WorktreePath, item.File)
		if !isWithin(plan.WorktreePath, destinationPath) {
			logger.Warn("skipping unsafe set_env path: %s", item.File)
			status = 1
			continue
		}
		source, err := os.ReadFile(destinationPath)
		if os.IsNotExist(err) {
			logger.Warn("set_env file not found, skipping: %s", item.File)
			continue
		}
		if err != nil {
			logger.Warn("failed to read set_env file %s: %s", item.File, err)
			status = 1
			continue
		}

		values := map[string]string{}
		failed := false
		for _, name := range sortedKeys(item.Vars) {
			value, err := RenderSetEnvTemplate(item.Vars[name], plan.Context)
			if err != nil {
				logger.Warn("failed to resolve set_env %s in %s: %s", name, item.File, err)
				status = 1
				failed = true
				continue
			}
			values[name] = value
		}
		if failed {
			continue
		}

		updated, changed := renderSetEnvFile(string(source), values)
		if !changed {
			continue
		}
		stat, err := os.Stat(destinationPath)
		if err != nil {
			logger.Warn("failed to stat set_env file %s: %s", item.File, err)
			status = 1
			continue
		}
		if err := os.WriteFile(destinationPath, []byte(updated), stat.Mode().Perm()); err != nil {
			logger.Warn("failed to write set_env values to %s: %s", item.File, err)
			status = 1
			continue
		}
		logger.Info("set env in %s", item.File)
	}
	return status
}

func RenderSetEnvTemplate(template string, context Context) (string, error) {
	var output strings.Builder
	remaining := template
	for {
		start := strings.Index(remaining, "${")
		if start < 0 {
			output.WriteString(remaining)
			return output.String(), nil
		}
		output.WriteString(remaining[:start])
		afterStart := remaining[start+2:]
		end := strings.Index(afterStart, "}")
		if end < 0 {
			return "", fmt.Errorf("unterminated template reference")
		}
		reference := afterStart[:end]
		if strings.Contains(reference, ":") {
			value, err := resolveSetEnvReference(reference, context)
			if err != nil {
				return "", err
			}
			output.WriteString(value)
		} else {
			output.WriteString("${")
			output.WriteString(reference)
			output.WriteString("}")
		}
		remaining = afterStart[end+1:]
	}
}

func resolveSetEnvReference(reference string, context Context) (string, error) {
	parts := strings.Split(reference, ":")
	workspaceName := ""
	sourceFile := ".env"
	varName := ""
	switch len(parts) {
	case 2:
		workspaceName = parts[0]
		varName = parts[1]
	case 3:
		workspaceName = parts[0]
		sourceFile = parts[1]
		varName = parts[2]
	default:
		return "", fmt.Errorf("invalid template reference %q", reference)
	}
	if workspaceName == "" || sourceFile == "" || varName == "" {
		return "", fmt.Errorf("invalid template reference %q", reference)
	}
	workspacePath, ok := context.WorkspacePaths[workspaceName]
	if !ok {
		return "", fmt.Errorf("unknown workspace %q", workspaceName)
	}
	sourcePath := filepath.Join(workspacePath, sourceFile)
	if !isWithin(workspacePath, sourcePath) {
		return "", fmt.Errorf("unsafe source path in template reference %q", reference)
	}
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", fmt.Errorf("read %s:%s: %w", workspaceName, sourceFile, err)
	}
	value, ok := envFileValue(string(source), varName)
	if !ok {
		return "", fmt.Errorf("missing %s in %s:%s", varName, workspaceName, sourceFile)
	}
	return value, nil
}

func renderSetEnvFile(source string, values map[string]string) (string, bool) {
	seen := map[string]bool{}
	lines := strings.SplitAfter(source, "\n")
	var output strings.Builder
	changed := false
	for _, line := range lines {
		name, prefix, ok := envAssignment(line)
		value, shouldSet := values[name]
		if !ok || !shouldSet {
			output.WriteString(line)
			continue
		}
		seen[name] = true
		newLine := prefix + quoteEnvValue(value, envValueQuote(line))
		if strings.HasSuffix(line, "\n") {
			newLine += "\n"
		}
		output.WriteString(newLine)
		if newLine != line {
			changed = true
		}
	}
	for _, name := range sortedKeys(values) {
		if seen[name] {
			continue
		}
		if output.Len() > 0 && !strings.HasSuffix(output.String(), "\n") {
			output.WriteByte('\n')
		}
		output.WriteString(name)
		output.WriteByte('=')
		output.WriteString(values[name])
		output.WriteByte('\n')
		changed = true
	}
	return output.String(), changed
}

func envFileValue(source string, varName string) (string, bool) {
	for _, line := range strings.SplitAfter(source, "\n") {
		name, _, ok := envAssignment(line)
		if !ok || name != varName {
			continue
		}
		trimmed := strings.TrimRight(line, "\r\n")
		_, value, ok := strings.Cut(trimmed, "=")
		if !ok {
			return "", false
		}
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		return value, true
	}
	return "", false
}

func envValueQuote(line string) string {
	trimmed := strings.TrimRight(line, "\r\n")
	_, value, ok := strings.Cut(trimmed, "=")
	if !ok {
		return ""
	}
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == value[len(value)-1] && (value[0] == '\'' || value[0] == '"') {
		return string(value[0])
	}
	return ""
}

func quoteEnvValue(value string, quote string) string {
	switch quote {
	case "'":
		if strings.Contains(value, "'") {
			return value
		}
		return "'" + value + "'"
	case `"`:
		escaped := strings.ReplaceAll(value, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		return `"` + escaped + `"`
	default:
		return value
	}
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func AllocateLocalPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("listener did not return a TCP address")
	}
	return address.Port, nil
}

func renderRandomizedEnv(source string, names []string, used map[int]bool, allocate PortAllocator, preserveExisting bool) (string, bool, error) {
	wanted := map[string]bool{}
	seen := map[string]bool{}
	for _, name := range names {
		wanted[name] = true
	}
	lines := strings.SplitAfter(source, "\n")
	var output strings.Builder
	changed := false
	for _, line := range lines {
		name, prefix, ok := envAssignment(line)
		if !ok || !wanted[name] {
			output.WriteString(line)
			continue
		}
		seen[name] = true
		if port, ok := existingEnvPort(line); ok && preserveExisting {
			used[port] = true
			output.WriteString(line)
			continue
		}
		port, err := nextPort(used, allocate)
		if err != nil {
			return "", false, err
		}
		output.WriteString(prefix)
		output.WriteString(strconv.Itoa(port))
		if strings.HasSuffix(line, "\n") {
			output.WriteByte('\n')
		}
		changed = true
	}
	for _, name := range names {
		if seen[name] {
			continue
		}
		port, err := nextPort(used, allocate)
		if err != nil {
			return "", false, err
		}
		if output.Len() > 0 && !strings.HasSuffix(output.String(), "\n") {
			output.WriteByte('\n')
		}
		output.WriteString(name)
		output.WriteByte('=')
		output.WriteString(strconv.Itoa(port))
		output.WriteByte('\n')
		changed = true
	}
	return output.String(), changed, nil
}

func nextPort(used map[int]bool, allocate PortAllocator) (int, error) {
	for attempts := 0; attempts < 100; attempts++ {
		port, err := allocate()
		if err != nil {
			return 0, err
		}
		if port <= 0 || port > 65535 {
			return 0, fmt.Errorf("allocated invalid port %d", port)
		}
		if used[port] {
			continue
		}
		used[port] = true
		return port, nil
	}
	return 0, fmt.Errorf("could not allocate a unique port")
}

func envAssignment(line string) (string, string, bool) {
	trimmed := strings.TrimRight(line, "\r\n")
	leadingLength := len(trimmed) - len(strings.TrimLeft(trimmed, " \t"))
	leading := trimmed[:leadingLength]
	rest := trimmed[leadingLength:]
	export := ""
	if strings.HasPrefix(rest, "export ") {
		export = "export "
		rest = strings.TrimLeft(strings.TrimPrefix(rest, "export "), " \t")
	}
	key, _, ok := strings.Cut(rest, "=")
	if !ok {
		return "", "", false
	}
	name := strings.TrimSpace(key)
	if name == "" || strings.ContainsAny(name, " \t") {
		return "", "", false
	}
	return name, leading + export + name + "=", true
}

func existingEnvPort(line string) (int, bool) {
	trimmed := strings.TrimRight(line, "\r\n")
	_, value, ok := strings.Cut(trimmed, "=")
	if !ok {
		return 0, false
	}
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"'`)
	port, err := strconv.Atoi(value)
	return port, err == nil && port > 0 && port <= 65535
}

func WriteContextEnv(plan Plan) error {
	envPath := ContextEnvPath(plan.WorktreePath)
	if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
		return err
	}
	content, err := RenderContextEnv(plan.Context)
	if err != nil {
		return err
	}
	return os.WriteFile(envPath, []byte(content), 0o600)
}

func RemoveContextEnv(worktreePath string) error {
	envPath := ContextEnvPath(worktreePath)
	if err := os.Remove(envPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func ContextEnvWorkspaceDirCount(worktreePath string) (int, error) {
	source, err := os.ReadFile(ContextEnvPath(worktreePath))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	count := 0
	for _, line := range strings.Split(string(source), "\n") {
		if isContextWorkspaceDirLine(strings.TrimSpace(line)) {
			count++
		}
	}
	return count, nil
}

func ContextEnvPath(worktreePath string) string {
	return filepath.Join(worktreePath, ".kesh.env")
}

func RenderContextEnv(context Context) (string, error) {
	values := map[string]string{}
	namesByKey := map[string]string{}
	for name, path := range context.WorkspacePaths {
		key, err := config.WorkspaceDirEnvKey(name)
		if err != nil {
			return "", err
		}
		if previous, ok := namesByKey[key]; ok {
			return "", fmt.Errorf("workspace env var %s conflicts between %q and %q", key, previous, name)
		}
		namesByKey[key] = name
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		values[key] = absolute
	}
	if context.PullRequest != nil {
		values["KESH_PR_NUMBER"] = strconv.Itoa(context.PullRequest.Number)
		values["KESH_PR_URL"] = context.PullRequest.URL
		values["KESH_PR_HEAD_REF"] = context.PullRequest.HeadRef
		values["KESH_PR_HEAD_SHA"] = context.PullRequest.HeadSHA
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var output strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&output, "export %s=%s\n", key, shellQuote(values[key]))
	}
	return output.String(), nil
}

func ReadPullRequestContext(worktreePath string) (PullRequestContext, bool, error) {
	source, err := os.ReadFile(ContextEnvPath(worktreePath))
	if os.IsNotExist(err) {
		return PullRequestContext{}, false, nil
	}
	if err != nil {
		return PullRequestContext{}, false, err
	}

	values := map[string]string{}
	for _, line := range strings.Split(string(source), "\n") {
		key, value, ok := exportedEnvValue(strings.TrimSpace(line))
		if ok {
			values[key] = value
		}
	}
	numberValue, ok := values["KESH_PR_NUMBER"]
	if !ok {
		return PullRequestContext{}, false, nil
	}
	number, err := strconv.Atoi(numberValue)
	if err != nil {
		return PullRequestContext{}, false, fmt.Errorf("invalid KESH_PR_NUMBER in %s: %w", ContextEnvPath(worktreePath), err)
	}
	return PullRequestContext{
		Number:  number,
		URL:     values["KESH_PR_URL"],
		HeadRef: values["KESH_PR_HEAD_REF"],
		HeadSHA: values["KESH_PR_HEAD_SHA"],
	}, true, nil
}

func RunPostCreate(ctx context.Context, plan Plan, logger Logger, shellRunner ShellRunner) int {
	if shellRunner == nil {
		shellRunner = DefaultShellRunner{}
	}
	for _, command := range plan.PostCreate {
		logger.Info("$ %s", command)
		result := shellRunner.RunShell(ctx, SourceEnvCommand(plan.WorktreePath, command), plan.WorktreePath, true)
		if result.ExitCode != 0 {
			detail := ""
			if result.Err != nil {
				detail = ": " + result.Err.Error()
			}
			logger.Warn("post create command failed (%d)%s: %s", result.ExitCode, detail, command)
			return 1
		}
	}
	return 0
}

func SourceEnvCommand(worktreePath string, command string) string {
	return ". " + shellQuote(ContextEnvPath(worktreePath)) + "; " + command
}

func JoinCommands(commands []string) string {
	return strings.Join(commands, " && ")
}

func isContextWorkspaceDirLine(line string) bool {
	const prefix = "export "
	if !strings.HasPrefix(line, prefix) {
		return false
	}
	key, value, ok := strings.Cut(strings.TrimPrefix(line, prefix), "=")
	if !ok || key == "KESH_WORKSPACE_DIR" {
		return false
	}
	if !strings.HasPrefix(key, "KESH_") || !strings.HasSuffix(key, "_DIR") {
		return false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(key, "KESH_"), "_DIR")
	if name == "" {
		return false
	}
	for _, char := range name {
		if (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\''
}

func exportedEnvValue(line string) (string, string, bool) {
	const prefix = "export "
	if !strings.HasPrefix(line, prefix) {
		return "", "", false
	}
	key, value, ok := strings.Cut(strings.TrimPrefix(line, prefix), "=")
	if !ok || strings.TrimSpace(key) == "" {
		return "", "", false
	}
	return strings.TrimSpace(key), shellUnquote(value), true
}

func shellUnquote(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
		inner := value[1 : len(value)-1]
		return strings.ReplaceAll(inner, "'\\''", "'")
	}
	return strings.Trim(value, `"'`)
}

func (logger Logger) Info(format string, args ...any) {
	writer := logger.Stdout
	if writer == nil {
		writer = os.Stdout
	}
	fmt.Fprintf(writer, logger.prefix()+format+"\n", args...)
}

func (logger Logger) Warn(format string, args ...any) {
	writer := logger.Stderr
	if writer == nil {
		writer = os.Stderr
	}
	fmt.Fprintf(writer, "warning: "+logger.prefix()+format+"\n", args...)
}

func (logger Logger) prefix() string {
	if logger.Prefix == "" {
		return ""
	}
	return logger.Prefix + ": "
}

func isWithin(root string, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && (relative == "." || (!strings.HasPrefix(relative, "..") && !filepath.IsAbs(relative)))
}

func pathExists(candidate string) bool {
	_, err := os.Lstat(candidate)
	return err == nil || !os.IsNotExist(err)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func shellResult(err error, stdout string, stderr string) run.Result {
	if err == nil {
		return run.Result{Stdout: stdout, Stderr: stderr, ExitCode: 0}
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return run.Result{Stdout: stdout, Stderr: stderr, ExitCode: exitErr.ExitCode(), Err: err}
	}
	return run.Result{Stdout: stdout, Stderr: stderr, ExitCode: 1, Err: err}
}
