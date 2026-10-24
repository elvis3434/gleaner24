//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly || windows)

package lock

import "os"

// Platforms without a supported file lock run unguarded.
func lockFile(*os.File) error   { return nil }
func unlockFile(*os.File) error { return nil }
