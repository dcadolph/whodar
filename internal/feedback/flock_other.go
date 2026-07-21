//go:build !unix

package feedback

// lockSuffix names the sibling advisory-lock file that guards writes to the
// store.
const lockSuffix = ".lock"

// lockFile is a no-op where flock is unavailable: single-process use stays
// correct and cross-process writes fall back to last-writer-wins.
func lockFile(string) (func(), error) {
	return func() {}, nil
}
