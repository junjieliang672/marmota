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
    -- how the column of questions, replies and sessions is drawn (render)
    config.AddRuntimeFile("marmota", config.RTSyntax, "syntax/marmota-replies.yaml")
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

-- Asking a session while you write, in a column on the right: above, what
-- was asked and replied, or the sessions to pick from; under it the box
-- everything is written in. The cursor is in the file or in that box. cmd+L
-- puts it in the box for a question to the session this editor writes to:
-- enter sends it with the file, and the lines you selected, named after it,
-- so the session reads them itself, and the cursor goes back to the file.
-- You go on writing; the reply shows above the box when the session's turn
-- has ended. cmd+enter puts the last reply at the cursor, or in place of
-- what is selected. cmd+shift+L lists the sessions above the box, which then
-- filters them, and cmd+K has the box take what the session is to change in
-- the file. The session is one of Marmota's: what you ask here shows in its
-- Sessions box too.
--
-- All of it goes through `y session`: list for the sessions to pick from,
-- this computer's, ask to send a question and wait for its reply.

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
-- message, run by the agent of the row picked: "cc" or "co".
local newSession = "new"
local codex = "co"

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

-- Moving and deleting by word in a line of Chinese, Japanese or Korean.
-- micro takes a run of letters for a word, and these characters are
-- letters with no spaces between them: option+← went over half a sentence,
-- and option+backspace deleted it. The system knows their words (我认为应该这样
-- is 我, 认为, 应该, 这样), and y asks it: `y words <line>` prints where each
-- word starts and ends. A line is asked about once, and remembered. Deleting
-- takes what moving goes over.
--
-- A line without such characters keeps micro's own rule, with nothing
-- asked: code is moved through as it was.
local lineWords = {}
local linesKept = 0
local linesMost = 200

-- wide reports a line with such characters: in UTF-8 they start with a byte
-- from 227 to 237 (U+3000 to U+DFFF: the punctuation, kana, Chinese
-- characters and Hangul).
local function wide(line)
    return line:find("[\227-\237]") ~= nil
end

-- wordsOf is the words of line, each where it starts and ends, in
-- characters; nil when y can't say.
local function wordsOf(line)
    local found = lineWords[line]
    if found ~= nil then
        return found
    end
    local out, err = shell.ExecCommand(marmota, "words", "--", line)
    if err ~= nil then
        return nil
    end
    found = {}
    for from, to in out:gmatch("(%d+),(%d+)") do
        table.insert(found, { tonumber(from), tonumber(to) })
    end
    if linesKept >= linesMost then
        lineWords, linesKept = {}, 0
    end
    lineWords[line] = found
    linesKept = linesKept + 1
    return found
end

-- wordStop is where a move by a word goes from the cursor, left with step
-- -1 and right with 1: the start of the nearest word before it, the end of
-- the nearest after it, what lies between moved over with the word. It is
-- nil where micro's own move serves: a line without such characters, and
-- from a line's edge, which goes on to the line before or after.
local function wordStop(bp, step)
    local c = bp.Cursor
    local line = bp.Buf:Line(c.Y)
    if not wide(line) then
        return nil
    end
    local length = util.CharacterCountInString(line)
    if (step < 0 and c.X == 0) or (step > 0 and c.X >= length) then
        return nil
    end
    local found = wordsOf(line)
    if found == nil then
        return nil
    end
    if step < 0 then
        local to = 0
        for _, w in ipairs(found) do
            if w[1] < c.X then
                to = w[1]
            end
        end
        return buffer.Loc(to, c.Y)
    end
    for _, w in ipairs(found) do
        if w[2] > c.X then
            return buffer.Loc(w[2], c.Y)
        end
    end
    return buffer.Loc(length, c.Y)
end

local function wordMove(bp, step, own)
    local to = wordStop(bp, step)
    if to == nil then
        return own(bp)
    end
    bp.Cursor:ResetSelection()
    bp.Cursor:GotoLoc(to)
    bp:Relocate()
    return true
end

-- wordLeft and wordRight are option+← and option+→.
function wordLeft(bp)
    return wordMove(bp, -1, function(b) return b:WordLeft() end)
end

function wordRight(bp)
    return wordMove(bp, 1, function(b) return b:WordRight() end)
end

local function wordSelect(bp, step, own)
    local to = wordStop(bp, step)
    if to == nil then
        return own(bp)
    end
    selectTo(bp, to)
    return true
end

-- selectWordLeft and selectWordRight are shift+option+← and →.
function selectWordLeft(bp)
    return wordSelect(bp, -1, function(b) return b:SelectWordLeft() end)
end

function selectWordRight(bp)
    return wordSelect(bp, 1, function(b) return b:SelectWordRight() end)
end

local function wordDelete(bp, step, own)
    if bp.Cursor:HasSelection() or bp.Buf.Type.Readonly then
        return own(bp)
    end
    local to = wordStop(bp, step)
    if to == nil then
        return own(bp)
    end
    local at = buffer.Loc(bp.Cursor.X, bp.Cursor.Y)
    if step < 0 then
        bp.Buf:Remove(to, at)
    else
        bp.Buf:Remove(at, to)
    end
    bp:Relocate()
    return true
end

