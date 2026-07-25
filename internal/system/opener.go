package system

import (
	"fmt"
	"runtime"
)

// Opener resolves and launches the platform URL opener.
type Opener struct {
	GOOS     string
	LookPath func(string) (string, error)
	Run      func(string, ...string) error
}

func DefaultOpener() Opener {
	return Opener{
		GOOS:     runtime.GOOS,
		LookPath: LookPath,
		Run: func(name string, args ...string) error {
			_, err := Command(name, args...).CombinedOutput()
			return err
		},
	}
}

func (o Opener) Command() string {
	names := []string{"xdg-open", "open"}
	if o.GOOS == "darwin" {
		names = []string{"open", "xdg-open"}
	}
	for _, name := range names {
		if path, err := o.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

func (o Opener) Open(target string) error {
	command := o.Command()
	if command == "" {
		return fmt.Errorf("no URL opener found (install xdg-open or open)")
	}
	if err := o.Run(command, target); err != nil {
		return fmt.Errorf("open URL: %w", err)
	}
	return nil
}
