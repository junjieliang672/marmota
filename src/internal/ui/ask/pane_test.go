package ask

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// A turn is laid out once, but a reply that goes on as the session works is
// drawn as it is now, and so are its tools.
func TestATurnIsDrawnAgainWhenItGoesOn(t *testing.T) {
	s := Session{Title: "s", Turns: []claudesession.Turn{{Prompt: "fix it", Reply: "Looking."}}}
	PrepareTurns(s, 80)
	first := ansi.Strip(RenderSession(s, "", 80, 30, 0, true))
	if !strings.Contains(first, "Looking.") {
		t.Fatalf("the reply isn't drawn:\n%s", first)
	}
	s.Turns[0].Reply += "\n\nFixed in app.go."
	s.Turns[0].Tools = []string{"Edit(app.go)"}
	again := ansi.Strip(RenderSession(s, "", 80, 30, 0, true))
	if !strings.Contains(again, "Fixed in app.go.") || !strings.Contains(again, "Edit(app.go)") {
		t.Fatalf("what the turn went on with isn't drawn:\n%s", again)
	}
}
