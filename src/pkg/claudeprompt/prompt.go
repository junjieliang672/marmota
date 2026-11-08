// Package claudeprompt reads Claude Code's prompt box off a terminal screen,
// so that text is only typed into a session that is waiting for it, and what
// the person has typed there is known and kept.
package claudeprompt

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Box is Claude Code's prompt box as the screen shows it.
type Box struct {
	// Shown is false when the screen has no prompt box, as while Claude asks
	// a question or shows a menu: nothing may be typed then.
	Shown bool
	// Text is what is typed in the box, its rows joined by newlines. It is
	// "" when the box is empty, whether or not it shows its placeholder.
	Text string
	// Stashed reports a prompt set aside with ctrl+s ("› stashed" above the
	// box). Claude Code puts it back after the next prompt is sent.
	Stashed bool
}

const (
	// The prompt row starts with the marker and a no-break space; your
	// earlier prompts, higher up in the conversation, have a plain space.
	promptStart = "❯\u00a0"
	// Rows a prompt wraps onto are indented by two spaces.
	continuation  = "  "
	stashedMarker = "› stashed"
	minRuleRunes  = 8
	// The prompt row and the rule under it follow the top rule.
	rowsUnderTopRule = 2
)

// pasteSummary is how the box shows a long paste: "[Pasted text #1 +11 lines]".
var pasteSummary = regexp.MustCompile(`\[Pasted text #\d+[^\]]*\]`)

// Read finds the prompt box in screen, as `wezterm cli get-text --escapes`
// prints it: a rule, the row starting with "❯", the rows the prompt wraps
// onto, and a rule. Claude Code draws its placeholder dim, and a box showing
// only that is empty.
func Read(screen string) Box {
	raw := strings.Split(screen, "\n")
	plain := make([]string, len(raw))
	for i := range raw {
		raw[i] = strings.TrimRight(raw[i], "\r")
		plain[i] = strings.TrimRight(ansi.Strip(raw[i]), " ")
	}
	// The box is the last one on screen. A question or a menu takes its
	// place, and then there is none.
	for top := len(plain) - rowsUnderTopRule; top >= 0; top-- {
		first := top + 1
		if !isRule(plain[top]) || !isPromptRow(plain[first]) {
			continue
		}
		bottom := closingRule(plain, first)
		if bottom < 0 {
			continue
		}
		lines := []string{strings.TrimPrefix(plain[first], promptStart)}
		for _, row := range plain[first+1 : bottom] {
			lines = append(lines, strings.TrimPrefix(row, continuation))
		}
		box := Box{Shown: true, Text: strings.TrimRightFunc(strings.Join(lines, "\n"), unicode.IsSpace)}
		if isPlaceholder(raw[first:bottom]) {
			box.Text = ""
		}
		box.Stashed = top > 0 && strings.Contains(plain[top-1], stashedMarker)
		return box
	}
	return Box{}
}

// Holds reports how text pasted into the box shows in what the box holds:
// exactly as pasted, or among other text. A paste of several lines may show
// as a summary instead, "[Pasted text #1 +11 lines]".
// It returns exactly, then within.
func Holds(boxText, pasted string) (bool, bool) {
	if summary := pasteSummary.FindString(boxText); summary != "" && strings.Contains(pasted, "\n") {
		return strings.TrimSpace(boxText) == summary, true
	}
	got, want := squash(boxText), squash(pasted)
	if want == "" {
		return got == "", false
	}
	return got == want, strings.Contains(got, want)
}

// squash drops all spaces, so text compares the same however the box wraps it.
func squash(s string) string {
	return strings.Join(strings.FieldsFunc(s, unicode.IsSpace), "")
}

func isPromptRow(row string) bool {
	return strings.HasPrefix(row, promptStart) || row == "❯"
}

// isRule reports a row of box-drawing lines. The top rule of the prompt box
// may carry the session's name: "──────── fix login ─".
func isRule(row string) bool {
	row = strings.TrimSpace(row)
	n := utf8.RuneCountInString(row)
	if n < minRuleRunes || !strings.HasPrefix(row, "──") || !strings.HasSuffix(row, "─") {
		return false
	}
	return strings.Count(row, "─")*2 >= n
}

// closingRule returns the rule under a prompt row and the rows it wraps
// onto, or -1 when another row comes first.
func closingRule(rows []string, first int) int {
	for i := first + 1; i < len(rows); i++ {
		switch {
		case isRule(rows[i]):
			return i
		case rows[i] != "" && !strings.HasPrefix(rows[i], continuation):
			return -1
		}
	}
	return -1
}

// isPlaceholder reports whether everything after the marker of a prompt is
// drawn dim, as Claude Code draws the hint in an empty box, over however many
// rows it wraps onto.
func isPlaceholder(rows []string) bool {
	return dimAfterMarker(strings.Join(rows, "\n"))
}

func dimAfterMarker(row string) bool {
	dim, afterMarker, sawText := false, false, false
	for i := 0; i < len(row); {
		if row[i] == ansi.ESC {
			n, params, isSGR := escape(row[i:])
			if isSGR {
				dim = applySGR(dim, params)
			}
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(row[i:])
		i += size
		switch {
		case !afterMarker:
			afterMarker = r == '❯'
		case unicode.IsSpace(r):
		case !dim:
			return false
		default:
			sawText = true
		}
	}
	return sawText
}

// escape measures the escape sequence s starts with, and returns its
// parameters when it sets colors and attributes ("\x1b[0;2m").
// It returns the length, the parameters and whether it is SGR.
func escape(s string) (int, string, bool) {
	const minLen = 2
	if len(s) < minLen {
		return len(s), "", false
	}
	switch s[1] {
	case '[':
		for i := 2; i < len(s); i++ {
			if c := s[i]; c >= 0x40 && c <= 0x7e {
				return i + 1, s[2:i], c == 'm'
			}
		}
		return len(s), "", false
	case ']': // up to BEL or ESC \
		if end := strings.IndexAny(s, "\a"); end >= 0 {
			return end + 1, "", false
		}
		if end := strings.Index(s, "\x1b\\"); end >= 0 {
			return end + minLen, "", false
		}
		return len(s), "", false
	case '(', ')', '*', '+': // a character set: one more byte
		return min(len(s), minLen+1), "", false
	default:
		return minLen, "", false
	}
}

// applySGR returns whether text is dim after the attributes in params.
func applySGR(dim bool, params string) bool {
	fields := strings.Split(params, ";")
	for i := 0; i < len(fields); i++ {
		switch fields[i] {
		case "", "0", "22":
			dim = false
		case "2":
			dim = true
		case "38", "48", "58":
			// A color follows: 5;n, or 2;r;g;b. WezTerm writes it with
			// colons instead, as one field.
			if i+1 < len(fields) {
				switch fields[i+1] {
				case "5":
					i += 2
				case "2":
					i += 4
				}
			}
		}
	}
	return dim
}
