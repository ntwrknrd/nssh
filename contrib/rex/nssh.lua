-- nssh actions for the Rex command palette.
--
-- Install: copy or link this file to ~/.config/rex/nssh.lua, add
--
--   require("nssh")
--
-- to ~/.config/rex/init.lua, and run `rex config reload`. To change the nssh
-- executable, use require("nssh").setup{ nssh = "/path/to/nssh" }.
--
-- Actions:
--   nssh_connect  pick hosts and open them as nssh panes
--   nssh_run      pick hosts, enter a command, and run it across them
--
-- Both also run from the CLI: rex do nssh_connect hosts=core1,core2
--                             rex do nssh_run hosts=core1,core2 command='show version'

local M = { options = { nssh = "nssh" } }

-- rex.ui.pick accepts at most this many items.
local MAX_ITEMS = 2000

function M.setup(options)
  for key, value in pairs(options or {}) do
    M.options[key] = value
  end
end

local function inventory()
  local result, err = rex.exec({ M.options.nssh, "inv", "list", "--json" }, { timeout = 30 })
  if not result then
    error("nssh: " .. tostring(err), 0)
  end
  if not result.ok then
    local why = result.stderr ~= "" and result.stderr or ("exit status " .. tostring(result.code))
    error("nssh inv list failed: " .. why, 0)
  end
  local data, decode_err = rex.json.decode(result.stdout)
  if not data then
    error("nssh inv list: " .. tostring(decode_err), 0)
  end
  return data.hosts or {}
end

-- Inventories larger than the picker limit are narrowed by group first.
local function narrow_by_group(title, hosts)
  local counts, groups = {}, {}
  for _, host in ipairs(hosts) do
    local group = host.group or "(no group)"
    if not counts[group] then
      counts[group] = 0
      groups[#groups + 1] = group
    end
    counts[group] = counts[group] + 1
  end
  table.sort(groups)
  if #groups > MAX_ITEMS then
    error("nssh: too many groups to pick from; pass hosts=", 0)
  end
  local items = {}
  for _, group in ipairs(groups) do
    items[#items + 1] = { id = group, title = group, subtitle = counts[group] .. " hosts", icon = "folder" }
  end
  local picked = rex.ui.pick({ title = title .. ": choose a group", placeholder = "Search groups", items = items })
  if not picked then
    return nil
  end
  local narrowed = {}
  for _, host in ipairs(hosts) do
    if (host.group or "(no group)") == picked then
      narrowed[#narrowed + 1] = host
    end
  end
  if #narrowed > MAX_ITEMS then
    error("nssh: group " .. picked .. " has more than " .. MAX_ITEMS .. " hosts; pass hosts=", 0)
  end
  return narrowed
end

local function split_hosts(text)
  local hosts = {}
  for name in string.gmatch(text, "[^,%s]+") do
    hosts[#hosts + 1] = name
  end
  return hosts
end

-- Returns the chosen host names, or nil when the operator cancels.
local function choose_hosts(title, given)
  if given and given ~= "" then
    return split_hosts(given)
  end
  local hosts = inventory()
  if #hosts == 0 then
    error("nssh: the inventory is empty", 0)
  end
  if #hosts > MAX_ITEMS then
    hosts = narrow_by_group(title, hosts)
    if not hosts then
      return nil
    end
  end
  local items = {}
  for _, host in ipairs(hosts) do
    items[#items + 1] = {
      id = host.host,
      title = host.host,
      subtitle = host.group,
      detail = host.hostname,
      icon = "server",
    }
  end
  local picked = rex.ui.pick({ title = title, placeholder = "Search hosts", items = items, multiple = true })
  if not picked or #picked == 0 then
    return nil
  end
  return picked
end

rex.action({
  name = "nssh_connect",
  title = "nssh: Connect to Hosts",
  description = "Pick inventory hosts and open each in an nssh pane.",
  category = "nssh",
  keywords = "ssh connect host server inventory",
  args = { hosts = "string?" },
  run = function(ctx, args)
    local hosts = choose_hosts("Connect to hosts", args.hosts)
    if not hosts then
      return
    end
    -- nssh places the panes itself, using the pane that ran the action to
    -- find the session and size splits. The action has no terminal, so
    -- NSSH_REX_OPEN asks nssh to open even a single host in a tab.
    local env = {
      REX_SESSION = ctx.session_id or "",
      REX_BLOCK = ctx.block_id or "none",
      NSSH_REX_OPEN = "1",
    }
    local result, err = rex.exec({ M.options.nssh, table.concat(hosts, ",") }, { env = env, stdin = "", timeout = 30 })
    if not result then
      error("nssh: " .. tostring(err), 0)
    end
    if not result.ok then
      local why = result.stderr ~= "" and result.stderr or result.stdout
      error("nssh: " .. why, 0)
    end
    rex.log("info", "nssh: " .. result.stderr .. result.stdout)
  end,
})

rex.action({
  name = "nssh_run",
  title = "nssh: Run Command on Hosts",
  description = "Pick inventory hosts and run one command across them in a new tab.",
  category = "nssh",
  keywords = "ssh run command batch hosts",
  args = { hosts = "string?", command = "string?" },
  run = function(ctx, args)
    local hosts = choose_hosts("Run on hosts", args.hosts)
    if not hosts then
      return
    end
    local command = args.command
    if not command or command == "" then
      command = rex.ui.input({ title = "Command for " .. #hosts .. " hosts", placeholder = "show version" })
    end
    if not command or command == "" then
      return
    end
    -- The tab stays open after the run so its results can be read.
    local _, err = rex.call("session.new_window", {
      session_id = ctx.session_id,
      window_label = command,
      layout = {
        block = {
          flavor = "com.superlogical.terminal.shell",
          label = "nssh: " .. command,
          options = {
            shell = "none",
            command = { M.options.nssh, table.concat(hosts, ","), command },
            exit = { on_completion = false },
          },
        },
      },
    })
    if err then
      error("nssh: " .. tostring(err), 0)
    end
  end,
})

return M
