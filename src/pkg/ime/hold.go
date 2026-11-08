// Package ime lets superfile tell the active input method to pass keys through
// as plain ASCII while superfile is in hotkey mode.
//
// # Why this exists
//
// With a CJK input method active, a single-letter hotkey never reaches
// superfile at all: the IME swallows it into its composition buffer upstream of
// the process. No amount of key filtering inside superfile can fix that, so the
// fix has to be a request to whatever owns the keyboard.
//
// # The ascii-hold protocol
//
// A client that wants pass-through creates a file named after its own PID in a
// shared directory, and unlinks it to release. A hold is in force as long as
// the directory holds at least one file named after a live process, so a client
// that is SIGKILLed is cleaned up by the reader with no daemon and no timeout.
//
// The file body is optional JSON naming the client bundle IDs the hold applies
// to:
//
//	{"apps": ["com.github.wez.wezterm"]}
//
// An empty body means "any app". superfile always writes a scope when it can
// identify the terminal, because superfile stays in hotkey mode even when it is
// not frontmost — an unscoped hold from a background tab would disable Chinese
// input everywhere.
//
// MarmotIM (github.com/junjieliang672/MarmotIM) implements the reader side.
// Input methods that do not know about the protocol simply ignore the files.
package ime

// Hold is a handle on superfile's ascii-hold request.
//
// The zero-value behaviour of every implementation is "do nothing": if the
// feature is disabled, or the platform has no input method to talk to, or the
// hold directory cannot be created, Set and Close are no-ops. A missing input
// method is not an error condition.
type Hold interface {
	// Set drives the hold to the requested state. It is idempotent and
	// returns immediately when the state is unchanged, which matters because
	// the caller invokes it on every Bubble Tea message, not just keystrokes.
	// Filesystem work never happens on the caller's goroutine.
	Set(active bool)

	// Close releases the hold and stops the worker. It blocks until the
	// release has actually landed, so that an exiting superfile does not
	// leave the input method in pass-through mode for the time it takes a
	// goroutine to get scheduled. Safe to call more than once.
	Close()
}

// noopHold is used when the feature is off, on platforms without an
// implementation, and whenever setup fails.
type noopHold struct{}

func (noopHold) Set(bool) {}
func (noopHold) Close()   {}

// New returns a Hold honouring the ascii-hold protocol, or an inert one when
// enabled is false, the platform is unsupported, or the hold directory is
// unusable. dir may be empty, in which case the platform default is used.
//
// New never returns nil and never returns an error: failing to reach an input
// method must not keep superfile from starting.
func New(enabled bool, dir string) Hold {
	if !enabled {
		return noopHold{}
	}
	return newPlatformHold(dir)
}
