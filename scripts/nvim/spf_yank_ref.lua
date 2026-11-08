-- spf_yank_ref.lua
--
-- Yank a reference to the lines you're on instead of the text itself, in the
-- form Claude Code attaches when you paste it into a session: an @mention
-- with the lines after it.
--
-- Useful when superfile opens a file with `e` (nvim) and you want to hand a
-- location to a Claude session rather than the code itself.
--
--   normal mode           gy / <leader>yl  ->  @src/internal/render.go#L42 (repo-relative)
--   visual mode (3 lines) gy / <leader>yl  ->  @src/internal/render.go#L42-44
--   either mode           gY / <leader>yL  ->  @/abs/path/src/internal/render.go#L42
--   either mode           <leader>yf       ->  /abs/path/src/internal/render.go
--
-- A path with spaces is quoted, @"my notes.md"#L3, which Claude Code reads too.
--
-- `gy` is the reliable one: `g` is a builtin prefix, so nvim waits for the next
-- key forever. A `<leader>` sequence is abandoned after 'timeoutlen' (400ms in
-- NvChad), after which <Space> runs as a motion instead.
--
-- Install: drop this file in ~/.config/nvim/lua/ and add
--   require "spf_yank_ref"
-- to your config (NvChad: lua/mappings.lua). `setup-macos-hotkeys.sh --neovim`
-- does both for you.

local M = {}

M.config = {
  -- Registers the reference is written to. "+" is the system clipboard.
  registers = { "+", '"' },
  -- Path shown in a `gy` reference: "git" (relative to the repo root, falling
  -- back to cwd), "cwd" (relative to cwd) or "absolute".
  path_style = "git",
  -- Path shown by `gY` and by the plain path yank.
  absolute_path_style = "absolute",
  -- Joins the two line numbers of a multi-line selection: "42-44".
  range_separator = "-",
  -- Set to false to keep your own mappings only.
  default_keymaps = true,
  -- Each action takes one lhs or a list of them.
  keymaps = {
    reference = { "gy", "<leader>yl" },
    absolute_reference = { "gY", "<leader>yL" },
    path = { "<leader>yf" },
  },
  notify = true,
}

local function git_root(abs_path)
  local out = vim.fn.systemlist({
    "git", "-C", vim.fs.dirname(abs_path), "rev-parse", "--show-toplevel",
  })
  if vim.v.shell_error ~= 0 or not out[1] or out[1] == "" then
    return nil
  end
  return out[1]
end

