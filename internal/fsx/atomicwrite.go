// Package fsx hosts tiny filesystem helpers that are independent of any
// other Riced package. Keeping them here breaks the dependency cycle
// that would otherwise occur if (say) registry needed apply's
// atomicWriteFile.
package fsx

import (
	"fmt"
	"os"
	"strconv"
)

// AtomicWriteFile writes body to path via "write tmp + rename". A crash
// between OpenFile and Rename leaves the previous content (if any) at
// path untouched; an orphan tmp file remains and can be deleted by hand
// (look for `*.riced.tmp.<pid>`).
//
// The tmp suffix embeds the PID so two distinct Riced processes
// targeting the same path don't trample each other's tmp file. The apply
// lock is the primary defense against concurrent writers; the PID
// suffix is belt-and-braces.
func AtomicWriteFile(path string, body []byte, mode os.FileMode) error {
	tmp := path + ".riced.tmp." + strconv.Itoa(os.Getpid())
	// O_EXCL: refuse to clobber a stale tmp from another in-flight
	// process. The lock should make this impossible in practice; defensive
	// is cheap.
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, path, err)
	}
	return nil
}
