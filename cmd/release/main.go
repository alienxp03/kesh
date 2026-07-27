// Command release validates a release snapshot and optionally creates the tag
// that starts the GitHub Actions release workflow.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var releaseVersionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
var releaseVersionPartsPattern = regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, input io.Reader, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("release", flag.ContinueOnError)
	flags.SetOutput(errorOutput)
	yes := flags.Bool("y", false, "create and push the release tag without prompting")
	flags.BoolVar(yes, "yes", false, "create and push the release tag without prompting")
	dryRun := flags.Bool("dry-run", false, "run the release snapshot without prompting or publishing")
	bump := flags.String("bump", "", "derive the next version from the latest tag: patch, minor, or major")
	flags.Usage = func() {
		fmt.Fprintln(errorOutput, "usage: go run ./cmd/release [options] [vX.Y.Z]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		flags.Usage()
		return errors.New("provide one release version or --bump, not both")
	}
	if *bump != "" && flags.NArg() != 0 {
		return errors.New("provide either a release version or --bump, not both")
	}
	if *bump == "" && flags.NArg() == 0 {
		flags.Usage()
		return errors.New("a release version or --bump is required")
	}
	version := ""
	var err error
	if *bump != "" {
		version, err = nextReleaseVersion(*bump)
	} else {
		version = flags.Arg(0)
	}
	if err != nil {
		return err
	}
	if !releaseVersionPattern.MatchString(version) {
		return fmt.Errorf("invalid release version %q: use vX.Y.Z or a semver prerelease", version)
	}

	fmt.Fprintf(output, "Running GoReleaser snapshot for %s...\n", version)
	if err := command(output, errorOutput, "goreleaser", "release", "--snapshot", "--clean"); err != nil {
		return fmt.Errorf("release dry run failed: %w", err)
	}
	if *dryRun {
		return nil
	}

	if !*yes {
		fmt.Fprintf(output, "Publish %s by creating and pushing its Git tag? [y/N] ", version)
		answer, err := bufio.NewReader(input).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("read confirmation: %w", err)
		}
		if !strings.EqualFold(strings.TrimSpace(answer), "y") {
			return errors.New("release cancelled")
		}
	}

	clean, err := repositoryIsClean()
	if err != nil {
		return err
	}
	if !clean {
		return errors.New("working tree is not clean; commit or stash changes before releasing")
	}
	branch, err := currentBranch()
	if err != nil {
		return err
	}
	if exists, err := localTagExists(version); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("local tag %s already exists", version)
	}
	if exists, err := remoteTagExists(version); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("remote tag %s already exists", version)
	}

	if err := command(output, errorOutput, "git", "push", "origin", branch); err != nil {
		return fmt.Errorf("push release commit: %w", err)
	}
	if err := command(output, errorOutput, "git", "tag", "-a", version, "-m", "Release "+version); err != nil {
		return fmt.Errorf("create release tag: %w", err)
	}
	if err := command(output, errorOutput, "git", "push", "origin", version); err != nil {
		return fmt.Errorf("push release tag: %w", err)
	}
	fmt.Fprintf(output, "Release tag %s pushed; GitHub Actions will publish the release and Homebrew formula.\n", version)
	return nil
}

func nextReleaseVersion(bump string) (string, error) {
	if bump != "patch" && bump != "minor" && bump != "major" {
		return "", fmt.Errorf("invalid bump %q: use patch, minor, or major", bump)
	}
	latest, err := latestReleaseTag()
	if err != nil {
		return "", err
	}
	return bumpReleaseVersion(latest, bump)
}

func bumpReleaseVersion(latest, bump string) (string, error) {
	if bump != "patch" && bump != "minor" && bump != "major" {
		return "", fmt.Errorf("invalid bump %q: use patch, minor, or major", bump)
	}
	parts := releaseVersionPartsPattern.FindStringSubmatch(latest)
	if len(parts) != 4 {
		return "", fmt.Errorf("latest release tag %q is not a supported semantic version", latest)
	}
	major, _ := strconv.Atoi(parts[1])
	minor, _ := strconv.Atoi(parts[2])
	patch, _ := strconv.Atoi(parts[3])
	switch bump {
	case "major":
		major++
		minor = 0
		patch = 0
	case "minor":
		minor++
		patch = 0
	case "patch":
		patch++
	}
	return fmt.Sprintf("v%d.%d.%d", major, minor, patch), nil
}

func latestReleaseTag() (string, error) {
	output, err := exec.Command("git", "tag", "--list", "v*", "--sort=-v:refname").Output()
	if err != nil {
		return "", fmt.Errorf("find latest release tag: %w", err)
	}
	for _, tag := range strings.Fields(string(output)) {
		if releaseVersionPattern.MatchString(tag) {
			return tag, nil
		}
	}
	return "", errors.New("no release tags found; use VERSION=vX.Y.Z for the first release")
}

func command(output, errorOutput io.Writer, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = output
	cmd.Stderr = errorOutput
	return cmd.Run()
}

func repositoryIsClean() (bool, error) {
	output, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		return false, fmt.Errorf("check working tree: %w", err)
	}
	return len(output) == 0, nil
}

func currentBranch() (string, error) {
	output, err := exec.Command("git", "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		return "", errors.New("release must run from a branch, not a detached HEAD")
	}
	branch := strings.TrimSpace(string(output))
	if branch == "" {
		return "", errors.New("release branch is empty")
	}
	return branch, nil
}

func localTagExists(version string) (bool, error) {
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/tags/"+version)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if exitCode(err) == 1 {
		return false, nil
	}
	return false, fmt.Errorf("check local tag %s: %w", version, err)
}

func remoteTagExists(version string) (bool, error) {
	cmd := exec.Command("git", "ls-remote", "--exit-code", "--tags", "origin", "refs/tags/"+version)
	output, err := cmd.Output()
	if err == nil {
		return len(strings.TrimSpace(string(output))) > 0, nil
	}
	if exitCode(err) == 2 {
		return false, nil
	}
	return false, fmt.Errorf("check remote tag %s: %w", version, err)
}

func exitCode(err error) int {
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	return -1
}
