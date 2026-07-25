// Package kitty owns Kitty remote-control command construction.
package kitty

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/alienxp03/dotfiles/apps/kesh/internal/system"
)

type Client struct {
	Executable string
}

func (c Client) run(args ...string) ([]byte, error) {
	if c.Executable == "" {
		return nil, fmt.Errorf("kitty was not found")
	}
	output, err := system.Command(c.Executable, args...).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			err = fmt.Errorf("%s: %s", err, message)
		}
	}
	return output, err
}

func (c Client) Run(args ...string) error {
	_, err := c.run(args...)
	return err
}

func (c Client) List() ([]byte, error) {
	if c.Executable == "" {
		return nil, fmt.Errorf("kitty was not found")
	}
	return system.Command(c.Executable, "@", "ls").Output()
}

func (c Client) CaptureScreen(windowID int) ([]byte, error) {
	return c.run("@", "get-text", "--match", "id:"+strconv.Itoa(windowID), "--extent", "screen", "--ansi")
}

func (c Client) LoadConfig() error {
	return c.Run("@", "load-config")
}

func (c Client) GotoSession(session string) error {
	return c.Run("@", "action", "goto_session", session)
}

func (c Client) FocusWindow(windowID int) error {
	return c.Run("@", "focus-window", "--match", "id:"+strconv.Itoa(windowID))
}

func (c Client) FocusTab(tabID int) error {
	return c.Run("@", "focus-tab", "--match", "id:"+strconv.Itoa(tabID))
}

func (c Client) CloseWindow(windowID int) error {
	return c.Run("@", "close-window", "--match", "id:"+strconv.Itoa(windowID))
}

func (c Client) SetWindowTitle(windowID int, title string) error {
	return c.Run("@", "set-window-title", "--match", "id:"+strconv.Itoa(windowID), title)
}

func (c Client) SetTabTitle(tabID int, title string) error {
	return c.Run("@", "set-tab-title", "--match", "id:"+strconv.Itoa(tabID), title)
}

// SaveSession asks Kitty to serialize one named session without closing it.
func (c Client) SaveSession(sessionName, file string, foregroundCommands bool) error {
	args := []string{"@", "action", "save_as_session", "--save-only"}
	if foregroundCommands {
		args = append(args, "--use-foreground-process")
	}
	args = append(args, "--match=session:^"+sessionName+"$", file)
	return c.Run(args...)
}
