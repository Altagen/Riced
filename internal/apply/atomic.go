package apply

import (
	"fmt"
	"os"

	"github.com/Altagen/Riced/internal/fsx"
)

// atomicWriteFile is the apply-local thin wrapper around fsx.AtomicWriteFile.
// Kept as a name to minimize churn in callers that already use it; the
// real implementation lives in fsx.
func atomicWriteFile(path string, body []byte, mode os.FileMode) error {
	return fsx.AtomicWriteFile(path, body, mode)
}

// tmpSuffix returns a per-process unique suffix used for atomic renames
// of OTHER artefacts (copyRegular, the symlink dance in Execute) that
// don't go through fsx.AtomicWriteFile. Keep it in sync with the suffix
// pattern fsx uses so `riced doctor` can spot orphans uniformly.
func tmpSuffix() string {
	return fmt.Sprintf(".riced.tmp.%d", os.Getpid())
}
