VERSION = "0.1.0"

-- Marmota's part of micro, the editor Marmota opens files in: the keys a Mac
-- text field has that micro lacks, and asking one of Marmota's sessions about
-- what you write. build.sh installs it with bindings.json, which names these
-- functions.

local micro = import("micro")
local buffer = import("micro/buffer")
local config = import("micro/config")
local util = import("micro/util")

function init()
    -- "help marmota" (cmd+H): the keys worth knowing, Marmota's and micro's
    config.AddRuntimeFile("marmota", config.RTHelp, "help/marmota.md")
    -- $(marmota.status) in the status line (statusformatr in settings.json)
    micro.SetStatusInfoFn("marmota.status")
end

-- deleteToLineStart deletes from the cursor back to the start of the line,
-- as cmd+backspace does in a Mac text field; at the start of a line it joins
-- the line to the one above. What is selected is deleted instead.
function deleteToLineStart(bp)
    local c = bp.Cursor
    if c:HasSelection() then
        c:DeleteSelection()
        c:ResetSelection()
        return true
    end
    local to = buffer.Loc(c.X, c.Y)
    local from = buffer.Loc(0, c.Y)
    if c.X == 0 then
        if c.Y == 0 then
            return true
        end
        from = buffer.Loc(util.CharacterCountInString(bp.Buf:Line(c.Y - 1)), c.Y - 1)
    end
    bp.Buf:Remove(from, to)
    bp:Relocate()
    return true
end

local function blank(buf, y)
    return buf:Line(y):match("^%s*$") ~= nil
end

local function lineEnd(buf, y)
    return buffer.Loc(util.CharacterCountInString(buf:Line(y)), y)
end

-- selection is what is selected, its ends in reading order, or nil.
local function selection(c)
    if not c:HasSelection() then
        return nil
    end
    local a, b = c.CurSelection[1], c.CurSelection[2]
    if b.Y < a.Y or (b.Y == a.Y and b.X < a.X) then
        a, b = b, a
    end
    return buffer.Loc(a.X, a.Y), buffer.Loc(b.X, b.Y)
end

local function select(bp, from, to, cursorAtStart)
    local c = bp.Cursor
    c:SetSelectionStart(from)
    c:SetSelectionEnd(to)
    c.OrigSelection[1] = from
    c.OrigSelection[2] = to
    if cursorAtStart then
        c:GotoLoc(from)
    else
        c:GotoLoc(to)
    end
    bp:Relocate()
end

-- paragraph is the lines of the paragraph at line y: the lines around it
-- with text. On an empty line it is the next paragraph down (step 1) or up
-- (step -1); nil when there is none.
local function paragraph(buf, y, step)
    local last = buf:LinesNum() - 1
    while y >= 0 and y <= last and blank(buf, y) do
        y = y + step
    end
    if y < 0 or y > last then
        return nil
    end
    local top, bottom = y, y
    while top > 0 and not blank(buf, top - 1) do
        top = top - 1
    end
    while bottom < last and not blank(buf, bottom + 1) do
        bottom = bottom + 1
    end
    return top, bottom
end

-- selectParagraphDown selects the paragraph the cursor is in, as shift+option+down
-- does in a Mac text field; again, the next one as well.
function selectParagraphDown(bp)
    local buf = bp.Buf
    local from, to = selection(bp.Cursor)
    if from == nil then
        local top, bottom = paragraph(buf, bp.Cursor.Y, 1)
        if top ~= nil then
            select(bp, buffer.Loc(0, top), lineEnd(buf, bottom), false)
        end
        return true
    end
    local _, bottom = paragraph(buf, to.Y + 1, 1)
    if bottom ~= nil then
        select(bp, from, lineEnd(buf, bottom), false)
    end
    return true
end

