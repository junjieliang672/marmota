package claudeask

// ChoiceArgs are the --model and --effort flags for the choices made in
// the Ask bar; none when Claude Code's own choice is kept.
func ChoiceArgs(model, effort string) []string {
	var args []string
	if model != "" {
		args = append(args, "--model", model)
	}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	return args
}

// Models are the models the Ask bar offers a Claude Code session, Claude
// Code's own choice first.
var Models = []string{"", "opus", "sonnet", "haiku", "fable"} //nolint:gochecknoglobals // fixed list

// Efforts are the effort levels the Ask bar offers, Claude Code's own
// choice first.
var Efforts = []string{"", "low", "medium", "high", "xhigh", "max"} //nolint:gochecknoglobals // fixed list

// PermissionModes are the modes a session the Ask bar starts can run in,
// bypass (Marmota's default) first.
//
//nolint:gochecknoglobals // fixed list
var PermissionModes = []string{"bypassPermissions", "acceptEdits", "auto", "plan", "manual"}

// PermissionModeLabel is how the bar names a permission mode. Transcripts
// record manual as "default", Claude Code's own name for it.
func PermissionModeLabel(mode string) string {
	switch mode {
	case "acceptEdits":
		return "accept edits"
	case "default", "manual":
		return "manual"
	case "", "bypassPermissions":
		return "bypass"
	default:
		return mode
	}
}

// PermissionArgs are the flags that start a session in a permission mode.
func PermissionArgs(mode string) []string {
	if mode == "" || mode == "bypassPermissions" {
		return []string{"--dangerously-skip-permissions"}
	}
	return []string{"--permission-mode", mode}
}
