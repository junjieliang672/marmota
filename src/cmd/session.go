package cmd

import (
	"context"
	"encoding/json"
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
		Name:  "session",
		Usage: "Start, list, write to and read Claude Code sessions in Marmota's tmux, here or on another computer",
		Commands: []*cli.Command{
			{
				Name:      "start",
				Usage:     "Start a session; it shows in Marmota's Sessions box",
				ArgsUsage: "[-- PROMPT]",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "on", Usage: `how to reach the computer, such as "ssh me@host" or ` +
						`"corp ssh gpu-1 --"; this computer when not given. Marmota remembers it`},
					&cli.StringFlag{Name: "cwd", Usage: "the folder to work in, ~ for that computer's home " +
						"(default: here, the current folder; there, home)"},
					&cli.StringFlag{Name: "model", Usage: "Claude Code's --model"},
					&cli.StringFlag{Name: "effort", Usage: "Claude Code's --effort"},
					&cli.StringFlag{Name: "permission-mode", Usage: "bypassPermissions (default), acceptEdits, auto, " +
						"plan or default"},
					&cli.BoolFlag{Name: "json", Usage: "print the session as JSON"},
				},
				Action: func(_ context.Context, c *cli.Command) error {
					started, err := internal.SessionStart(internal.SessionStartOptions{
						On: c.String("on"), Cwd: c.String("cwd"), Prompt: strings.Join(c.Args().Slice(), " "),
						Model: c.String("model"), Effort: c.String("effort"), Mode: c.String("permission-mode"),
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
				Name:  "list",
				Usage: "List the sessions in Marmota's tmux and what each is doing",
				Flags: []cli.Flag{&cli.BoolFlag{Name: "json", Usage: "print JSON"}},
				Action: func(_ context.Context, c *cli.Command) error {
					sessions, failed := internal.SessionList()
					return internal.WriteSessionList(os.Stdout, sessions, failed, c.Bool("json"))
				},
			},
			{
				Name:      "send",
				Usage:     "Type a message into a session's prompt and send it",
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
		},
	}
}
