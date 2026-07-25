package git

import "github.com/alienxp03/kesh/internal/system"

type Wktree struct {
	Executable string
}

func (w Wktree) New(directory, mode, branch string, selected []string) error {
	args := []string{"new"}
	switch mode {
	case "all":
		args = append(args, "--workspaces")
	case "selected":
		for _, name := range selected {
			args = append(args, "--workspace", name)
		}
	}
	args = append(args, branch)
	command := system.Command(w.Executable, args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		return CommandError("wktree new", output, err)
	}
	return nil
}
