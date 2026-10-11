package cp

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCancelScpStopsSSHChild(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "scp")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 30 &\necho $! > \"$1\"\nwait\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	pidFile := filepath.Join(dir, "child.pid")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runScpCommand(ctx, []string{pidFile}, nil) }()
	var pid int
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			if pid > 0 {
				break
			}
		}
		time.Sleep(time.Millisecond)
	}
	if pid <= 0 {
		t.Fatal("fake SCP did not start its SSH child")
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled SCP succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled SCP did not exit")
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		// A killed orphan can briefly remain a zombie until the container's init
		// reaps it. It must not still be running after SCP has returned.
		if os.IsNotExist(err) || (err == nil && strings.Fields(string(data))[2] == "Z") {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("SSH child survived SCP cancellation")
}