-- deleteWordLeft is option+backspace, deleteWordRight option+delete
-- (fn+option+backspace): what option+← and option+→ would go over.
function deleteWordLeft(bp)
    return wordDelete(bp, -1, function(b) return b:DeleteWordLeft() end)
end

function deleteWordRight(bp)
    return wordDelete(bp, 1, function(b) return b:DeleteWordRight() end)
end

-- paneNamed is the pane that shows the buffer of that name, when one does.
local function paneNamed(name)
    local tabs = micro.Tabs().List
    for i = 1, #tabs do
        local panes = tabs[i].Panes
        for j = 1, #panes do
            local buf = panes[j].Buf -- a terminal's pane has none
            if buf ~= nil and buf:GetName() == name then
                return panes[j], tabs[i], j - 1
            end
        end
    end
    return nil
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

-- The place a question was about. Each question keeps it: the file, the
-- first and last line asked about (counted from 1) and what those lines
-- said; with nothing selected, the cursor's line alone, whole set. cmd+K's
-- also keeps the whole file as it was, and once the session has changed it,
-- the lines its change spans now. The replies say where those lines are
-- now, and enter on a question there selects them in the file again.

-- linesOf is text as its lines, without the line ends.
local function linesOf(text)
    local out = {}
    for line in (text .. "\n"):gmatch("(.-)\r?\n") do
        table.insert(out, line)
    end
    return out
end

-- placeOf is what the pane's question is about, as a question keeps it:
-- the selected lines (a selection that ends at the start of a line ends on
-- the line before), else the cursor's line. nil for a buffer that is no
-- file.
local function placeOf(bp)
    if bp.Buf.Path == "" or bp.Buf.AbsPath == "" then
        return nil
    end
    local place = { path = bp.Buf.AbsPath }
    local from, to = selection(bp.Cursor)
    if from == nil then
        place.first, place.last, place.whole = bp.Cursor.Y + 1, bp.Cursor.Y + 1, true
        return place
    end
    local last = to.Y
    if to.X == 0 and to.Y > from.Y then
        last = to.Y - 1 -- the selection ends before this line's first character
    end
    place.first, place.last, place.lines = from.Y + 1, last + 1, {}
    for y = from.Y, last do
        table.insert(place.lines, bp.Buf:Line(y))
    end
    return place
end

-- bufferOf is the buffer of a pane that has the file open, when one does.
local function bufferOf(path)
    local tabs = micro.Tabs().List
    for i = 1, #tabs do
        local panes = tabs[i].Panes
        for j = 1, #panes do
            local buf = panes[j].Buf
            if buf ~= nil and buf.AbsPath == path then
                return buf
            end
        end
    end
    return nil
end

-- fileLines is the file as it is now, as lines: what a pane shows of it,
-- what isn't saved too, else what is on disk. nil when it can't be read.
local function fileLines(path, fromDisk)
    local buf = not fromDisk and bufferOf(path) or nil
    if buf ~= nil then
        return linesOf(util.String(buf:Bytes()))
    end
    local f = io.open(path, "r")
    if f == nil then
        return nil
    end
    local text = f:read("*a") or ""
    f:close()
    return linesOf(text)
end

-- whereNow finds the lines a question kept in the file now: "same" at the
-- line they were on; "moved" and the first line they are on now, the
-- nearest to where they were when they are there more than once; else
-- "changed".
local function whereNow(place, lines)
    local kept = place.lines
    local function at(first)
        for k = 1, #kept do
            if lines[first + k - 1] ~= kept[k] then
                return false
            end
        end
        return true
    end
    if at(place.first) then
        return "same", place.first
    end
    local best = nil
    for first = 1, #lines - #kept + 1 do
        if at(first) and (best == nil or math.abs(first - place.first) < math.abs(best - place.first)) then
            best = first
        end
    end
    if best ~= nil then
        return "moved", best
    end
    return "changed", place.first
end

