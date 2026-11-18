//go:build !(darwin && cgo)

package words

const supported = false

// bounds has no words to give where the system isn't asked: callers keep
// their own rule (see Supported).
func bounds(string) [][2]int {
	return nil
}
