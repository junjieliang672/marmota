package notify

type ConfirmActionType int

const (
	RenameAction ConfirmActionType = iota
	DeleteAction
	QuitAction
	NoAction
	PermanentDeleteAction
	SudoDeleteAction
	// StopClaudeSessionAction stops the session picked in the Sessions sheet.
	StopClaudeSessionAction
)
