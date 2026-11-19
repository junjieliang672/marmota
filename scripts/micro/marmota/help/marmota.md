# Keys in micro

cmd+H shows this page and puts it away. cmd+W closes what the cursor is in:
this page, a file, a tab; the last file closes micro. "option" is the left
option key.

## Files, tabs and panes

| Key | Does |
|-----|------|
| cmd+S | Save |
| cmd+W | Close this file, tab or pane; it asks first when the file isn't saved. The last one closes micro. ctrl+Q does the same |
| cmd+P | Open a file by name |
| ctrl+O | Open a file by its path; tab completes it |
| ctrl+T | A new tab |
| option+, / option+. | The tab before / after |
| option+W | The next pane of a split |
| cmd+H | This page, shown or put away |
| cmd+M | Put micro away and go back to Marmota: micro stops with its files, tabs and replies as they are, and `b` in Marmota brings it back. Only where `e` in Marmota opened it |
| option+X | The command bar (at the end of this page) |

## Edit

| Key | Does |
|-----|------|
| cmd+Z / cmd+shift+Z | Undo / redo |
| cmd+/ | Comment the line or the selected lines; again takes the comment off |
| option+↑ / option+↓ | Move the line, or the selected lines, up / down |
| ctrl+D | Duplicate the line, or what is selected |
| tab / shift+tab | Indent / outdent what is selected. While you type a word, tab completes it from the words of the file |
| option+backspace | Delete the word before the cursor: what option+← would go over. fn+option+backspace deletes the word after it |
| cmd+backspace | Delete back to the start of the line |
| ctrl+K | Cut the line |

## Select and copy

