package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type fileConfig struct {
	Clone struct {
		Root string `toml:"root"`
	} `toml:"clone"`
	Worktree struct {
		Root string `toml:"root"`
	} `toml:"worktree"`
	Checkout struct {
		Root string `toml:"root"`
	} `toml:"checkout"`
}

func readFile(path string) (fileConfig, error) {
	var result fileConfig
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("read Kesh config: %w", err)
	}
	if _, err := toml.Decode(string(content), &result); err != nil {
		return fileConfig{}, fmt.Errorf("invalid Kesh config: %w", err)
	}
	return result, nil
}

func CloneRoot(path, home string) (string, error) {
	configuration, err := readFile(path)
	if err != nil {
		return "", err
	}
	return resolveRoot(configuration.Clone.Root, filepath.Join(home, "workspace"), home, "clone")
}

func WorktreeRoot(path, home string) (string, error) {
	configuration, err := readFile(path)
	if err != nil {
		return "", err
	}
	return resolveRoot(configuration.Worktree.Root, filepath.Join(home, "worktree"), home, "worktree")
}

func CheckoutRoot(path, home string) (string, error) {
	configuration, err := readFile(path)
	if err != nil {
		return "", err
	}
	cloneRoot, err := resolveRoot(configuration.Clone.Root, filepath.Join(home, "workspace"), home, "clone")
	if err != nil {
		return "", err
	}
	return resolveRoot(configuration.Checkout.Root, cloneRoot, home, "checkout")
}

func resolveRoot(configured, fallback, home, label string) (string, error) {
	root := fallback
	var err error
	if configured = strings.TrimSpace(configured); configured != "" {
		root, err = ExpandHomePath(configured, home)
		if err != nil {
			return "", fmt.Errorf("invalid %s root: %w", label, err)
		}
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("%s root must be an absolute or home-relative path", label)
	}
	return filepath.Clean(root), nil
}

func ExpandHomePath(path, home string) (string, error) {
	path = strings.TrimSpace(path)
	if strings.ContainsAny(path, "\r\n") {
		return "", fmt.Errorf("path cannot contain a line break")
	}
	switch {
	case path == "":
		return "", fmt.Errorf("path is required")
	case path == "~":
		return home, nil
	case strings.HasPrefix(path, "~/"):
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	case strings.HasPrefix(path, "~"):
		return "", fmt.Errorf("user-specific home paths are not supported: %s", path)
	default:
		return filepath.Clean(path), nil
	}
}
