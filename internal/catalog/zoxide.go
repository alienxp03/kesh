// Package catalog owns external sources used to assemble Kesh entries.
package catalog

import (
	"fmt"

	"github.com/alienxp03/dotfiles/apps/kesh/internal/system"
)

type Zoxide struct {
	Executable string
}

func (z Zoxide) Query() ([]byte, error) {
	if z.Executable == "" {
		return nil, fmt.Errorf("zoxide was not found")
	}
	output, err := system.Command(z.Executable, "query", "-l").Output()
	if err != nil {
		return nil, fmt.Errorf("zoxide query: %w", err)
	}
	return output, nil
}

func (z Zoxide) Add(path string) error {
	if z.Executable == "" {
		return fmt.Errorf("zoxide was not found")
	}
	output, err := system.Command(z.Executable, "add", "--", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("zoxide add: %s: %w", output, err)
	}
	return nil
}