| Key | Does |
|-----|------|
| shift + any key that moves | Select as it moves: shift+option+→ a word, shift+cmd+→ to the line's end |
| shift+option+↓ / ↑ | Select the paragraph; again, the next one too |
| cmd+shift+G | Select up to a place: a line, the top, a paragraph's end, what brackets hold (below) |
| cmd+A | Select all |
| cmd+C | Copy what is selected; with nothing selected, the line |
| cmd+shift+C | Copy the file and the selected lines (or the cursor's line) as `@src/app.go#L3-9`, to paste in a session |
| cmd+X | Cut what is selected; with nothing selected, the line |
| cmd+V | Paste |
| the mouse | Click puts the cursor, drag selects, double click a word, triple click a line. The wheel, turned while you drag, scrolls and the selection goes on |

## Several cursors

| Key | Does |
|-----|------|
| option+N | Select the word at the cursor; again, its next occurrence too, with a cursor of its own. What you type goes to each |
| option+M | A cursor on each line of what is selected |
| ctrl+click | One more cursor where you click |
| esc | Back to one cursor |

## Move

| Key | Does |
|-----|------|
| cmd+← / cmd+→ | Start / end of the line (ctrl+A / ctrl+E too) |
| option+← / option+→ | A word left / right. In a line of Chinese, Japanese or Korean the words are the system's, as in any text field of the Mac: 我认为应该这样 is 我, 认为, 应该, 这样 |
| cmd+↑ / cmd+↓ | Start / end of the file |
| page up / page down | A screen up / down |
| cmd+G | Go to a place: a line's number and enter, or one key (below) |
| ctrl+- | Back to where the cursor was before a jump; ctrl+shift+- forward again |
| option+] / option+[ | The next / previous lines changed since the last commit, or outside git since the file was opened (marked at the left edge) |

After cmd+G, one key goes there at once; after cmd+shift+G it selects, from
the cursor or the end of what is selected:

| Key | cmd+G goes to | cmd+shift+G selects |
|-----|---------------|---------------------|
| a number, enter | That line | Through that line; from its start when it is above |
| t / b | The file's top / bottom | To the top / bottom |
| p | The next paragraph's start | To the paragraph's end; again, the next one's |
| P | The paragraph's start; from there, the one before | To the paragraph's start; again, the one before |
| m | The bracket that matches the one at the cursor, else the one that opens around it | What the brackets around the cursor hold; again, the brackets too; again, what the pair around them holds |
| n | Back to where the last of these jumps left from; again, the one before | To that place |
| g | Keeps the line open: each key goes on from the last, until esc | The same |

Brackets are ( ) [ ] { }, counted as written: one inside a string counts too.

## Find

| Key | Does |
|-----|------|
| cmd+F | Find in this file (a regular expression). enter, then ctrl+N for the next match, ctrl+P the one before; esc ends it |
| cmd+option+F | Fuzzy search the lines of this file, with a preview |
| cmd+shift+F | Search every file under the folder (asks where, then what) |
| cmd+shift+O | Jump to a heading or a function of this file |

## Ask a session

| Key | Does |
|-----|------|
| cmd+L | Ask the session about this file and the lines you selected: the cursor goes to the box at the foot of the column on the right. Write the question; enter sends it and the cursor is back in the file. The reply shows above the box when the session's turn has ended. From the box, cmd+L goes back to the file |
| cmd+shift+L | Pick the session questions go to: this computer's sessions are listed above the box, Claude Code's rows starting with ✳ and Codex's with ◆, each in its color, with what the session does (working, idle, needs you), its title and its folder. What you type in the box filters them, a row staying when it has each word; ↑ ↓ move the mark, enter picks, esc leaves the list. The first two rows start a new session with your question: a Claude Code session, a Codex session |
| cmd+K | Have the session change the selected lines, or with none the file: the box takes what it is to change; cmd+Z takes the change back |
| enter, shift+enter, esc (in the box) | Send, or pick the marked session; a new line; back to the file, with what is written kept |
| cmd+enter | Put the last reply at the cursor, or in place of what is selected |
| cmd+J | Show the column, or put it away: what was asked and what the box holds are there again |
| enter (in the replies) | Back to the file, with the lines the question was about selected: where they are now when lines went in above them, those the session changed for cmd+K. The line that says whom says so too: "plan.md L3-4 · now at L5-6", "lines changed since", "changed L3-5". Reach the replies with a click or option+W |
| cmd+O | Open the session in a pane under the editor: the one that stopped to ask you something, else the one questions go to. Answer it there, or watch it work; closing the pane leaves it running |

The session is the one Marmota's Ask box wrote to when it opened the editor,
until you pick another. It is one of Marmota's sessions: what you ask here
shows in its Sessions box, where you can go on with it. The file is saved
when you ask, and named to the session with the selected lines, so it reads
them itself.

You needn't wait for a reply before you ask again, the same session or
another: each question waits for its own reply, and the replies show under
their questions as they come. What you asked is highlighted, under a line
that says whom; the reply starts under the session's name. The status line says whom questions go to and
how many replies are still awaited: "→ fix login  ✳ 2 asking". A session that
stops to ask you something (a permission, or whether to trust its folder)
says so in place of its reply: answer it from Marmota's Sessions box, or open
it with cmd+O, and the reply, or the change, comes. While a session changes the file, what you type
into it may give way to its change when that comes in: cmd+Z brings yours
back. The sessions listed are this computer's: one on another computer is
asked from a terminal, with `y session ask`.

The column shows in the tab you ask from: asked from another tab, it comes
along, with what was asked. The line over the box names the box's keys, and
the one under it whom the question goes to and what it is about: "Ask fix
login · plan.md L3-4".

A Codex session is asked through Codex's own server, and its reply is its
answer, without what it said as it worked. A new one starts with full
access, at medium effort, as Marmota's Ask box starts one.

A session a question starts from here is this editor's own: Marmota's
Sessions box doesn't list it, and it closes when the editor does. One that
stops to ask you something says so in the replies: cmd+O opens it.

## The command bar

option+X opens it at the foot; type a command, enter. tab completes
commands and file names, ↑ brings back the ones you ran.

| Command | Does |
|---------|------|
| `replace old new` | Replace, asking at each match. `replace old new -a` replaces all at once; `-l` takes old as plain text, not a regular expression |
| `vsplit file` / `hsplit file` | Open a file beside / below this one |
| `tab file` | Open a file in a new tab |
| `set softwrap off` | Change a setting until micro closes; `help options` lists them |
| `help keybindings` / `help commands` | Every key / every command |

ctrl+B runs a shell command and shows what it prints.

In Marmota, e opens a file here, in Marmota's place until you close it
(cmd+W), and E in a new tab. Claude Code's ctrl+G opens its prompt here too
($EDITOR, set in ~/.zshenv). The keys are set in
~/.config/micro/bindings.json; the cmd keys are sent by Marmota's block in
~/.wezterm.lua, only while micro is the pane's program: elsewhere cmd+W
closes WezTerm's tab and cmd+H hides WezTerm.
