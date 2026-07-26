package layout

import (
	"strings"

	"github.com/alienxp03/kesh/internal/workspace/setup"
)

func VisibleCommand(worktreePath string, command string) string {
	return strings.Join([]string{
		SourceShellStartupCommand(),
		"printf '%s\\n' " + SingleQuote("$ "+command),
		setup.SourceEnvCommand(worktreePath, "eval "+SingleQuote(command)),
		"kesh_status=$?",
		"if [ \"$kesh_status\" -ne 0 ]; then printf '%s\\n' " + SingleQuote("warning: pane command failed: "+command) + " >&2; fi",
		"exec \"${SHELL:-/bin/sh}\" -i",
	}, "; ")
}

func SourceShellStartupCommand() string {
	return strings.Join([]string{
		"if [ -n \"${ZSH_VERSION:-}\" ] && [ -r \"${ZDOTDIR:-$HOME}/.zshrc\" ]; then . \"${ZDOTDIR:-$HOME}/.zshrc\" >/dev/null 2>&1; fi",
		"if [ -n \"${BASH_VERSION:-}\" ] && [ -r \"$HOME/.bashrc\" ]; then shopt -s expand_aliases 2>/dev/null || true; . \"$HOME/.bashrc\" >/dev/null 2>&1; fi",
	}, "; ")
}

func PaneShellCommand(worktreePath string, command string) string {
	return "exec \"${SHELL:-/bin/sh}\" -fc " + SingleQuote(VisibleCommand(worktreePath, command))
}

func PaneCommandText(command PaneCommand) string {
	return setup.JoinCommands(command.Commands)
}

func SingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
