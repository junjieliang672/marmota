package common

// Placeholder inteface for now, might later move 'model' type to commons and have
// and add an execute(model) function to this
type ModelAction interface {
	String() string
}

type NoAction struct {
}

func (n NoAction) String() string {
	return "NoAction"
}

type ShellCommandAction struct {
	Command string
}

func (s ShellCommandAction) String() string {
	return "ShellCommandAction for command " + s.Command
}

// We could later move 'model' type to commons and have
// these actions implement an execute(model) interface
type CDCurrentPanelAction struct {
	Location string
}

func (c CDCurrentPanelAction) String() string {
	return "CDCurrentPanelAction to " + c.Location
}

type OpenPanelAction struct {
	Location string
}

func (o OpenPanelAction) String() string {
	return "OpenPanelAction at " + o.Location
}

// ClaudeAction opens Claude Code in a new WezTerm pane via the managed WezTerm
// layout, with --dangerously-skip-permissions. RemoteControl additionally
// passes --remote-control so the session is reachable from the Claude app.
type ClaudeAction struct {
	RemoteControl bool
}

func (c ClaudeAction) String() string {
	if c.RemoteControl {
		return "ClaudeAction (smart-layout, skip-permissions, remote-control)"
	}
	return "ClaudeAction (smart-layout, skip-permissions)"
}

// CodexAction opens OpenAI Codex CLI in a new WezTerm pane via the managed
// WezTerm layout, with full-access sandboxing and on-request approvals.
// No configuration — behaviour is constant.
type CodexAction struct{}

func (c CodexAction) String() string {
	return "CodexAction (smart-layout, danger-full-access, on-request)"
}
