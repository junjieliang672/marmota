package sessionhost

import (
	"encoding/base64"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ParseOn reads how to reach a computer, as `y session start --on` takes it:
// a command whose words run a command over there. "ssh [options] dest"
// becomes an ssh host, so it shares one connection; anything else is a
// command the remote command follows, such as "corp ssh gpu-1 --".
func ParseOn(on string) (Host, error) {
	words, err := splitWords(on)
	if err != nil {
		return Host{}, err
	}
	if len(words) == 0 {
		return Host{}, errors.New("--on says nothing: give the command that reaches it, such as \"ssh me@host\"")
	}
	if path.Base(words[0]) == "ssh" {
		dest := ""
		var opts []string
		for i := 1; i < len(words); i++ {
			w := words[i]
			if strings.HasPrefix(w, "-") {
				opts = append(opts, w)
				// Options that take a value: the next word is the value.
				if len(w) == 2 && strings.ContainsRune("bcDEeFIiJLlmOoPpQRSWw", rune(w[1])) && i+1 < len(words) {
					i++
					opts = append(opts, words[i])
				}
				continue
			}
			if dest != "" {
				return Host{}, fmt.Errorf("--on %q: give ssh and where to go, not a command to run there", on)
			}
			dest = w
		}
		if dest == "" {
			return Host{}, fmt.Errorf("--on %q names no destination", on)
		}
		return Host{Name: shortName(dest), SSH: dest, SSHArgs: opts}, nil
	}
	return Host{Name: shortName(words[len(words)-1]), Command: words}, nil
}

// shortName is a name to show for a destination: "me@192.168.1.20" is
// "192.168.1.20" until the computer says its own name.
func shortName(dest string) string {
	if i := strings.LastIndexByte(dest, '@'); i >= 0 {
		dest = dest[i+1:]
	}
	if dest == "--" || dest == "" {
		return "remote"
	}
	return dest
}

// splitWords splits a command line as a shell would: words apart by spaces,
// in single or double quotes, with backslashes.
func splitWords(s string) ([]string, error) {
	var words []string
	var cur strings.Builder
	inWord := false
	var quote rune
	escaped := false
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, inWord = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case unicode.IsSpace(r):
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 || escaped {
		return nil, fmt.Errorf("%q has an open quote", s)
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words, nil
}

// shellTimeout is how long a shell command over there may take.
const shellTimeout = 30 * time.Second

// Hostname is the computer's short name, "nova".
func (c *Client) Hostname() (string, error) {
	out, err := c.Shell("hostname -s 2>/dev/null || hostname", shellTimeout)
	return strings.TrimSpace(out), err
}

// Agents is what `claude agents --json` prints over there.
func (c *Client) Agents() ([]byte, error) {
	out, err := c.Shell("claude agents --json", shellTimeout)
	return []byte(out), err
}

// TranscriptPath finds a Claude Code session's transcript over there, or "".
func (c *Client) TranscriptPath(id string) (string, error) {
	out, err := c.Shell(`for f in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}"/projects/*/`+shellQuote(id)+
		`.jsonl; do [ -f "$f" ] && { printf '%s\n' "$f"; break; }; done; true`, shellTimeout)
	return strings.TrimSpace(out), err
}

// readLimit is the most of a file one ReadFrom brings.
const readLimit = 4 << 20

// ReadFrom returns a file's bytes from offset on, at most readLimit of them,
// and the file's size. A file that shrank comes back from its start.
func (c *Client) ReadFrom(file string, offset int64) ([]byte, int64, error) {
	q := shellQuote(file)
	out, err := c.Shell(`s=$(wc -c < `+q+` | tr -d ' '); echo "$s"; o=`+strconv.FormatInt(offset, 10)+
		`; [ "$o" -gt "$s" ] && o=0; echo "$o"; tail -c +$((o + 1)) `+q+` | head -c `+strconv.Itoa(readLimit)+
		` | base64 | tr -d '\n'; echo`, shellTimeout)
	if err != nil {
		return nil, 0, err
	}
	lines := strings.SplitN(out, "\n", 3)
	if len(lines) < 3 {
		return nil, 0, fmt.Errorf("unexpected answer reading %s", file)
	}
	size, err := strconv.ParseInt(strings.TrimSpace(lines[0]), 10, 64)
	if err != nil {
		return nil, 0, fmt.Errorf("reading %s: %w", file, err)
	}
	from, _ := strconv.ParseInt(strings.TrimSpace(lines[1]), 10, 64)
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[2]))
	if err != nil {
		return nil, 0, fmt.Errorf("reading %s: %w", file, err)
	}
	if from != offset {
		return data, size, ErrShrank
	}
	return data, size, nil
}

// ErrShrank says a file read from an offset was shorter than that: what came
// back is from its start.
var ErrShrank = errors.New("the file is shorter than before")
