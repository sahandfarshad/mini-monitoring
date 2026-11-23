//go:build !windows

package core

// Non-Windows builds (development only) keep secrets as-is.
func protect(b []byte) ([]byte, error)   { return b, nil }
func unprotect(b []byte) ([]byte, error) { return b, nil }
