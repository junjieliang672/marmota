# Marmota's keys in micro

cmd+/ opens this page, ctrl+q closes it (and any file or tab).

## Ask a session

| Key | Does |
|-----|------|
| cmd+L | Ask the session this editor writes to, about this file: type the question, enter. The file is saved, and named to the session with the lines you selected, so it reads them itself. Go on writing: its reply shows on the right when its turn has ended |
| cmd+shift+L | Pick the session questions go to, then ask it. The first row is a session not started yet: your question starts it, in this editor's folder, and the next ones go on with it |
| cmd+K | Have the session change the selected lines, or with none the file, as you ask, in the file itself: the editor shows the change when it is made, and cmd+Z takes it back |
| cmd+enter | Put the last reply at the cursor, or in place of what is selected; cmd+Z takes it back |
| cmd+J | Show the replies on the right, or put them away |

The session is the one Marmota's Ask box wrote to when it opened the editor,
until you pick another. It is one of Marmota's sessions: what you ask here
shows in its Sessions box, where you can go on with it.

You needn't wait for a reply before you ask again, the same session or
another: each question waits for its own reply, and the replies show under
their questions as they come. The status line says whom questions go to and
how many replies are still awaited: "→ fix login  ✳ 2 asking". A session that
stops to ask you something (a permission, or whether to trust its folder)
says so in place of its reply: answer it from Marmota's Sessions box, and the
reply, or the change, comes. While a session changes the file, what you type
into it may give way to its change when that comes in: cmd+Z brings yours
back. A session on another computer can't read or change the file: it is
sent the selected text itself, and after cmd+K replies with the new text
for cmd+enter to put in.

## Find and jump

| Key | Does |
|-----|------|
| cmd+F | Find in this file (a regular expression). enter, then ctrl+N for the next match, ctrl+P the one before; esc ends it |
| cmd+option+F | Fuzzy search the lines of this file, with a preview |
| cmd+shift+F | Search every file under the folder (asks where, then what) |
| cmd+P | Open a file by name |
| cmd+shift+O | Jump to a heading or a function of this file |
| cmd+G | Go to a place: a line's number and enter, or one key (below) |
| cmd+shift+G | Select up to a place: the same keys |
| ctrl+- | Back to where the cursor was before a jump; ctrl+shift+- forward again |
| cmd+↑ / cmd+↓ | Start / end of the file |
| cmd+← / cmd+→ | Start / end of the line |
| option+← / option+→ | A word left / right |

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

## Select and copy

| Key | Does |
|-----|------|
| shift + any key that moves | Select as it moves: shift+option+→ a word, shift+cmd+→ to the line's end |
| shift+option+↓ / ↑ | Select the paragraph; again, the next one too |
| cmd+A | Select all |
| cmd+C | Copy what is selected; with nothing selected, the line |
| cmd+X | Cut what is selected; with nothing selected, the line |
| cmd+V | Paste |
| the mouse | Click puts the cursor, drag selects, double click a word, triple click a line. The wheel, turned while you drag, scrolls and the selection goes on |

## Edit

| Key | Does |
|-----|------|
| cmd+S | Save |
| cmd+Z / cmd+shift+Z | Undo / redo |
| option+backspace | Delete the word before the cursor |
| cmd+backspace | Delete back to the start of the line |
| option+↑ / option+↓ | Move the line up / down |
| ctrl+D | Duplicate the line |
| tab / shift+tab | Indent / outdent what is selected |

## Files, tabs and panes

| Key | Does |
|-----|------|
| ctrl+Q | Close this file; the last one closes micro |
| option+, / option+. | The tab before / after |
| option+W | The next pane of a split |
| option+X | micro's command bar: `help keybindings` lists every key, `help commands` every command |

In Marmota, e opens a file here and E in a new tab. The keys are set in
~/.config/micro/bindings.json; the cmd keys are sent by Marmota's block in
~/.wezterm.lua, only while micro is the pane's program.
