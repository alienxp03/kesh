package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alienxp03/kesh/internal/config"
	kittyx "github.com/alienxp03/kesh/internal/kitty"
	"github.com/alienxp03/kesh/internal/state"
	"github.com/alienxp03/kesh/internal/workspace"
)

type startupTarget struct {
	Name        string
	Path        string
	SessionFile string
	Pin         *int
	HasRecipe   bool
}

func runStart(kitty string) error {
	targets, err := loadStartupTargets()
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return nil
	}

	client := kittyx.Client{Executable: kitty}
	kittyState, err := client.State()
	if err != nil {
		return fmt.Errorf("check Kitty sessions: %w", err)
	}
	if hasNamedKittySession(kittyState) {
		return errors.New("kesh start: startup sessions are already running; nothing was started")
	}
	if err := beginKittyRun(kitty, currentKittyPID()); err != nil {
		return err
	}

	pins, err := loadPins()
	if err != nil {
		return err
	}
	if err := validateStartupPins(targets, pins); err != nil {
		return err
	}

	env := currentEnvironment()
	for _, target := range targets {
		if target.HasRecipe {
			err = workspace.Open(context.Background(), workspace.OpenOptions{
				Cwd:         target.Path,
				Mode:        workspace.ModeAll,
				SessionName: target.Name,
				SessionFile: target.SessionFile,
				Env:         env,
			})
		} else {
			err = workspace.OpenFolder(context.Background(), target.Path, target.Name, target.SessionFile, env, nil)
		}
		if err != nil {
			return fmt.Errorf("start startup sessions: %w", err)
		}
	}

	updatedPins := startupPins(targets, pins)
	if len(updatedPins) != len(pins) || !samePins(updatedPins, pins) {
		if err := persistPins(kitty, updatedPins); err != nil {
			return fmt.Errorf("save startup pins: %w", err)
		}
	}
	return nil
}

func loadStartupTargets() ([]startupTarget, error) {
	home := os.Getenv("HOME")
	configured, err := config.StartupSessions(configPath(), home)
	if err != nil {
		return nil, err
	}
	targets := make([]startupTarget, 0, len(configured))
	usedNames := map[string]bool{}
	usedPins := map[int]bool{}
	for _, session := range configured {
		info, err := os.Stat(session.Path)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("startup path is not a directory: %s", session.Path)
		}
		name := startupSessionName(session.Name, session.Path, usedNames)
		if name == "" {
			return nil, fmt.Errorf("startup session name is empty for %s", session.Path)
		}
		usedNames[name] = true
		if session.Pin != nil {
			if usedPins[*session.Pin] {
				return nil, fmt.Errorf("startup pin slot %d is assigned more than once", *session.Pin)
			}
			usedPins[*session.Pin] = true
		}
		recipe, _, err := loadRecipe(session.Path)
		if err != nil {
			return nil, err
		}
		targets = append(targets, startupTarget{
			Name:        name,
			Path:        session.Path,
			SessionFile: filepath.Join(savedSessionDirectory(), name+".kitty-session"),
			Pin:         session.Pin,
			HasRecipe:   recipe != nil,
		})
	}
	return targets, nil
}

func startupSessionName(configured, path string, used map[string]bool) string {
	name := safeName(strings.TrimSpace(configured))
	if name == "" {
		name = safeName(filepath.Base(filepath.Clean(path)))
	}
	if name == "" {
		name = "session"
	}
	if used[name] {
		name += "-" + shortHash(path)
	}
	return name
}

func hasNamedKittySession(kittyState kittyx.State) bool {
	for _, osWindow := range kittyState {
		for _, tab := range osWindow.Tabs {
			for _, window := range tab.Windows {
				if strings.TrimSpace(window.SessionName) != "" || strings.TrimSpace(window.Env["KESH_KITTY_SESSION"]) != "" {
					return true
				}
			}
		}
	}
	return false
}

func validateStartupPins(targets []startupTarget, pins pinStore) error {
	for _, target := range targets {
		if target.Pin == nil {
			continue
		}
		existing, ok := pins[strconv.Itoa(*target.Pin)]
		if !ok || existing.Key == "workspace:"+target.Name {
			continue
		}
		return errors.New("kesh start: startup pin slots are already occupied; nothing was started")
	}
	return nil
}

func startupPins(targets []startupTarget, current pinStore) pinStore {
	updated := copyPins(current)
	for _, target := range targets {
		if target.Pin == nil {
			continue
		}
		slot := strconv.Itoa(*target.Pin)
		for existingSlot, existing := range updated {
			if existing.Key == "workspace:"+target.Name && existingSlot != slot {
				delete(updated, existingSlot)
			}
		}
		updated[slot] = state.PinTarget{
			Key:         "workspace:" + target.Name,
			Name:        target.Name,
			Kind:        "workspace",
			SessionFile: target.SessionFile,
			Version:     currentPinVersion,
		}
	}
	return updated
}

func samePins(left, right pinStore) bool {
	if len(left) != len(right) {
		return false
	}
	for slot, target := range left {
		if right[slot] != target {
			return false
		}
	}
	return true
}

func currentEnvironment() map[string]string {
	env := make(map[string]string)
	for _, entry := range os.Environ() {
		if key, value, ok := strings.Cut(entry, "="); ok {
			env[key] = value
		}
	}
	return env
}
