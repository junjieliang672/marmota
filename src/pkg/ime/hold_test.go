package ime

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewDisabledIsInert(t *testing.T) {
	// A disabled hold must not even look at the directory, so a path that
	// could never be created is still fine.
	h := New(false, "/dev/null/nope")
	assert.NotNil(t, h, "New must never return nil")

	h.Set(true)
	h.Set(false)
	h.Close()
	h.Close() // Close is documented as safe to call twice.
}
