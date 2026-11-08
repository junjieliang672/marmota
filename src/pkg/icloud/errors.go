package icloud

import "errors"

var (
	// ErrNotAvailable is returned when iCloud operations are not available.
	ErrNotAvailable = errors.New("iCloud operations are not available on this platform")
)
