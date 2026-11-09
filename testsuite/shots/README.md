# Tutorial screenshots

`docs/shots/*.webp` are real Marmota with real Claude Code and Codex sessions, on two demo projects. Retake them when the screen changes.

1. `./demo.sh` makes the demo projects in `~/code` (set `DEMO` for another folder). `tidepool` is a small Go tool for tide times; `field-notes` holds a few Markdown notes. It also makes `~/code/.shots`, which keeps the `y` you start apart from yours: its settings and state, and its own Codex home, signed in through your `~/.codex/auth.json`. It sees only the demo's Claude Code transcripts.
2. `P=$(./start.sh)` starts `y` in a WezTerm window in the workspace `marmota-shots`, which your window doesn't show. Its sessions are real: `>claude`, `!` `n` and the rest start them in the demo projects, with your own Claude Code settings.
3. `./keys.sh $P z t i d e '\r'` types into it and prints its screen. To type into a session's pane, use `wezterm cli send-text --pane-id N --no-paste`.
4. `./capture.py NAME $P` saves every pane of that window to `out/NAME.json`, colors included. `./render.py NAME` draws it as WezTerm does into `out/NAME.png`, using headless Chrome and the JetBrains Mono Nerd Font.
5. `cwebp -lossless -z 9 out/NAME.png -o ../../docs/shots/NAME.webp`.
6. Stop the sessions, or your own Marmota lists them as well: it lists every live session on the computer. `./demo.sh --clean` removes the projects, `~/code/.shots`, and the demo sessions' transcripts.

Nothing about your computer reaches a picture:
- your user name becomes `alex.morgan`, cut or repeated to the same length;
- memory and disk in Details are scaled to a 16 GB / 512 GB Mac, so the bars still match;
- `subs.json` replaces e-mail addresses and Claude Code's weekly usage line; add pairs there for anything else.

Read each picture before you commit it all the same: `ls -l` in a session's output, for one, prints your user name.

Things that cost time before:
- Started from inside Claude Code, the `CLAUDE*` variables reach the sessions, and Claude Code turns transcripts off for them, so Marmota can't follow them. `start.sh` clears them.
- Codex's socket lives in its home, and macOS caps socket paths at 104 bytes. In a deep folder Codex sessions never get listed (`connect: invalid argument`).
- A first session in a folder asks whether you trust it; answer in its pane.
