//go:build !darwin

package ime

// newPlatformHold returns an inert hold. The ascii-hold protocol is currently
// only implemented by a macOS input method; on other platforms the reader side
// does not exist, so writing the files would be pure noise.
func newPlatformHold(_ string) Hold {
	return noopHold{}
}
