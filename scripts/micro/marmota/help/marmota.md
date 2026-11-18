# Marmota's keys in micro

cmd+/ opens this page, ctrl+q closes it (and any file or tab).

## Find and jump

| Key | Does |
|-----|------|
| cmd+F | Find in this file (a regular expression). enter, then ctrl+N for the next match, ctrl+P the one before; esc ends it |
| cmd+option+F | Fuzzy search the lines of this file, with a preview |
| cmd+shift+F | Search every file under the folder (asks where, then what) |
| cmd+P | Open a file by name |
| cmd+shift+O | Jump to a heading or a function of this file |
| cmd+G | Go to a line: type its number |
| ctrl+- | Back to where the cursor was before a jump; ctrl+shift+- forward again |
| cmd+↑ / cmd+↓ | Start / end of the file |
| cmd+← / cmd+→ | Start / end of the line |
| option+← / option+→ | A word left / right |

## Select and copy

| Key | Does |
|-----|------|
| shift + any key that moves | Select as it moves: shift+option+→ a word, shift+cmd+→ to the line's end |
| shift+option+↓ / ↑ | Select the paragraph; again, the next one too |
| cmd+A | Select all |
| cmd+C | Copy what is selected; with nothing selected, the line |
| cmd+X | Cut what is selected; with nothing selected, the line |
| cmd+V | Paste |
| the mouse | Click puts the cursor, drag selects, double click a word, triple click a line |

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
