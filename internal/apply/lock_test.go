package apply_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Altagen/Riced/internal/apply"
)

func TestAcquireLock_FirstCallSucceeds(t *testing.T) {
	home := t.TempDir()
	lock, err := apply.AcquireLock(home)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	if lock == nil {
		t.Fatal("lock is nil")
	}
	defer lock.Release()
	if _, err := os.Stat(apply.LockPath(home)); err != nil {
		t.Errorf("lock file not created on disk: %v", err)
	}
}

func TestAcquireLock_SecondCallFails(t *testing.T) {
	home := t.TempDir()
	lock, err := apply.AcquireLock(home)
	if err != nil {
		t.Fatalf("first AcquireLock: %v", err)
	}
	defer lock.Release()

	_, err = apply.AcquireLock(home)
	if err == nil {
		t.Fatal("second AcquireLock should have errored -- lock held")
	}
	if !strings.Contains(err.Error(), "lock") {
		t.Errorf("error should mention 'lock', got: %v", err)
	}
}

func TestAcquireLock_ReleaseAllowsReacquire(t *testing.T) {
	home := t.TempDir()
	lock, err := apply.AcquireLock(home)
	if err != nil {
		t.Fatal(err)
	}
	lock.Release()

	// File must be gone, second acquire must succeed.
	if _, err := os.Stat(apply.LockPath(home)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("lock file should be removed by Release, stat err=%v", err)
	}
	lock2, err := apply.AcquireLock(home)
	if err != nil {
		t.Fatalf("AcquireLock after Release should succeed: %v", err)
	}
	lock2.Release()
}

func TestLock_Release_IsIdempotent(t *testing.T) {
	home := t.TempDir()
	lock, _ := apply.AcquireLock(home)
	lock.Release()
	lock.Release() // second call must not panic / fail
}
