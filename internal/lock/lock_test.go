package lock

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcquireRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "gleaner.lock")
	l, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if l.Path() != path {
		t.Errorf("path = %s", l.Path())
	}
	_, err = Acquire(path)
	if !errors.Is(err, ErrLocked) || !strings.Contains(err.Error(), path) {
		t.Fatalf("second Acquire = %v, want ErrLocked naming the file", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	if err := l.Release(); err != nil {
		t.Errorf("double release: %v", err)
	}
	l2, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire after Release: %v", err)
	}
	l2.Release()
	var nilLock *Lock
	if err := nilLock.Release(); err != nil {
		t.Error(err)
	}
}

// TestHelperHoldLock is run as a child process by TestOtherProcess.
func TestHelperHoldLock(t *testing.T) {
	path := os.Getenv("LOCK_HELPER_PATH")
	if path == "" {
		t.Skip("helper")
	}
	l, err := Acquire(path)
	if err != nil {
		os.Exit(3)
	}
	os.Stdout.WriteString("locked\n")
	// Hold the lock until the parent closes our stdin.
	buf := make([]byte, 1)
	os.Stdin.Read(buf)
	l.Release()
	os.Exit(0)
}

func TestOtherProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quotes.lock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperHoldLock$")
	cmd.Env = append(os.Environ(), "LOCK_HELPER_PATH="+path)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 7)
	if _, err := stdout.Read(buf); err != nil || string(buf) != "locked\n" {
		t.Fatalf("helper did not lock: %q %v", buf, err)
	}
	if _, err := Acquire(path); !errors.Is(err, ErrLocked) {
		t.Errorf("lock held by another process: Acquire = %v", err)
	}
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper: %v", err)
	}
	// The lock is gone with the process.
	l, err := Acquire(path)
	if err != nil {
		t.Fatalf("after the other process exited: %v", err)
	}
	l.Release()
}
