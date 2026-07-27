package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type fileConfig struct {
	Clone struct {
		Root string `yaml:"root"`
	} `yaml:"clone"`
	Worktree struct {
		Root string `yaml:"root"`
	} `yaml:"worktree"`
	Checkout struct {
		Root string `yaml:"root"`
	} `yaml:"checkout"`
	Startup StartupConfig `yaml:"startup"`
}

type StartupConfig struct {
	Sessions []StartupSession `yaml:"sessions"`
}

type StartupSession struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
	Pin  *int   `yaml:"pin"`
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
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(&result); err != nil {
		return fileConfig{}, fmt.Errorf("invalid Kesh config: %w", err)
	}
	return result, nil
}

func StartupSessions(path, home string) ([]StartupSession, error) {
	configuration, err := readFile(path)
	if err != nil {
		return nil, err
	}
	result := make([]StartupSession, len(configuration.Startup.Sessions))
	for index, session := range configuration.Startup.Sessions {
		label := fmt.Sprintf("startup.sessions[%d]", index)
		if strings.TrimSpace(session.Path) == "" {
			return nil, fmt.Errorf("invalid Kesh config: %s.path is required", label)
		}
		expanded, err := ExpandHomePath(session.Path, home)
		if err != nil {
			return nil, fmt.Errorf("invalid Kesh config: %s.path: %w", label, err)
		}
		if !filepath.IsAbs(expanded) {
			return nil, fmt.Errorf("invalid Kesh config: %s.path must be absolute or home-relative", label)
		}
		if session.Pin != nil && (*session.Pin < 0 || *session.Pin > 9) {
			return nil, fmt.Errorf("invalid Kesh config: %s.pin must be between 0 and 9", label)
		}
		session.Path = filepath.Clean(expanded)
		session.Name = strings.TrimSpace(session.Name)
		result[index] = session
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