-- Strips `base` from `abs_path`, or returns nil when abs_path lives elsewhere.
local function strip_base(abs_path, base)
  if not base or base == "" then
    return nil
  end
  base = base:gsub("/+$", "") .. "/"
  if abs_path:sub(1, #base) ~= base then
    return nil
  end
  return abs_path:sub(#base + 1)
end

local function display_path(abs_path, style)
  if style == "absolute" then
    return abs_path
  end
  if style == "git" then
    local rel = strip_base(abs_path, git_root(abs_path))
    if rel then
      return rel
    end
  end
  return strip_base(abs_path, vim.uv.cwd()) or abs_path
end

local function current_path(style)
  local abs_path = vim.api.nvim_buf_get_name(0)
  if abs_path == "" then
    vim.notify("spf_yank_ref: buffer has no file name", vim.log.levels.WARN)
    return nil
  end
  return display_path(vim.fn.fnamemodify(abs_path, ":p"), style)
end

-- In visual mode "v" is the anchor and "." the cursor; either can be first.
local function visual_range()
  local anchor, cursor = vim.fn.line("v"), vim.fn.line(".")
  if anchor > cursor then
    anchor, cursor = cursor, anchor
  end
  return anchor, cursor
end

local function leave_visual_mode()
  if vim.fn.mode():match("^[vV\22]") then
    vim.cmd("normal! \27")
  end
end

local function yank(text)
  for _, register in ipairs(M.config.registers) do
    vim.fn.setreg(register, text)
  end
  if M.config.notify then
    vim.notify("Yanked " .. text)
  end
end

--- Builds a reference without yanking it, e.g. "@src/render.go#L42-44".
--- @param first integer|nil first line (defaults to the cursor line)
--- @param last integer|nil last line (defaults to `first`)
--- @param style string|nil overrides `config.path_style`
--- @return string|nil
function M.reference(first, last, style)
  local path = current_path(style or M.config.path_style)
  if not path then
    return nil
  end
  first = first or vim.fn.line(".")
  last = last or first
  local mention = "@" .. path
  if path:find("%s") then
    mention = '@"' .. path .. '"'
  end
  if first == last then
    return mention .. "#L" .. first
  end
  return mention .. "#L" .. first .. M.config.range_separator .. last
end

--- Yanks a reference for the cursor line or the visual selection.
--- @param opts table|nil { visual = boolean, style = string }
function M.yank_reference(opts)
  opts = opts or {}
  local first, last
  if opts.visual then
    first, last = visual_range()
    leave_visual_mode()
  end
  local reference = M.reference(first, last, opts.style)
  if reference then
    yank(reference)
  end
end

--- Yanks the file path with no line numbers.
--- @param opts table|nil { style = string }
function M.yank_path(opts)
  opts = opts or {}
  leave_visual_mode()
  local path = current_path(opts.style or M.config.absolute_path_style)
  if path then
    yank(path)
  end
end

local function create_keymaps()
  local keys = M.config.keymaps
  local map = function(lhs, modes, fn, desc)
    for _, key in ipairs(type(lhs) == "table" and lhs or { lhs }) do
      if key and key ~= "" then
        vim.keymap.set(modes, key, fn, { desc = desc, silent = true })
      end
    end
  end

  map(keys.reference, "n", function()
    M.yank_reference()
  end, "Yank @file#L reference")
  map(keys.reference, "x", function()
    M.yank_reference({ visual = true })
  end, "Yank @file#L-range reference")

  map(keys.absolute_reference, "n", function()
    M.yank_reference({ style = M.config.absolute_path_style })
  end, "Yank absolute @file#L reference")
  map(keys.absolute_reference, "x", function()
    M.yank_reference({ visual = true, style = M.config.absolute_path_style })
  end, "Yank absolute @file#L-range reference")

  map(keys.path, { "n", "x" }, function()
    M.yank_path()
  end, "Yank file path")
end

local function create_commands()
  -- :SpfYankRef yanks the cursor line, :'<,'>SpfYankRef the range, and the
  -- bang variant uses the absolute path instead of the relative one.
  vim.api.nvim_create_user_command("SpfYankRef", function(args)
    local style = args.bang and M.config.absolute_path_style or nil
    if args.range > 0 then
      local reference = M.reference(args.line1, args.line2, style)
      if reference then
        yank(reference)
      end
    else
      M.yank_reference({ style = style })
    end
  end, { range = true, bang = true, force = true, desc = "Yank an @file#L reference" })

  -- :SpfYankPath yanks the absolute path, :SpfYankPath! the relative one.
  vim.api.nvim_create_user_command("SpfYankPath", function(args)
    M.yank_path({ style = args.bang and M.config.path_style or nil })
  end, { bang = true, force = true, desc = "Yank the file path" })
end

--- @param opts table|nil overrides merged into `M.config`
function M.setup(opts)
  opts = opts or {}
  M.config = vim.tbl_deep_extend("force", M.config, opts)
  -- tbl_deep_extend merges lists index by index; a shorter list must win.
  if opts.registers then
    M.config.registers = opts.registers
  end
  for action, lhs in pairs(opts.keymaps or {}) do
    M.config.keymaps[action] = lhs
  end
  if M.config.default_keymaps then
    create_keymaps()
  end
  create_commands()
end

M.setup()

return M
