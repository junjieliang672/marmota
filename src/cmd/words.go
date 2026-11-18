package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/junjieliang672/marmota/src/pkg/words"
)

// wordsCommand is `y words`: where the words of a line start and end, as
// the system finds them, for an editor that can't ask it itself. micro's
// marmota plugin moves and deletes by word with it in lines of Chinese.
func wordsCommand() *cli.Command {
	return &cli.Command{
		Name:      "words",
		Usage:     "Print where the words of a line start and end, as the system finds them",
		ArgsUsage: "TEXT",
		Hidden:    true,
		Action: func(_ context.Context, c *cli.Command) error {
			if !words.Supported() {
				return cli.Exit("y words: this system isn't asked for words", 1)
			}
			found := words.Bounds(strings.Join(c.Args().Slice(), " "))
			out := make([]string, len(found))
			for i, w := range found {
				out[i] = fmt.Sprintf("%d,%d", w[0], w[1])
			}
			fmt.Println(strings.Join(out, " "))
			return nil
		},
	}
}