-- selectParagraphUp is selectParagraphDown, upward.
function selectParagraphUp(bp)
    local buf = bp.Buf
    local from, to = selection(bp.Cursor)
    if from == nil then
        local top, bottom = paragraph(buf, bp.Cursor.Y, -1)
        if top ~= nil then
            select(bp, buffer.Loc(0, top), lineEnd(buf, bottom), true)
        end
        return true
    end
    local top = paragraph(buf, from.Y - 1, -1)
    if top ~= nil then
        select(bp, buffer.Loc(0, top), to, true)
    end
    return true
end

-- Going to a place, and selecting up to one. cmd+G asks where, in the line
-- at the foot: a line's number and enter, or one key, which goes there at
-- once: t the file's top, b its bottom, p the next paragraph's start, P the
-- one before, m the bracket that matches the one at the cursor (else the one
-- that opens around it), n back to where the last of these left from.
-- cmd+shift+G takes the same keys and selects instead: from the cursor, or
-- the end of what is selected, to that place; m selects what the brackets
-- hold. g keeps the line open for more of them, until esc.

local function before(a, b)
    return a.Y < b.Y or (a.Y == b.Y and a.X < b.X)
end

local function same(a, b)
    return a.X == b.X and a.Y == b.Y
end

local function cursorLoc(bp)
    return buffer.Loc(bp.Cursor.X, bp.Cursor.Y)
end

