package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/junjieliang672/marmota/src/internal"
)

// sessionCommand is `y session`: for a coding agent to start, list, write to
// and read the sessions Marmota runs in tmux, here or on another computer.
func sessionCommand() *cli.Command {
	return &cli.Command{
		Name: "session",
		Usage: "Start, list, write to and read Claude Code and Codex sessions in Marmota's tmux, here or (Claude " +
			"Code's) on another computer",
		Commands: []*cli.Command{
			{
				Name:      "start",
				Usage:     "Start a session; it shows in Marmota's Sessions box, unless it is --temporary",
				ArgsUsage: "[-- PROMPT]",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "agent", Usage: "cc for Claude Code (default), co for Codex, which " +
						"starts on this computer only"},
					&cli.StringFlag{Name: "on", Usage: `how to reach the computer, such as "ssh me@host" or ` +
						`"corp ssh gpu-1 --"; this computer when not given. Marmota remembers it`},
					&cli.StringFlag{Name: "cwd", Usage: "the folder to work in, ~ for that computer's home " +
						"(default: here, the current folder; there, home)"},
					&cli.StringFlag{Name: "model", Usage: "the agent's --model"},
					&cli.StringFlag{Name: "effort", Usage: "Claude Code's --effort; Codex's effort, medium when not given"},
					&cli.StringFlag{Name: "permission-mode", Usage: "bypassPermissions (default), acceptEdits, auto, " +
						"plan or default; Codex runs in its nearest to each"},
					&cli.BoolFlag{Name: "temporary", Usage: "a session for the program that runs this, as the " +
						"editor's for a question asked from it: Marmota's Sessions box and `y session list` leave " +
						"it out, and it closes when that program does. On this computer only"},
					&cli.BoolFlag{Name: "json", Usage: "print the session as JSON"},
				},
				Action: func(_ context.Context, c *cli.Command) error {
					started, err := internal.SessionStart(internal.SessionStartOptions{
						Agent: c.String("agent"), On: c.String("on"), Cwd: c.String("cwd"),
						Prompt: strings.Join(c.Args().Slice(), " "),
						Model:  c.String("model"), Effort: c.String("effort"), Mode: c.String("permission-mode"),
						// The program that ran this is the one a temporary session closes with.
						Temporary: c.Bool("temporary"), Owner: os.Getppid(),
					})
					if err != nil {
						return cli.Exit("y session start: "+err.Error(), 1)
					}
					if c.Bool("json") {
						return json.NewEncoder(os.Stdout).Encode(started)
					}
					where := "this computer"
					if started.Host != "" {
						where = started.Host
					}
					fmt.Printf("started %s on %s in %s\n", started.ID, where, started.Cwd)
					return nil
				},
			},
			{
				Name: "adopt",
				Usage: "Move sessions running on another computer outside Marmota into its tmux there: each " +
					"stops where it runs, leaving its terminal at the shell, and goes on with the same " +
					"conversation in Marmota",
				ArgsUsage: "[NAME or ID]...",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "on", Usage: `how to reach the computer, such as "ssh me@host"`},
					&cli.BoolFlag{Name: "list", Usage: "only list the sessions that could be moved"},
					&cli.BoolFlag{Name: "json", Usage: "print JSON"},
				},
				Action: func(_ context.Context, c *cli.Command) error {
					adopted, err := internal.SessionAdopt(c.String("on"), c.Args().Slice(), c.Bool("list"))
					if err != nil {
						return cli.Exit("y session adopt: "+err.Error(), 1)
					}
					if c.Bool("json") {
						return json.NewEncoder(os.Stdout).Encode(adopted)
					}
					if len(adopted) == 0 {
						fmt.Println("no session runs there outside Marmota")
						return nil
					}
					failed := false
					for _, a := range adopted {
						switch {
						case c.Bool("list"):
							fmt.Printf("%s  %-12s %-8s %s  (%s)\n", a.ID, a.Name, a.Status, a.Title, a.Cwd)
						case a.Moved:
							fmt.Printf("moved %s (%s) into Marmota\n", a.Name, a.ID)
						default:
							failed = true
							fmt.Printf("couldn't move %s (%s): %s\n", a.Name, a.ID, a.Error)
						}
					}
					if failed {
						return cli.Exit("", 1)
					}
					return nil
				},
			},
			{
				Name:  "list",
				Usage: "List the sessions in Marmota's tmux and what each is doing",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "json", Usage: "print JSON"},
					&cli.BoolFlag{Name: "rows", Usage: "print rows for a picker such as fzf (--ansi, tab-delimited): " +
						"the id, agent, host and title, then the row to show, in color"},
					&cli.StringFlag{Name: "new-in", Usage: "with --rows: first a row for each agent that stands " +
						"for a session not started yet, to start in this folder"},
					&cli.BoolFlag{Name: "local", Usage: "only this computer's sessions, without asking the others"},
				},
				Action: func(_ context.Context, c *cli.Command) error {
					sessions, failed := internal.SessionList(c.Bool("local"))
					if c.Bool("rows") {
						return internal.WriteSessionRows(os.Stdout, sessions, c.String("new-in"))
					}
					return internal.WriteSessionList(os.Stdout, sessions, failed, c.Bool("json"))
				},
			},
			{
				Name: "pick",
				Usage: "Pick a session in fzf, each row in its agent's color, and print the row picked: the id, " +
					"agent, host and title, tab-separated. Exits with 1 when none is picked",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "new-in", Usage: "first a row for each agent that stands for a session " +
						"not started yet, to start in this folder: its id is new"},
					&cli.StringFlag{Name: "prompt", Value: "Which session? ", Usage: "what the picker asks"},
				},
				Action: func(_ context.Context, c *cli.Command) error {
					row, err := internal.SessionPick(c.String("new-in"), c.String("prompt"))
					if errors.Is(err, internal.ErrNothingPicked) {
						return cli.Exit("", 1)
					}
					if err != nil {
						return cli.Exit("y session pick: "+err.Error(), 1)
					}
					fmt.Println(row)
					return nil
				},
			},
			{
				Name:      "send",
				Usage:     "Send a session a message: typed into its prompt, or for Codex through its server",
				ArgsUsage: "ID MESSAGE...",
				Action: func(_ context.Context, c *cli.Command) error {
					if c.Args().Len() < 2 {
						return cli.Exit("y session send: give the session's id and the message", 1)
					}
					if err := internal.SessionSend(c.Args().First(), strings.Join(c.Args().Tail(), " ")); err != nil {
						return cli.Exit("y session send: "+err.Error(), 1)
					}
					return nil
				},
			},
			{
				Name: "ask",
				Usage: "Send a message to a session and print its reply, once the turn the message started has ended. " +
					"Exits with 3 when the session stopped to ask you something instead",
				ArgsUsage: "ID MESSAGE...",
				Flags: []cli.Flag{
					&cli.DurationFlag{Name: "timeout", Value: internal.SessionAskTimeout,
						Usage: "how long to wait for the reply"},
					&cli.BoolFlag{Name: "sent", Usage: "the message is already the session's, such as the one " +
						"it was started with: only wait for its reply"},
					&cli.BoolFlag{Name: "keep-waiting", Usage: "go on waiting when the session stops to ask you " +
						"something, for you to answer it, instead of exiting with 3"},
				},
				Action: func(_ context.Context, c *cli.Command) error {
					if c.Args().Len() < 2 {
						return cli.Exit("y session ask: give the session's id and the message", 1)
					}
					reply, err := internal.SessionAsk(c.Args().First(), strings.Join(c.Args().Tail(), " "),
						c.Duration("timeout"), c.Bool("sent"), c.Bool("keep-waiting"))
					if needs, ok := internal.IsNeedsYou(err); ok {
						if needs.Reply != "" {
							fmt.Println(needs.Reply)
						}
						return cli.Exit("y session ask: "+err.Error(), 3) //nolint:mnd // its own exit status
					}
					if err != nil {
						return cli.Exit("y session ask: "+err.Error(), 1)
					}
					fmt.Println(reply)
					return nil
				},
			},
			{
				Name:      "read",
				Usage:     "Print a session's last turns",
				ArgsUsage: "ID",
				Flags:     []cli.Flag{&cli.IntFlag{Name: "turns", Value: 3, Usage: "how many turns"}},
				Action: func(_ context.Context, c *cli.Command) error {
					if c.Args().Len() != 1 {
						return cli.Exit("y session read: give the session's id", 1)
					}
					if err := internal.SessionRead(os.Stdout, c.Args().First(), int(c.Int("turns"))); err != nil {
						return cli.Exit("y session read: "+err.Error(), 1)
					}
					return nil
				},
			},
			{
				Name: "show",
				Usage: "Open a session's own window in a pane under this one (WezTerm), to watch it or answer " +
					"what it asks; closing the pane leaves the session running",
				ArgsUsage: "ID",
				Action: func(_ context.Context, c *cli.Command) error {
					if c.Args().Len() != 1 {
						return cli.Exit("y session show: give the session's id", 1)
					}
					shown, err := internal.SessionShow(c.Args().First())
					if err != nil {
						return cli.Exit("y session show: "+err.Error(), 1)
					}
					fmt.Printf("showing %s in pane %d\n", shown.ID, shown.Pane)
					return nil
				},
			},
		},
	}
}
