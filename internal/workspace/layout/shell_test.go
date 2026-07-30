package layout

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestVisibleCommandKeepsShellAfterInterrupt(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, ".kesh.env"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(directory, "ready")
	paneCommand := "sh -c " + SingleQuote("printf ready > "+SingleQuote(ready)+"; exec sleep 30")
	command := exec.Command("/bin/sh", "-c", VisibleCommand(directory, paneCommand))
	command.Env = []string{
		"HOME=" + os.Getenv("HOME"),
		"PATH=" + os.Getenv("PATH"),
		"SHELL=/bin/sh",
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
			_ = command.Wait()
			t.Fatalf("pane command did not start:\n%s", output.String())
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := syscall.Kill(-command.Process.Pid, syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("pane wrapper exited unsuccessfully after interrupt: %v\n%s", err, output.String())
		}
	case <-time.After(2 * time.Second):
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		<-done
		t.Fatalf("pane wrapper did not recover after interrupt:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "warning: pane command failed") {
		t.Fatalf("pane wrapper did not continue after interrupt:\n%s", output.String())
	}
}