-- characters is a line as its characters: the one at X is characters[X + 1].
local function characters(line)
    local out = {}
    for ch in line:gmatch("[%z\1-\127\194-\244][\128-\191]*") do
        out[#out + 1] = ch
    end
    return out
end

-- leftFrom is where the jumps of each file left from, the last one last.
local leftFrom = {}
local leftFromMost = 50

local function leftFromOf(bp)
    local key = bp.Buf.AbsPath
    if key == "" then
        key = tostring(bp.Buf)
    end
    if leftFrom[key] == nil then
        leftFrom[key] = {}
    end
    return leftFrom[key]
end

-- show brings the cursor into view: a place off the screen to its middle.
local function show(bp)
    local view = bp:GetView()
    local y = bp.Cursor.Y
    if y < view.StartLine.Line or y >= view.StartLine.Line + bp:BufView().Height then
        bp:Center()
    end
    bp:Relocate()
end

-- go puts the cursor at loc, and remembers where it left from.
local function go(bp, loc)
    local from = cursorLoc(bp)
    if same(from, loc) and not bp.Cursor:HasSelection() then
        return
    end
    local stack = leftFromOf(bp)
    if not same(from, loc) then
        stack[#stack + 1] = from
        if #stack > leftFromMost then
            table.remove(stack, 1)
        end
    end
    bp.Cursor:ResetSelection()
    bp.Cursor:GotoLoc(loc)
    show(bp)
end

-- selectTo selects from what is selected, or the cursor, to loc.
local function selectTo(bp, loc)
    local from, to = selection(bp.Cursor)
    if from == nil then
        from, to = cursorLoc(bp), cursorLoc(bp)
    end
    if before(loc, from) then
        select(bp, loc, to, true)
    elseif not same(loc, from) then
        select(bp, from, loc, false)
    end
    show(bp)
end

local function fileEnd(buf)
    return lineEnd(buf, buf:LinesNum() - 1)
end

-- nextParagraph is the start of the first paragraph after line y, else the
-- file's end.
local function nextParagraph(buf, y)
    local last = buf:LinesNum() - 1
    while y <= last and not blank(buf, y) do
        y = y + 1
    end
    local top = paragraph(buf, y, 1)
    if y > last or top == nil then
        return fileEnd(buf)
    end
    return buffer.Loc(0, top)
end

-- paragraphStart is the start of the paragraph loc is in, and from there,
-- or between two, of the one before; the file's start when there is none.
local function paragraphStart(buf, loc)
    local y = loc.Y
    if not blank(buf, y) then
        local top = paragraph(buf, y, -1)
        if before(buffer.Loc(0, top), loc) then
            return buffer.Loc(0, top)
        end
        y = top - 1
    end
    if y < 0 then
        return buffer.Loc(0, 0)
    end
    local top = paragraph(buf, y, -1)
    return buffer.Loc(0, top or 0)
end

-- paragraphEnd is the end of the paragraph loc is in, and from there, or
-- between two, of the next one; the file's end when there is none.
local function paragraphEnd(buf, loc)
    local y = loc.Y
    if not blank(buf, y) then
        local _, bottom = paragraph(buf, y, 1)
        if before(loc, lineEnd(buf, bottom)) then
            return lineEnd(buf, bottom)
        end
        y = bottom + 1
    end
    if y > buf:LinesNum() - 1 then
        return fileEnd(buf)
    end
    local _, bottom = paragraph(buf, y, 1)
    if bottom == nil then
        return fileEnd(buf)
    end
    return lineEnd(buf, bottom)
end

local opens = { ["("] = true, ["["] = true, ["{"] = true }
local closes = { [")"] = true, ["]"] = true, ["}"] = true }

-- bracketReach is how many lines back an opening bracket is looked for.
local bracketReach = 5000

-- opener is the bracket that opens around loc: the nearest one before it
-- that nothing closes before loc. Brackets are counted as written: one in a
-- string or a comment counts too.
local function opener(buf, loc)
    local depth = 0
    for y = loc.Y, math.max(0, loc.Y - bracketReach), -1 do
        local chars = characters(buf:Line(y))
        local x = #chars
        if y == loc.Y then
            x = math.min(x, loc.X)
        end
        for i = x, 1, -1 do
            if closes[chars[i]] then
                depth = depth + 1
            elseif opens[chars[i]] then
                if depth == 0 then
                    return buffer.Loc(i - 1, y)
                end
                depth = depth - 1
            end
        end
    end
    return nil
end

-- brackets is the pair around from..to: where it opens and where it closes.
local function brackets(buf, from, to)
    local at = from
    while true do
        local open = opener(buf, at)
        if open == nil then
            return nil
        end
        local close, _, found = buf:FindMatchingBrace(open)
        if found and not before(close, to) then
            return open, buffer.Loc(close.X, close.Y)
        end
        at = open
    end
end

-- toBracket goes to the bracket that matches the one at the cursor, else to
-- the one that opens around the cursor.
local function toBracket(bp)
    local buf = bp.Buf
    local at = cursorLoc(bp)
    local match, left, found = buf:FindMatchingBrace(at)
    if found and not left then
        go(bp, buffer.Loc(match.X, match.Y))
        return true
    end
    local open = opener(buf, at)
    if open == nil then
        return false
    end
    go(bp, open)
    return true
end

-- selectBrackets selects what the brackets around the cursor hold; with
-- that selected, the brackets too; then what the pair around those holds.
local function selectBrackets(bp)
    local buf = bp.Buf
    local from, to = selection(bp.Cursor)
    if from == nil then
        from, to = cursorLoc(bp), cursorLoc(bp)
        -- on an opening bracket, it is that one's
        if opens[characters(buf:Line(from.Y))[from.X + 1]] then
            from = buffer.Loc(from.X + 1, from.Y)
            to = from
        end
    end
    local open, close = brackets(buf, from, to)
    if open == nil then
        return false
    end
    local inside = buffer.Loc(open.X + 1, open.Y)
    if same(from, inside) and same(to, close) then
        select(bp, open, buffer.Loc(close.X + 1, close.Y), false)
    else
        select(bp, inside, close, false)
    end
    show(bp)
    return true
end

-- toLine goes to line number, or selects up to it: through its end when
-- it is below, from its start when above.
local function toLine(bp, number, selecting)
    local buf = bp.Buf
    local y = math.max(0, math.min(number, buf:LinesNum()) - 1)
    if not selecting then
        go(bp, buffer.Loc(0, y))
        return
    end
    local from = selection(bp.Cursor) or cursorLoc(bp)
    if y < from.Y then
        selectTo(bp, buffer.Loc(0, y))
    else
        selectTo(bp, lineEnd(buf, y))
    end
end

-- jumpKey does what key asks. It reports whether there is such a key.
local function jumpKey(bp, key, selecting)
    local buf = bp.Buf
    local move = selecting and selectTo or go
    local from, to = selection(bp.Cursor)
    if key == "t" then
        move(bp, buffer.Loc(0, 0))
    elseif key == "b" then
        move(bp, fileEnd(buf))
    elseif key == "p" and selecting then
        selectTo(bp, paragraphEnd(buf, to or cursorLoc(bp)))
    elseif key == "P" and selecting then
        selectTo(bp, paragraphStart(buf, from or cursorLoc(bp)))
    elseif key == "p" then
        go(bp, nextParagraph(buf, bp.Cursor.Y))
    elseif key == "P" then
        go(bp, paragraphStart(buf, cursorLoc(bp)))
    elseif key == "m" then
        local found
        if selecting then
            found = selectBrackets(bp)
        else
            found = toBracket(bp)
        end
        if not found then
            micro.InfoBar():Message("No brackets around the cursor")
        end
    elseif key == "n" then
        local stack = leftFromOf(bp)
        local back = stack[#stack]
        if back == nil then
            micro.InfoBar():Message("No jump to go back from")
        elseif selecting then
            selectTo(bp, back)
        else
            table.remove(stack)
            bp.Cursor:ResetSelection()
            bp.Cursor:GotoLoc(back)
            show(bp)
        end
    else
        return false
    end
    return true
end

local jumpKeys = "line⏎  t top  b bottom  p/P paragraph  m bracket  n back"

-- askWhere opens the line that takes a jump's key. staying keeps it open
-- after each, until esc.
local function askWhere(bp, selecting, staying)
    local label = selecting and "Select to" or "Go to"
    if staying then
        label = label .. " (esc ends)"
    end
    local keys = jumpKeys
    if not staying then
        keys = keys .. "  g stay"
    end
    local taken = false -- a key closed the line, not esc
    micro.InfoBar():Prompt(label .. ": " .. keys .. " › ", "", "MarmotaJump", function(typed)
        if taken or typed == "" or typed:match("^%d+$") then
            return
        end
        taken = true
        micro.InfoBar():DonePrompt(true)
        local key = typed:sub(-1)
        if key == "g" then
            staying = true
        elseif not jumpKey(bp, key, selecting) then
            micro.InfoBar():Message("No such place: " .. jumpKeys)
        end
        if staying then
            askWhere(bp, selecting, true)
        end
    end, function(typed, canceled)
        if taken or canceled then
            return
        end
        if typed:match("^%d+$") then
            toLine(bp, tonumber(typed), selecting)
            if staying then
                askWhere(bp, selecting, true)
            end
        end
    end)
end

function jump(bp)
    askWhere(bp, false, false)
    return true
end

function jumpSelect(bp)
    askWhere(bp, true, false)
    return true
end

-- drag is the left button held and moved: it selects up to the pointer.
-- The wheel turned meanwhile comes here too, as a move that goes nowhere
-- (micro's terminal library takes it for one): what the terminal sent says
-- which it was, and the wheel scrolls first, so a selection goes on past
-- what the screen showed.
function drag(bp, event)
    local button = tonumber(event:EscSeq():match("^\27%[<(%d+);"))
    if button ~= nil and button >= 64 and button < 128 then
        if button % 4 == 0 then
            bp:ScrollUpAction()
        elseif button % 4 == 1 then
            bp:ScrollDownAction()
        end
    end
    return bp:MouseDrag(event)
end

-- Asking a session while you write. cmd+L asks the session this editor
-- writes to: your question goes to it with the file, and the lines you
-- selected, named after it, so it reads them itself. You go on writing; its
-- reply shows in a pane on the right when its turn has ended. cmd+enter puts
-- the last reply at the cursor, or in place of what is selected. The session
-- is one of Marmota's: what you ask here shows in its Sessions box too.
--
-- All of it goes through `y session`: list for the sessions to pick from,
-- ask to send a question and wait for its reply.

local shell = import("micro/shell")
local os = import("os")
local strings = import("strings")
local filepath = import("path/filepath")

-- marmota is the y that opened this editor, else the one on PATH.
local marmota = os.Getenv("MARMOTA_BIN")
if marmota == "" then
    marmota = "y"
end

-- session is the one questions go to: the one Marmota's Ask box wrote to
-- when it opened this editor, until you pick another (cmd+shift+L). agent is
-- who runs it, as `y session list` says: "cc" Claude Code, "co" Codex.
local session = {
    id = os.Getenv("MARMOTA_SESSION"),
    title = os.Getenv("MARMOTA_SESSION_TITLE"),
    host = os.Getenv("MARMOTA_SESSION_HOST"),
    agent = os.Getenv("MARMOTA_SESSION_AGENT"),
}

-- newSession stands for a session not started yet: the next question
-- starts one in this editor's folder, with the question as its first
-- message. agents are who can run it, and what each is called.
local newSession = "new"
local codex = "co"
local agents = { { "cc", "Claude Code" }, { codex, "Codex" } }

-- asked holds each question of this editor, oldest first: who was asked,
-- the question, and the reply once there is one. Several can wait for their
-- replies at once, each from its own session. arrived counts the replies, so
-- the last one to come is known.
local asked = {}
local arrived = 0
local repliesName = "Marmota replies"

-- status is $(marmota.status) in the status line: whom questions go to, and
-- how many replies are still awaited.
function status(b)
    local waiting = 0
    for _, a in ipairs(asked) do
        if a.reply == nil then
            waiting = waiting + 1
        end
    end
    local out = ""
    if session.id ~= "" then
        out = "→ " .. session.title .. "  "
    end
    if waiting > 0 then
        out = out .. "✳ " .. waiting .. " asking  "
    end
    return out
end

-- folder is where this editor works: the folder Marmota opened it in.
local function folder()
    local dir, err = os.Getwd()
    if err ~= nil then
        return ""
    end
    return dir
end

local function shellQuote(s)
    return "'" .. s:gsub("'", "'\\''") .. "'"
end

-- paneNamed is the pane that shows the buffer of that name, when one does.
local function paneNamed(name)
    local tabs = micro.Tabs().List
    for i = 1, #tabs do
        local panes = tabs[i].Panes
        for j = 1, #panes do
            if panes[j].Buf:GetName() == name then
                return panes[j], tabs[i], j - 1
            end
        end
    end
    return nil
end

-- repliesPane is the pane that shows the replies, when one does.
local function repliesPane()
    return paneNamed(repliesName)
end

-- keys is cmd+H: the page of keys ("help marmota"), shown below the file or
-- put away, wherever the cursor is.
function keys(bp)
    local pane = paneNamed("Help marmota") -- the name micro gives a help page's buffer
    if pane ~= nil then
        pane:Quit()
    else
        bp:HandleCommand("help marmota")
    end
    return true
end

local function repliesText()
    local out = {}
    for _, a in ipairs(asked) do
        table.insert(out, "› " .. a.question .. "   — " .. a.title)
        if a.reply == nil then
            table.insert(out, "")
            if a.needsYou ~= nil then
                table.insert(out, "⚠ " .. a.needsYou)
                table.insert(out, "✳ waiting for your answer there…")
            else
                table.insert(out, a.starting and "✳ starting a session…" or "✳ asking…")
            end
        else
            table.insert(out, "")
            table.insert(out, a.reply)
        end
        table.insert(out, "")
    end
    return table.concat(out, "\n")
end

-- showReplies draws the questions and replies in their pane on the right,
-- opening it beside bp when it isn't there; the keyboard stays where it is.
local function showReplies(bp)
    local pane = repliesPane()
    if pane == nil then
        local tab = bp:Tab()
        local at = tab:GetPane(bp:ID())
        local buf = buffer.NewBuffer(repliesText(), repliesName)
        buf.Type.Scratch = true
        buf:SetOptionNative("softwrap", true)
        buf:SetOptionNative("wordwrap", true)
        buf:SetOptionNative("filetype", "markdown")
        buf:SetOptionNative("ruler", false)
        pane = bp:VSplitIndex(buf, true)
        tab:SetActive(at)
    else
        pane.Buf:Replace(pane.Buf:Start(), pane.Buf:End(), repliesText())
    end
    pane.Cursor:GotoLoc(pane.Buf:End())
    pane:Relocate()
end

-- remote reports a session on another computer, which can't read this
-- one's files.
local function remote()
    return session.host ~= "" and session.host ~= "here"
end

-- mention names the file to a session, with the selected lines, as Claude
-- Code writes it: "@notes/plan.md#L12-30". Codex has no such form: it is
-- told in words, "the file /notes/plan.md, lines 12-30". It says whether
-- lines are named.
local function mention(bp)
    local path = bp.Buf.AbsPath
    local from, to = selection(bp.Cursor)
    local out = "@" .. path
    if session.agent == codex then
        out = "the file " .. path
    elseif path:find(" ") then
        out = '@"' .. path .. '"'
    end
    if from == nil then
        return out, false
    end
    local last = to.Y
    if to.X == 0 and to.Y > from.Y then
        last = to.Y - 1 -- the selection ends before this line's first character
    end
    if session.agent == codex then
        if last > from.Y then
            return out .. ", lines " .. (from.Y + 1) .. "-" .. (last + 1), true
        end
        return out .. ", line " .. (from.Y + 1), true
    end
    if last > from.Y then
        return out .. "#L" .. (from.Y + 1) .. "-" .. (last + 1), true
    end
    return out .. "#L" .. (from.Y + 1), true
end

-- excerpt is the selected text, or the file's, for a session that can't
-- read the file itself.
local function excerpt(bp)
    local text
    if bp.Cursor:HasSelection() then
        text = util.String(bp.Cursor:GetSelection())
    else
        text = util.String(bp.Buf:Bytes())
    end
    return "From " .. filepath.Base(bp.Buf.AbsPath) .. ":\n\n" .. text
end

-- reference is what a question says of what it is about: the file and the
-- selected lines by name, for the session to read; their text itself for a
-- session on another computer.
local function reference(bp)
    if remote() then
        return excerpt(bp)
    end
    local name = mention(bp)
    if session.agent == codex then
        return "This is about " .. name .. "."
    end
    return name
end

-- editRequest is what cmd+K adds to what you ask for: that the change is
-- made in the file itself, which this editor shows as soon as it is. A
-- session on another computer can't reach the file: it replies with the new
-- text, for cmd+enter to put in.
local function editRequest(bp)
    if remote() then
        return "Reply with only the new text, nothing before or after it, for this:\n\n" .. excerpt(bp)
    end
    local name, lines = mention(bp)
    if lines then
        return "Do this by editing the file in place: " .. name .. " (those lines only, unless it needs more). "
            .. "It is open in an editor that shows your change. Reply with one line on what you changed."
    end
    return "Do this by editing the file in place: " .. name .. ". "
        .. "It is open in an editor that shows your change. Reply with one line on what you changed."
end

-- reload shows a file as it is on disk now in every pane that has it open:
-- a session changed it. The change is one step to undo.
local function reload(path)
    local tabs = micro.Tabs().List
    for i = 1, #tabs do
        local panes = tabs[i].Panes
        for j = 1, #panes do
            if panes[j].Buf.AbsPath == path then
                panes[j].Buf:ReOpen()
                panes[j]:Relocate()
            end
        end
    end
end

local onReply

-- wait has entry's reply waited for: its message is the session's already.
local function wait(index, bp)
    local entry = asked[index]
    shell.JobSpawn(marmota, { "session", "ask", "--sent", "--keep-waiting", entry.session, entry.message },
        nil, nil, onReply, index, bp)
end

onReply = function(output, args)
    -- its place in asked, not the entry: a table handed to a job comes
    -- back a copy
    local entry, bp = asked[args[1]], args[2]
    output = strings.TrimSpace(output)
    local failed = output:match("y session ask: (.*)$")
    if failed ~= nil and failed:find("it stopped to ask you", 1, true) and entry.needsYou == nil then
        -- it needs your answer, in Marmota: said once, and waited for
        entry.needsYou = failed
        micro.InfoBar():Error(entry.title .. " " .. failed)
        wait(args[1], bp)
        showReplies(bp)
        return
    end
    if failed ~= nil then
        local before = strings.TrimSpace(output:sub(1, #output - #failed - #"y session ask: "))
        entry.reply = "⚠ " .. failed
        if before ~= "" then
            entry.reply = before .. "\n\n" .. entry.reply
        end
        micro.InfoBar():Error(entry.title .. ": " .. failed)
    else
        entry.reply = output
        entry.done = true
        if entry.edits ~= nil then
            reload(entry.edits)
            micro.InfoBar():Message(entry.title .. " changed " .. filepath.Base(entry.edits) .. ": cmd+Z takes it back")
        else
            micro.InfoBar():Message(entry.title .. " replied: cmd+enter puts it at the cursor")
        end
    end
    arrived = arrived + 1
    entry.arrived = arrived
    showReplies(bp)
end

-- onStarted has the session a question started: the question is its first
-- message, and its reply is waited for as any other.
local function onStarted(output, args)
    local entry, bp = asked[args[1]], args[2]
    local id = output:match("started (%S+)")
    if id == nil then
        entry.reply = "⚠ " .. strings.TrimSpace(output)
        arrived = arrived + 1
        entry.arrived = arrived
        micro.InfoBar():Error("Couldn't start a session: " .. strings.TrimSpace(output))
        showReplies(bp)
        return
    end
    entry.starting = false
    entry.session = id
    if session.id == newSession and session.agent == entry.agent then
        -- the next questions go on with it
        session.id, session.title, session.host = id, entry.title, "here"
    end
    shell.JobSpawn(marmota, { "session", "ask", "--sent", id, entry.message }, nil, nil, onReply, args[1], bp)
    showReplies(bp)
end

-- send asks the session: question, and about it either what it is about
-- (cmd+L) or, with edit, that it changes the file itself (cmd+K).
local function send(bp, question, edit)
    if bp.Buf.Path ~= "" and bp.Buf:Modified() then
        bp:Save()
    end
    local message = question
    if bp.Buf.Path ~= "" then
        message = question .. "\n\n" .. (edit and editRequest(bp) or reference(bp))
    end
    local entry = { title = session.title, question = question, message = message, session = session.id,
        agent = session.agent }
    if edit and bp.Buf.Path ~= "" and not remote() then
        entry.edits = bp.Buf.AbsPath
    end
    table.insert(asked, entry)
    if session.id == newSession then
        entry.starting = true
        shell.JobSpawn(marmota, { "session", "start", "--agent", session.agent, "--cwd", folder(), "--", message },
            nil, nil, onStarted, #asked, bp)
    else
        shell.JobSpawn(marmota, { "session", "ask", session.id, message }, nil, nil, onReply, #asked, bp)
    end
    showReplies(bp)
    micro.InfoBar():Message("Asked " .. session.title .. "; go on writing")
end

local function promptQuestion(bp)
    micro.InfoBar():Prompt("Ask " .. session.title .. " › ", "", "MarmotaAsk", nil, function(resp, canceled)
        if not canceled and strings.TrimSpace(resp) ~= "" then
            send(bp, strings.TrimSpace(resp), false)
        end
    end)
end

local function promptEdit(bp)
    local what = bp.Cursor:HasSelection() and "these lines" or "this file"
    micro.InfoBar():Prompt(session.title .. ", change " .. what .. " › ", "", "MarmotaEdit", nil,
        function(resp, canceled)
            if not canceled and strings.TrimSpace(resp) ~= "" then
                send(bp, strings.TrimSpace(resp), true)
            end
        end)
end

-- pickSession lists Marmota's sessions to pick the one questions go to,
-- then goes on with after.
local function pickSession(bp, after)
    -- the sessions, and first a row for one not started yet, by each agent
    local dir = folder()
    local rows = {}
    for _, agent in ipairs(agents) do
        table.insert(rows, newSession .. "  " .. agent[1] .. "  here  -  + New " .. agent[2] .. " session in "
            .. filepath.Base(dir) .. "  " .. dir)
    end
    local list = "{ " .. shellQuote(marmota)
        .. " session list 2>/dev/null || echo 'ID  AGENT  HOST  STATUS  TITLE  FOLDER'; }"
        -- "\n" as two characters: awk makes the line break of it
        .. " | awk -v rows=" .. shellQuote(table.concat(rows, "\\n")) .. " 'NR==1{print; print rows; next} {print}'"
    -- the id stays out of sight: the title and the folder say which it is
    local fzf = "fzf --header-lines=1 --with-nth=2.. --layout=reverse --prompt='Ask which session? ' --no-multi"
    local cmd = "sh -c " .. shellQuote(list .. " | " .. fzf)
    local err = shell.RunTermEmulator(bp, cmd, false, true, function(output, args)
        local line = strings.TrimSpace(output)
        if line == "" then
            return
        end
        -- ID  AGENT  HOST  STATUS  TITLE  FOLDER, two spaces or more apart
        local fields = {}
        for field in (line .. "  "):gmatch("(.-)%s%s+") do
            table.insert(fields, field)
        end
        if #fields < 5 then
            micro.InfoBar():Error("Couldn't read the session picked: " .. line)
            return
        end
        session.id, session.agent, session.host, session.title = fields[1], fields[2], fields[3], fields[5]
        if session.id == newSession then
            session.title = "new · " .. filepath.Base(folder())
            if session.agent == codex then
                session.title = "new Codex · " .. filepath.Base(folder())
            end
        end
        micro.InfoBar():Message("Questions go to " .. session.title)
        if after ~= nil then
            after(args[1])
        end
    end, { bp })
    if err ~= nil then
        micro.InfoBar():Error(err)
    end
end

-- ask is cmd+L: a question to the session, about this file.
function ask(bp)
    if session.id == "" then
        pickSession(bp, promptQuestion)
    else
        promptQuestion(bp)
    end
    return true
end

-- askAnother is cmd+shift+L: pick the session, then ask it.
function askAnother(bp)
    pickSession(bp, promptQuestion)
    return true
end

-- edit is cmd+K: the session changes the selected lines, or the file, in
-- the file itself, as you ask; this editor shows the change when it is
-- made, and cmd+Z takes it back.
function edit(bp)
    if bp.Buf.Path == "" then
        micro.InfoBar():Message("Save the file first: the session changes it on disk")
        return true
    end
    if session.id == "" then
        pickSession(bp, promptEdit)
    else
        promptEdit(bp)
    end
    return true
end

-- insertReply is cmd+enter: the last reply at the cursor, or in place of
-- what is selected. What a session said of a change it made isn't one.
function insertReply(bp)
    -- the reply that came last, whichever question it answers
    local reply, last = nil, 0
    for _, a in ipairs(asked) do
        if a.done and a.edits == nil and a.arrived > last then
            reply, last = a.reply, a.arrived
        end
    end
    if reply == nil then
        micro.InfoBar():Message("No reply yet")
        return true
    end
    if bp.Buf:GetName() == repliesName then
        micro.InfoBar():Message("Put the cursor where the reply goes first")
        return true
    end
    local c = bp.Cursor
    if c:HasSelection() then
        c:DeleteSelection()
        c:ResetSelection()
    end
    bp.Buf:Insert(buffer.Loc(c.X, c.Y), reply)
    bp:Relocate()
    return true
end

-- toggleReplies is cmd+J: the replies' pane, shown or put away.
function toggleReplies(bp)
    local pane = repliesPane()
    if pane ~= nil then
        pane:Quit()
    elseif #asked > 0 then
        showReplies(bp)
    else
        micro.InfoBar():Message("Nothing asked yet: cmd+L asks a session")
    end
    return true
end