-- changedSpan is the lines a change spans in after, by comparing before and
-- after from the start and from the end: from the first line that differs
-- to the last. last is before first when lines were only taken out; nil
-- when nothing changed. A change of yours elsewhere meanwhile widens it.
local function changedSpan(before, after)
    local first = 1
    while first <= #before and first <= #after and before[first] == after[first] do
        first = first + 1
    end
    if first > #before and first > #after then
        return nil
    end
    local same = 0
    while same < #before - first + 1 and same < #after - first + 1
        and before[#before - same] == after[#after - same] do
        same = same + 1
    end
    return first, #after - same
end

-- span writes lines first to last: "L3", or "L3-4".
local function span(first, last)
    if last > first then
        return "L" .. first .. "-" .. last
    end
    return "L" .. first
end

-- placeNow says which lines of its file a question is about now: the first
-- and last to select (nil when the whole file was asked about, or cmd+K
-- changed nothing: the cursor's line is gone back to), and whether those
-- lines changed since. For cmd+K, once the session is done, the lines it
-- changed; else where the lines asked about are, when lines went in above
-- them.
local function placeNow(a)
    local place = a.place
    if a.changed ~= nil then
        local first, last = a.changed[1], a.changed[2]
        return first, math.max(first, last), false
    end
    if place.whole or (a.done and a.edits ~= nil) then
        return nil, nil, false
    end
    local lines = fileLines(place.path)
    if not lines then
        return place.first, place.last, false
    end
    local state, first = whereNow(place, lines)
    return first, first + #place.lines - 1, state == "changed"
end

-- Each question is a line that says whom you asked, then the question,
-- highlighted; the reply starts under the session's name; a line parts one
-- question from the next. syntax/marmota-replies.yaml colors them by how
-- these lines start: a question's by a no-break space, which shows as a
-- space.
local highlighted = "\194\160"

-- shownFor is which question each line of the replies is part of, by the
-- line's number from 0, as repliesText last wrote them.
local shownFor = {}

local function repliesText()
    local out, count = {}, 0
    shownFor = {}
    local function add(text, index)
        table.insert(out, text)
        for _ in (text .. "\n"):gmatch("\n") do
            shownFor[count] = index
            count = count + 1
        end
    end
    for i, a in ipairs(asked) do
        if i > 1 then
            add(string.rep("─", 36), i)
            add("", i)
        end
        add("You → " .. a.title, i)
        for line in (a.question .. "\n"):gmatch("(.-)\n") do
            add(highlighted .. line .. " ", i)
        end
        add("", i)
        if a.reply ~= nil then
            add("⏺ " .. a.title, i)
            add(a.reply, i)
        elseif a.needsYou ~= nil then
            add("⚠ " .. a.needsYou, i)
            add("✳ cmd+O opens it under the editor: answer it there, then close that pane", i)
        else
            add(a.starting and "✳ starting a session…" or "✳ asking…", i)
        end
        add("", i)
    end
    return table.concat(out, "\n")
end

-- The column. Its upper pane, the replies' (repliesName), shows what was
-- asked and replied, or the sessions while one is picked; its lower one is
-- the box (boxName). Each has its status line say what it is for: the upper
-- one's, between the two, the keys; the box's, at the foot, whom the
-- question goes to and what it is about.
local boxName = "Marmota ask"
local boxRows = 4
local columnShare = 0.4 -- of the width of the pane it opens beside

-- side is the column's state. mode is what the box is for: "ask" a
-- question, "edit" what the session is to change in the file, "pick" the
-- session, after which it is for after again. While a session is picked the
-- box filters rows, the sessions listed, to shown, sel of them marked;
-- listing is set while they are listed, to the listing's number. held is
-- what the box held before it became the filter, draft what it held when
-- the column was put away. file is the pane a question is about, and about
-- says so: "plan.md L3-4".
local side = {
    mode = "ask",
    after = "ask",
    rows = {},
    shown = {},
    sel = 1,
    filter = "",
    listing = 0,
    listed = 0,
    held = "",
    draft = "",
    file = nil,
    about = "",
}

-- inColumn reports a pane of the column.
local function inColumn(bp)
    local name = bp.Buf:GetName()
    return name == repliesName or name == boxName
end

-- written is what a pane's buffer holds.
local function written(pane)
    return util.String(pane.Buf:Bytes())
end

-- write puts text in the box, the cursor after it.
local function write(box, text)
    box.Buf:Replace(box.Buf:Start(), box.Buf:End(), text)
    box.Cursor:GotoLoc(box.Buf:End())
    box:Relocate()
end

-- aboutOf says what a question from this pane is about: its file, and the
-- lines selected.
local function aboutOf(bp)
    if bp.Buf.Path == "" then
        return ""
    end
    local name = filepath.Base(bp.Buf.AbsPath)
    local from, to = selection(bp.Cursor)
    if from == nil then
        return name
    end
    local last = to.Y
    if to.X == 0 and to.Y > from.Y then
        last = to.Y - 1 -- the selection ends before this line's first character
    end
    if last > from.Y then
        return name .. " L" .. (from.Y + 1) .. "-" .. (last + 1)
    end
    return name .. " L" .. (from.Y + 1)
end

-- label is the box's status line.
local function label()
    if side.mode == "pick" then
        return "Pick a session · type to filter"
    end
    if side.mode == "edit" then
        return session.title .. " changes " .. (side.about ~= "" and side.about or "the file")
    end
    if side.about ~= "" then
        return "Ask " .. session.title .. " · " .. side.about
    end
    return "Ask " .. session.title
end

-- sessionsText is the list of sessions, those the box's words leave: a row
-- stays when it has each of them. The marked row starts with an arrow.
local function sessionsText()
    local words = {}
    for word in side.filter:lower():gmatch("%S+") do
        table.insert(words, word)
    end
    side.shown = {}
    for _, row in ipairs(side.rows) do
        local text, has = row.text:lower(), true
        for _, word in ipairs(words) do
            if not text:find(word, 1, true) then
                has = false
                break
            end
        end
        if has then
            table.insert(side.shown, row)
        end
    end
    side.sel = math.max(1, math.min(side.sel, #side.shown))
    local out = { "Ask which session?", "" }
    for i, row in ipairs(side.shown) do
        table.insert(out, (i == side.sel and "▶ " or "  ") .. row.text)
    end
    if side.listing > 0 then
        table.insert(out, "  listing sessions…")
    elseif #side.shown == 0 then
        table.insert(out, "  no session has that")
    end
    return table.concat(out, "\n")
end

-- render draws the column, when it is there: the questions and replies, the
-- last in view, or the sessions; and what each status line says.
local function render()
    local show = paneNamed(repliesName)
    if show == nil then
        return
    end
    local picking = side.mode == "pick"
    local text = picking and sessionsText() or repliesText()
    -- the keyboard in the replies: the cursor stays on the question it is on
    local cur = micro.CurPane()
    local reading = not picking and cur ~= nil and cur.Buf ~= nil and cur.Buf:GetName() == repliesName
    if text ~= written(show) then
        local y = show.Cursor.Y
        show.Buf.Type.Readonly = false
        show.Buf:Replace(show.Buf:Start(), show.Buf:End(), text)
        show.Buf.Type.Readonly = true
        show.Buf:SetOptionNative("softwrap", not picking) -- a session's row is one line, cut at the edge
        if picking then
            show.Cursor:GotoLoc(buffer.Loc(0, side.sel + 1)) -- the marked row, under the two lines above the rows
        elseif reading then
            show.Cursor:GotoLoc(buffer.Loc(0, math.min(y, show.Buf:LinesNum() - 1)))
        else
            show.Cursor:GotoLoc(show.Buf:End())
        end
        show:Relocate()
    end
    local keys = "enter sends · shift+enter a new line · esc back"
    if picking then
        keys = "↑↓ · enter picks · esc cancels"
    elseif reading then
        keys = "enter selects the question's lines in the file · esc back"
    end
    show.Buf:SetOptionNative("statusformatl", keys)
    local box = paneNamed(boxName)
    if box ~= nil then
        box.Buf:SetOptionNative("statusformatl", label())
    end
end

-- closeColumn puts the column away. What the box holds is kept for when it
-- is back, and so is all that was asked.
local function closeColumn()
    local box = paneNamed(boxName)
    if box ~= nil then
        side.draft = side.mode == "pick" and side.held or written(box)
        box:Quit()
    end
    if side.mode == "pick" then
        side.mode, side.held = side.after, ""
    end
    local show = paneNamed(repliesName)
    if show ~= nil then
        show:Quit()
    end
end

-- openColumn shows the column beside bp, a file's pane, when it isn't in
-- bp's tab: one that is in another tab goes where you are. The keyboard
-- stays where it is.
local function openColumn(bp)
    local tab = bp:Tab()
    local _, at = paneNamed(repliesName)
    if at ~= nil then
        local panes = tab.Panes
        for j = 1, #panes do
            local buf = panes[j].Buf
            if buf ~= nil and buf:GetName() == repliesName then
                return
            end
        end
        closeColumn()
    end
    local active = tab:GetPane(bp:ID())
    local view = bp:GetView()
    local width, height = view.Width, view.Height

    local shown = buffer.NewBuffer("", repliesName)
    shown.Type.Scratch = true
    shown:SetOptionNative("softwrap", true)
    shown:SetOptionNative("wordwrap", true)
    shown:SetOptionNative("filetype", "marmota-replies")
    shown:SetOptionNative("ruler", false)
    shown:SetOptionNative("statusformatr", "")
    local show = bp:VSplitIndex(shown, true)
    bp:ResizePane(math.floor(width * (1 - columnShare)))

    local text = buffer.NewBuffer(side.draft, boxName)
    text.Type.Scratch = true
    text:SetOptionNative("softwrap", true)
    text:SetOptionNative("wordwrap", true)
    text:SetOptionNative("ruler", false)
    text:SetOptionNative("statusformatr", "")
    local box = show:HSplitIndex(text, true)
    show:ResizePane(height - boxRows - 1)
    box.Cursor:GotoLoc(text:End())
    shown.Type.Readonly = true

    tab:SetActive(active)
    render()
end

-- toBox gives the box the keyboard.
local function toBox()
    local box, tab, index = paneNamed(boxName)
    if box ~= nil then
        tab:SetActive(index)
    end
end

-- filePane is the pane a question is about, while it is still open beside
-- the column.
local function filePane()
    local _, tab = paneNamed(repliesName)
    if tab == nil or side.file == nil then
        return nil
    end
    local panes = tab.Panes
    for j = 1, #panes do
        if panes[j]:ID() == side.file:ID() then
            return panes[j], tab, j - 1
        end
    end
    return nil
end

-- toFile gives the keyboard back to the file.
local function toFile()
    local pane, tab, index = filePane()
    if pane ~= nil then
        tab:SetActive(index)
        return
    end
    local _, beside = paneNamed(repliesName)
    if beside ~= nil then
        beside:SetActive(0)
    end
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

-- reference is what a question says of what it is about: the file and the
-- selected lines by name, for the session to read.
local function reference(bp)
    local name = mention(bp)
    if session.agent == codex then
        return "This is about " .. name .. "."
    end
    return name
end

-- editRequest is what cmd+K adds to what you ask for: that the change is
-- made in the file itself, which this editor shows as soon as it is.
local function editRequest(bp)
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
local function wait(index)
    local entry = asked[index]
    shell.JobSpawn(marmota, { "session", "ask", "--sent", "--keep-waiting", entry.session, entry.message },
        nil, nil, onReply, index)
end

onReply = function(output, args)
    -- its place in asked, not the entry: a table handed to a job comes
    -- back a copy
    local entry = asked[args[1]]
    output = strings.TrimSpace(output)
    local failed = output:match("y session ask: (.*)$")
    if failed ~= nil and failed:find("it stopped to ask you", 1, true) and entry.needsYou == nil then
        -- it needs your answer: said once, and waited for. Where to answer
        -- is this editor's to say (cmd+O), not y's.
        entry.needsYou = failed:gsub("%s*%(.*%)$", "")
        micro.InfoBar():Error(entry.title .. " stopped to ask you something: cmd+O opens it to answer")
        wait(args[1])
        render()
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
            local now = fileLines(entry.edits, true)
            if now ~= nil and entry.before ~= nil then
                local first, last = changedSpan(entry.before, now)
                if first ~= nil then
                    entry.changed = { first, last }
                end
            end
            micro.InfoBar():Message(entry.title .. " changed " .. filepath.Base(entry.edits) .. ": cmd+Z takes it back")
        else
            micro.InfoBar():Message(entry.title .. " replied: cmd+enter puts it at the cursor")
        end
    end
    arrived = arrived + 1
    entry.arrived = arrived
    render()
end

-- onStarted has the session a question started: the question is its first
-- message, and its reply is waited for as any other.
local function onStarted(output, args)
    local entry = asked[args[1]]
    local id = output:match("started (%S+)")
    if id == nil then
        entry.reply = "⚠ " .. strings.TrimSpace(output)
        arrived = arrived + 1
        entry.arrived = arrived
        micro.InfoBar():Error("Couldn't start a session: " .. strings.TrimSpace(output))
        render()
        return
    end
    entry.starting = false
    entry.session = id
    if session.id == newSession and session.agent == entry.agent then
        -- the next questions go on with it
        session.id, session.title, session.host = id, entry.title, "here"
    end
    shell.JobSpawn(marmota, { "session", "ask", "--sent", id, entry.message }, nil, nil, onReply, args[1])
    render()
end

-- send asks the session: question, and about it either what it is about
-- (cmd+L) or, with edit, that it changes the file itself (cmd+K). bp is the
-- file's pane, nil when it has been closed since: the question then goes by
-- itself.
local function send(bp, question, edit)
    local file = bp ~= nil and bp.Buf.Path ~= ""
    if file and bp.Buf:Modified() then
        bp:Save()
    end
    local message = question
    if file then
        message = question .. "\n\n" .. (edit and editRequest(bp) or reference(bp))
    end
    local entry = { title = session.title, question = question, message = message, session = session.id,
        agent = session.agent }
    if file then
        entry.place = placeOf(bp)
    end
    if edit and file then
        entry.edits = bp.Buf.AbsPath
        entry.before = linesOf(util.String(bp.Buf:Bytes()))
    end
    table.insert(asked, entry)
    if session.id == newSession then
        entry.starting = true
        -- a session of this editor's own: Marmota's Sessions box leaves it
        -- out, and it closes when the editor does
        shell.JobSpawn(marmota, { "session", "start", "--temporary", "--agent", session.agent, "--cwd", folder(),
            "--", message }, nil, nil, onStarted, #asked)
    else
        shell.JobSpawn(marmota, { "session", "ask", session.id, message }, nil, nil, onReply, #asked)
    end
    render()
    micro.InfoBar():Message("Asked " .. session.title .. "; go on writing")
end

-- padded is text with spaces after it, up to width characters.
local function padded(text, width)
    return text .. string.rep(" ", width - util.CharacterCountInString(text))
end

-- rowText is a row of the list: who runs the session, what it does, its
-- title and its folder. A long one is cut at the column's edge.
local function rowText(agent, does, title, where)
    local out = padded(agent, 13) .. "  " .. padded(does, 11) .. " " .. title
    if where ~= nil and where ~= "" then
        out = out .. "  " .. where
    end
    return out
end

-- newRows are the list's first rows: a session not started yet, by each
-- agent. They are there at once; the sessions that run follow when y has
-- listed them.
local function newRows()
    local name = filepath.Base(folder())
    return {
        { id = newSession, agent = "cc", host = "here", title = "new · " .. name,
            text = padded("✳ Claude Code", 13) .. "  + New session in " .. name },
        { id = newSession, agent = codex, host = "here", title = "new Codex · " .. name,
            text = padded("◆ Codex", 13) .. "  + New session in " .. name },
    }
end

-- onListed has the sessions y listed: a line each, tab-separated, the id,
-- the agent, the host, the title and the row to show. That row is written
-- for a picker as wide as a terminal, in colors of its own: its parts, which
-- two spaces or more part, are put together again for the column, and
-- syntax/marmota-replies.yaml colors them here.
local function onListed(output, args)
    if args[1] ~= side.listing then
        return -- an earlier listing: the list was opened again since
    end
    side.listing = 0
    local rows = newRows()
    for line in (output .. "\n"):gmatch("(.-)\r?\n") do
        local fields = {}
        for field in (line .. "\t"):gmatch("(.-)\t") do
            table.insert(fields, field)
        end
        if #fields >= 5 then
            local text = fields[5]:gsub("\27%[[%d;]*m", "")
            local parts = {}
            for part in (text .. "  "):gmatch("(.-)%s%s+") do
                if part ~= "" then
                    table.insert(parts, part)
                end
            end
            if #parts >= 3 then
                text = rowText(parts[1], parts[2], parts[3], parts[4])
            end
            table.insert(rows, { id = fields[1], agent = fields[2], host = fields[3], title = fields[4],
                text = text })
        end
    end
    local failed = output:match("y session list: ([^\r\n]*)")
    if failed ~= nil then
        micro.InfoBar():Error("Couldn't list the sessions: " .. failed)
    end
    side.rows = rows
    if side.mode ~= "pick" then
        return
    end
    if side.filter == "" and side.sel == 1 then
        -- the mark starts on the session questions go to now
        for i, row in ipairs(rows) do
            if row.id == session.id and row.id ~= newSession then
                side.sel = i
            end
        end
    end
    render()
end

-- pick has the column list the sessions, for the box to filter and enter to
-- pick one; the box is then for after again, "ask" or "edit", with what it
-- held.
local function pick(after)
    local box = paneNamed(boxName)
    if side.mode ~= "pick" then
        side.after = after
        side.held = box ~= nil and written(box) or ""
        side.mode = "pick"
    end
    if box ~= nil then
        write(box, "")
    end
    side.filter, side.sel = "", 1
    side.rows = newRows()
    side.listed = side.listed + 1
    side.listing = side.listed
    -- this computer's only: asking the others takes a second and more
    shell.JobSpawn(marmota, { "session", "list", "--rows", "--local" }, nil, nil, onListed, side.listing)
    render()
end

-- endPick goes back to what the box was for, with what it held.
local function endPick()
    side.mode = side.after
    local box = paneNamed(boxName)
    if box ~= nil then
        write(box, side.held)
    end
    side.held = ""
    render()
end

-- enter in the box: the marked session is picked, or what is written is
-- sent and the cursor goes back to the file.
local function submit(box)
    if side.mode == "pick" then
        local row = side.shown[side.sel]
        if row == nil then
            return
        end
        session.id, session.agent, session.host, session.title = row.id, row.agent, row.host, row.title
        micro.InfoBar():Message("Questions go to " .. session.title)
        endPick()
        return
    end
    if session.id == "" then
        pick(side.mode) -- nobody to ask yet
        return
    end
    local text = strings.TrimSpace(written(box))
    if text == "" then
        return
    end
    local file = filePane()
    local edit = side.mode == "edit"
    if edit and (file == nil or file.Buf.Path == "") then
        micro.InfoBar():Error("The file to change is closed: cmd+K in a file asks again")
        return
    end
    write(box, "")
    side.mode = "ask"
    send(file, text, edit)
    toFile()
end

-- The box's keys. micro asks a plugin before it does what a key is bound
-- to, by the action's name, and leaves it when the plugin says false.

-- paneWith is the pane that has the file open: the one questions came from
-- when it still has it, else the first, and the tab it is in, both counted
-- from 0.
local function paneWith(path)
    local first = nil
    local tabs = micro.Tabs().List
    for i = 1, #tabs do
        local panes = tabs[i].Panes
        for j = 1, #panes do
            local buf = panes[j].Buf
            if buf ~= nil and buf.AbsPath == path then
                local found = { panes[j], i - 1, j - 1 }
                if side.file ~= nil and panes[j]:ID() == side.file:ID() then
                    return found[1], found[2], found[3]
                end
                first = first or found
            end
        end
    end
    if first == nil then
        return nil
    end
    return first[1], first[2], first[3]
end

-- backTo is enter on a question in the replies: the keyboard goes back to
-- its file, with the lines it was about selected where they are now; those
-- the session changed, once it has, for cmd+K's. Lines changed since are
-- selected where they were, and the status line says so. A file no pane
-- has open any more opens in a new tab.
local function backTo(replies)
    local a = asked[shownFor[replies.Cursor.Y]]
    if a == nil or a.place == nil then
        micro.InfoBar():Message("This question wasn't about a file")
        return
    end
    local place = a.place
    local first, last, changed = placeNow(a)
    local pane, tab, index = paneWith(place.path)
    if pane == nil then
        -- named from this editor's folder when it is in it, as the tab shows it
        local name, dir = place.path, folder()
        if dir ~= "" and strings.HasPrefix(name, dir .. "/") then
            name = name:sub(#dir + 2)
        end
        local quoted = "'" .. name:gsub("'", "'\\''") .. "'"
        replies:HandleCommand("tab " .. quoted)
        pane = micro.CurPane()
        if pane == nil or pane.Buf.AbsPath ~= place.path then
            micro.InfoBar():Error("Couldn't open " .. place.path)
            return
        end
    else
        micro.Tabs():SetActive(tab)
        micro.Tabs().List[tab + 1]:SetActive(index)
    end
    local buf = pane.Buf
    local lastLine = buf:LinesNum()
    if first == nil then
        -- the whole file was asked about: back to the cursor's line then
        pane.Cursor:ResetSelection()
        pane.Cursor:GotoLoc(buffer.Loc(0, math.min(place.first, lastLine) - 1))
        show(pane)
        return
    end
    first, last = math.min(first, lastLine), math.min(last, lastLine)
    select(pane, buffer.Loc(0, first - 1), lineEnd(buf, last - 1), true)
    show(pane)
    if changed then
        micro.InfoBar():Message(filepath.Base(place.path) .. " " .. span(first, last)
            .. ": these lines changed since you asked")
    end
end

-- onMouseRelease: a click on a question in the replies (the line that says
-- whom, or what you asked) goes back to what it was about, as enter does. A
-- drag that selected text, to copy it, stays where it is.
function onMouseRelease(bp)
    if bp.Buf:GetName() ~= repliesName or side.mode == "pick" or bp.Cursor:HasSelection() then
        return true
    end
    local line = bp.Buf:Line(bp.Cursor.Y)
    if strings.HasPrefix(line, "You → ") or strings.HasPrefix(line, highlighted) then
        backTo(bp)
    end
    return true
end

-- enter in the box sends, or picks. In the replies, it goes back to what
-- the question there was about.
function preInsertNewline(bp)
    local name = bp.Buf:GetName()
    if name == repliesName then
        if side.mode ~= "pick" then
            backTo(bp)
        end
        return false
    end
    if name ~= boxName then
        return true
    end
    submit(bp)
    return false
end

-- newLine is shift+enter (WezTerm sends a line feed for it, ctrl+J): a new
-- line in the box. Elsewhere it is what micro has ctrl+J do.
function newLine(bp)
    if bp.Buf:GetName() ~= boxName then
        return bp:PlayMacro()
    end
    if side.mode ~= "pick" then
        bp:InsertNewline()
    end
    return true
end

-- up and down in the box move the mark while a session is picked.
local function mark(bp, step)
    if bp.Buf:GetName() ~= boxName or side.mode ~= "pick" then
        return true
    end
    side.sel = math.max(1, math.min(#side.shown, side.sel + step))
    render()
    return false
end

function preCursorUp(bp)
    return mark(bp, -1)
end

function preCursorDown(bp)
    return mark(bp, 1)
end

-- escape is esc. In the column it goes back to the file; while a session
-- is picked it leaves the list first, for the box with what it held when
-- there is a session to ask. Elsewhere it is all that micro has esc do. It
-- is bound to the key itself, not asked before micro's own: micro goes on
-- with the rest of what esc does, which takes the selection off the pane
-- that has the keyboard, by then the file's, the lines a question is about.
function escape(bp)
    if not inColumn(bp) then
        bp:Escape()
        bp:Deselect()
        bp:ClearInfo()
        bp:RemoveAllMultiCursors()
        bp:UnhighlightSearch()
        return true
    end
    if side.mode == "pick" then
        endPick()
        if session.id ~= "" and bp.Buf:GetName() == boxName then
            return true
        end
    end
    toFile()
    return true
end

-- Closing one of the column's panes (cmd+W) puts the column away.
function preQuit(bp)
    if not inColumn(bp) then
        return true
    end
    closeColumn()
    return false
end

-- What is typed in the box filters the sessions while one is picked. micro
-- says when anything happened, not what: the box is read again. Otherwise
-- the replies are drawn again, for their keys, which say what the pane with
-- the keyboard does (render changes nothing when that is as it was).
function onAnyEvent()
    if side.mode ~= "pick" then
        if #asked > 0 then
            render()
        end
        return
    end
    local box = paneNamed(boxName)
    if box == nil then
        return
    end
    local filter = written(box)
    if filter ~= side.filter then
        side.filter, side.sel = filter, 1
        render()
    end
end

-- copyReference is cmd+shift+C: the file and the selected lines, or the
-- cursor's line, copied as Claude Code reads them pasted in a session,
-- "@src/app.go#L3-9", the path from the git repository's top (else as it
-- is), as nvim's gy writes it.
function copyReference(bp)
    local path = bp.Buf.AbsPath
    if bp.Buf.Path == "" or path == "" then
        micro.InfoBar():Error("This buffer is no file")
        return true
    end
    local top = shell.ExecCommand("git", "-C", filepath.Dir(path), "rev-parse", "--show-toplevel")
    top = strings.TrimSpace(top or "")
    if top ~= "" and strings.HasPrefix(path, top .. "/") then
        path = string.sub(path, #top + 2)
    end
    if path:find(" ") then
        path = '"' .. path .. '"'
    end
    local first, last = bp.Cursor.Y, bp.Cursor.Y
    local from, to = selection(bp.Cursor)
    if from ~= nil then
        first, last = from.Y, to.Y
        if to.X == 0 and to.Y > from.Y then
            last = to.Y - 1 -- the selection ends before this line's first character
        end
    end
    local ref = "@" .. path .. "#L" .. (first + 1)
    if last > first then
        ref = ref .. "-" .. (last + 1)
    end
    local _, err = shell.ExecCommand("sh", "-c",
        'printf %s "$1" | { pbcopy 2>/dev/null || wl-copy 2>/dev/null || xclip -selection clipboard; }',
        "sh", ref)
    if err ~= nil then
        micro.InfoBar():Error("Could not copy " .. ref .. ": " .. err:Error())
        return true
    end
    micro.InfoBar():Message("Copied " .. ref)
    return true
end

-- from has the column open beside the pane a key was pressed in, which a
-- question is then about. A key pressed in the column leaves that as it is.
local function from(bp)
    if not inColumn(bp) then
        side.file, side.about = bp, aboutOf(bp)
        openColumn(bp)
    end
end

-- boxFor has the box be for mode, "ask" or "edit": after a session is
-- picked, when there is none yet to ask.
local function boxFor(mode)
    if session.id == "" then
        pick(mode)
        return
    end
    if side.mode == "pick" then
        side.after = mode
        endPick()
        return
    end
    side.mode = mode
    render()
end

-- ask is cmd+L: the cursor goes to the box, for a question to the session
-- about this file. From the box, it goes back to the file.
function ask(bp)
    if bp.Buf:GetName() == boxName then
        if side.mode == "pick" then
            endPick()
        end
        toFile()
        return true
    end
    from(bp)
    boxFor("ask")
    toBox()
    return true
end

-- askAnother is cmd+shift+L: the sessions are listed above the box, to pick
-- the one questions go to.
function askAnother(bp)
    from(bp)
    pick(side.mode == "edit" and "edit" or "ask")
    toBox()
    return true
end

-- edit is cmd+K: the box takes what the session is to change in the
-- selected lines, or the file, which it changes in the file itself; this
-- editor shows the change when it is made, and cmd+Z takes it back.
function edit(bp)
    local file = bp
    if inColumn(bp) then
        file = filePane()
    end
    if file == nil or file.Buf.Path == "" then
        micro.InfoBar():Message("Save the file first: the session changes it on disk")
        return true
    end
    from(bp)
    boxFor("edit")
    toBox()
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
    if inColumn(bp) then
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

-- openSession is cmd+O: the session's own window, in a pane under the
-- editor, to answer what it asks or to watch it work. It is the session
-- that waits for your answer, else the one questions go to. Closing the
-- pane leaves the session running.
function openSession(bp)
    local id, title = nil, nil
    for i = #asked, 1, -1 do
        local a = asked[i]
        if a.reply == nil and a.needsYou ~= nil and a.session ~= newSession then
            id, title = a.session, a.title
            break
        end
    end
    if id == nil and session.id ~= "" and session.id ~= newSession then
        id, title = session.id, session.title
    end
    if id == nil then
        micro.InfoBar():Message("No session to open yet: cmd+L asks one")
        return true
    end
    shell.JobSpawn(marmota, { "session", "show", id }, nil, nil, function(output, args)
        local failed = strings.TrimSpace(output):match("y session show: (.*)$")
        if failed ~= nil then
            micro.InfoBar():Error("Couldn't open " .. args[1] .. ": " .. failed)
        else
            micro.InfoBar():Message(args[1] .. " is open below: close that pane when you are done, it goes on")
        end
    end, title)
    return true
end

-- toggleReplies is cmd+J: the column, shown or put away. What was asked,
-- and what the box holds, is there again when it is shown.
function toggleReplies(bp)
    if paneNamed(repliesName) ~= nil then
        closeColumn()
    else
        from(bp)
    end
    return true
end

-- holds counts the files micro holds and those of them that aren't saved, a
-- file shown in two panes once. The replies' pane and the page of keys are
-- no files.
local function holds()
    local seen, files, unsaved = {}, 0, 0
    local tabs = micro.Tabs().List
    for i = 1, #tabs do
        local panes = tabs[i].Panes
        for j = 1, #panes do
            local b = panes[j].Buf
            if b ~= nil and b.Type.Kind == buffer.BTDefault and not b.Type.Scratch then
                local key = b.AbsPath
                if key == "" then
                    key = i .. ":" .. j -- not yet a file: each pane's own
                end
                if not seen[key] then
                    seen[key] = true
                    files = files + 1
                    if b:Modified() then
                        unsaved = unsaved + 1
                    end
                end
            end
        end
    end
    return files, unsaved
end

-- putAway is cmd+M: micro stops, with its files, tabs, undo history and the
-- replies as they are, and Marmota, whose e opened it, takes the terminal
-- back. b in Marmota continues it, and it goes on from here. Before it
-- stops it writes what it holds for Marmota, in the file Marmota named; when
-- it continues it reads what Marmota wants of it beside that file: "quit"
-- when Marmota quits, or "say" and a line to show.
--
-- Only an editor Marmota's e opened is put away. Any other program that
-- opened micro waits for it to end, and would wait for good.
function putAway(bp)
    local state = os.Getenv("MARMOTA_EDITOR_STATE")
    if state == "" then
        if os.Getenv("MARMOTA_EDITOR_AWAY") ~= "" then
            micro.InfoBar():Message("An editor is put away already: close this one, then b in Marmota brings that one back")
        else
            micro.InfoBar():Message("Nothing to go back to here: cmd+M puts micro away where e in Marmota opened it")
        end
        return true
    end
    local files, unsaved = holds()
    local f = io.open(state, "w")
    if f ~= nil then
        f:write(files .. " " .. unsaved .. "\n")
        f:close()
    end

    bp:Suspend() -- comes back when Marmota continues micro

    local order = ""
    f = io.open(state .. ".do", "r")
    if f ~= nil then
        order = f:read("*l") or ""
        f:close()
        os.Remove(state .. ".do")
    end
    if order == "quit" then
        bp:QuitAll()
    elseif order:sub(1, 4) == "say " then
        micro.InfoBar():Message(order:sub(5))
    end
    return true
end
