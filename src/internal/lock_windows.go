package internal

// lockFile is no lock on Windows, where sessions don't run in tmux yet.
func lockFile(string) func() { return func() {} }
