VERSION = "0.1.0"

-- Marmota's part of micro, the editor Marmota opens files in: the keys a Mac
-- text field has that micro lacks, and asking one of Marmota's sessions about
-- what you write. build.sh installs it with bindings.json, which names these
-- functions.

local buffer = import("micro/buffer")
local config = import("micro/config")
local util = import("micro/util")

function init()
    -- "help marmota" (cmd+/): the keys Marmota sets up
    config.AddRuntimeFile("marmota", config.RTHelp, "help/marmota.md")
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

-- Asking a session while you write. cmd+L asks the session this editor
-- writes to: your question goes to it with the file, and the lines you
-- selected, named after it, so it reads them itself. You go on writing; its
-- reply shows in a pane on the right when its turn has ended. cmd+enter puts
-- the last reply at the cursor, or in place of what is selected. The session
-- is one of Marmota's: what you ask here shows in its Sessions box too.
--
-- All of it goes through `y session`: list for the sessions to pick from,
-- ask to send a question and wait for its reply.

local micro = import("micro")
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
-- when it opened this editor, until you pick another (cmd+shift+L).
local session = {
    id = os.Getenv("MARMOTA_SESSION"),
    title = os.Getenv("MARMOTA_SESSION_TITLE"),
    host = os.Getenv("MARMOTA_SESSION_HOST"),
}

-- asked holds each question of this editor, oldest first: who was asked,
-- the question, and the reply once there is one.
local asked = {}
local repliesName = "Marmota replies"

local function shellQuote(s)
    return "'" .. s:gsub("'", "'\\''") .. "'"
end

-- repliesPane is the pane that shows the replies, when one does.
local function repliesPane()
    local tabs = micro.Tabs().List
    for i = 1, #tabs do
        local panes = tabs[i].Panes
        for j = 1, #panes do
            if panes[j].Buf:GetName() == repliesName then
                return panes[j], tabs[i], j - 1
            end
        end
    end
    return nil
end

local function repliesText()
    local out = {}
    for _, a in ipairs(asked) do
        table.insert(out, "› " .. a.question .. "   — " .. a.title)
        if a.reply == nil then
            table.insert(out, "")
            table.insert(out, "✳ asking…")
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

-- reference names what the question is about: the file, with the selected
-- lines, as Claude Code writes a mention of them, "@notes/plan.md#L12-30".
-- A session on another computer can't read the file, and gets the selected
-- text, or the file's, itself.
local function reference(bp)
    local path = bp.Buf.AbsPath
    local from, to = selection(bp.Cursor)
    if session.host ~= "" and session.host ~= "here" then
        local text
        if from ~= nil then
            text = util.String(bp.Cursor:GetSelection())
        else
            text = util.String(bp.Buf:Bytes())
        end
        return "From " .. filepath.Base(path) .. ":\n\n" .. text
    end
    local mention = "@" .. path
    if path:find(" ") then
        mention = '@"' .. path .. '"'
    end
    if from ~= nil then
        local last = to.Y
        if to.X == 0 and to.Y > from.Y then
            last = to.Y - 1 -- the selection ends before this line's first character
        end
        if last > from.Y then
            mention = mention .. "#L" .. (from.Y + 1) .. "-" .. (last + 1)
        else
            mention = mention .. "#L" .. (from.Y + 1)
        end
    end
    return mention
end

local function onReply(output, args)
    -- its place in asked, not the entry: a table handed to a job comes
    -- back a copy
    local entry, bp = asked[args[1]], args[2]
    output = strings.TrimSpace(output)
    local failed = output:match("y session ask: (.*)$")
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
        micro.InfoBar():Message(entry.title .. " replied: cmd+enter puts it at the cursor")
    end
    showReplies(bp)
end

local function send(bp, question)
    if bp.Buf.Path ~= "" and bp.Buf:Modified() then
        bp:Save()
    end
    local message = question
    if bp.Buf.Path ~= "" then
        message = question .. "\n\n" .. reference(bp)
    end
    local entry = { title = session.title, question = question }
    table.insert(asked, entry)
    shell.JobSpawn(marmota, { "session", "ask", session.id, message }, nil, nil, onReply, #asked, bp)
    showReplies(bp)
    micro.InfoBar():Message("Asked " .. session.title .. "; go on writing")
end

local function promptQuestion(bp)
    micro.InfoBar():Prompt("Ask " .. session.title .. " › ", "", "MarmotaAsk", nil, function(resp, canceled)
        if not canceled and strings.TrimSpace(resp) ~= "" then
            send(bp, strings.TrimSpace(resp))
        end
    end)
end

-- pickSession lists Marmota's sessions to pick the one questions go to,
-- then goes on with after.
local function pickSession(bp, after)
    local list = shellQuote(marmota) .. " session list 2>/dev/null"
    -- the id stays out of sight: the title and the folder say which it is
    local fzf = "fzf --header-lines=1 --with-nth=2.. --layout=reverse --prompt='Ask which session? ' --no-multi"
    local cmd = "sh -c " .. shellQuote(list .. " | " .. fzf)
    local err = shell.RunTermEmulator(bp, cmd, false, true, function(output, args)
        local line = strings.TrimSpace(output)
        if line == "" then
            return
        end
        -- ID  HOST  STATUS  TITLE  FOLDER, two spaces or more apart
        local fields = {}
        for field in (line .. "  "):gmatch("(.-)%s%s+") do
            table.insert(fields, field)
        end
        if #fields < 4 then
            micro.InfoBar():Error("Couldn't read the session picked: " .. line)
            return
        end
        session.id, session.host, session.title = fields[1], fields[2], fields[4]
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

-- insertReply is cmd+enter: the last reply at the cursor, or in place of
-- what is selected.
function insertReply(bp)
    local reply
    for i = #asked, 1, -1 do
        if asked[i].done then
            reply = asked[i].reply
            break
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
