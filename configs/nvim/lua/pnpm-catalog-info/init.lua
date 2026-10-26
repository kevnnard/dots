--- pnpm-catalog-info.nvim
--- Shows latest npm versions as virtual text in pnpm-workspace.yaml
--- Similar UX to package-info.nvim

local M = {}

-- Namespace for virtual text
local ns = vim.api.nvim_create_namespace("pnpm_catalog_info")

-- Cache: package -> { latest, fetched_at }
local cache = {}
local CACHE_TTL = 60 * 60 -- 1 hour in seconds

-- Default config
M.config = {
  colors = {
    up_to_date = "#3C4048",
    outdated = "#d19a66",
  },
  icons = {
    up_to_date = "|  ",
    outdated = "|  ",
  },
  autostart = true,
  hide_up_to_date = false,
}

-- Highlight groups
local function setup_highlights(colors)
  vim.api.nvim_set_hl(0, "PnpmCatalogUpToDate", { fg = colors.up_to_date })
  vim.api.nvim_set_hl(0, "PnpmCatalogOutdated", { fg = colors.outdated })
end

-- Strip leading ^ ~ >= etc from a semver string
local function strip_range(version)
  return version:match("^[%^~>=<v]*(.+)$") or version
end

-- Compare two semver strings: returns true if a < b
local function is_outdated(current, latest)
  local function parse(v)
    local major, minor, patch = v:match("^(%d+)%.(%d+)%.?(%d*)")
    return tonumber(major) or 0, tonumber(minor) or 0, tonumber(patch) or 0
  end
  local cma, cmi, cp = parse(strip_range(current))
  local lma, lmi, lp = parse(strip_range(latest))
  if lma ~= cma then return lma > cma end
  if lmi ~= cmi then return lmi > cmi end
  return lp > cp
end

-- Fetch latest version from npm registry (async via vim.system)
local function fetch_latest(pkg, callback)
  local now = os.time()
  if cache[pkg] and (now - cache[pkg].fetched_at) < CACHE_TTL then
    callback(cache[pkg].latest)
    return
  end

  vim.system(
    { "npm", "view", pkg, "version", "--json" },
    { text = true },
    function(result)
      if result.code ~= 0 or not result.stdout then
        callback(nil)
        return
      end
      -- stdout may be a JSON string like "\"1.2.3\"\n" or plain "1.2.3\n"
      local raw = result.stdout:gsub("%s+", "")
      local version = raw:match('^"(.+)"$') or raw:match("^(.+)$")
      if version and version ~= "" then
        cache[pkg] = { latest = version, fetched_at = os.time() }
        callback(version)
      else
        callback(nil)
      end
    end
  )
end

-- Parse catalog entries from buffer lines
-- Returns list of { line (0-indexed), pkg, version }
local function parse_catalog(bufnr)
  local lines = vim.api.nvim_buf_get_lines(bufnr, 0, -1, false)
  local entries = {}
  local in_catalog = false

  for i, line in ipairs(lines) do
    if line:match("^catalog:") then
      in_catalog = true
    elseif in_catalog then
      -- Stop when we hit a new top-level key (no leading spaces)
      if line:match("^%S") and not line:match("^%s*#") then
        in_catalog = false
      else
        -- Match: "  package-name: version" or "  '@scope/pkg': version"
        local pkg, version = line:match("^%s+([%w@/%.%-_]+):%s*(.+)$")
        if not pkg then
          -- Handle quoted keys like '@hookform/resolvers'
          pkg, version = line:match("^%s+'([^']+)':%s*(.+)$")
        end
        if pkg and version then
          version = version:gsub("%s*#.*$", ""):gsub("%s+$", "") -- strip comments/trailing spaces
          table.insert(entries, { line = i - 1, pkg = pkg, version = version })
        end
      end
    end
  end

  return entries
end

-- Set virtual text on a buffer line
local function set_vtext(bufnr, line, text, hl_group)
  vim.api.nvim_buf_set_extmark(bufnr, ns, line, 0, {
    virt_text = { { text, hl_group } },
    virt_text_pos = "eol",
    hl_mode = "combine",
  })
end

-- Clear all virtual text in buffer
local function clear_vtext(bufnr)
  vim.api.nvim_buf_clear_namespace(bufnr, ns, 0, -1)
end

-- Show versions for a given buffer
local function show(bufnr)
  bufnr = bufnr or vim.api.nvim_get_current_buf()
  local entries = parse_catalog(bufnr)
  if #entries == 0 then
    vim.notify("[pnpm-catalog-info] No catalog entries found", vim.log.levels.WARN)
    return
  end

  clear_vtext(bufnr)

  for _, entry in ipairs(entries) do
    fetch_latest(entry.pkg, function(latest)
      if not latest then return end
      vim.schedule(function()
        if not vim.api.nvim_buf_is_valid(bufnr) then return end
        local outdated = is_outdated(entry.version, latest)
        if M.config.hide_up_to_date and not outdated then return end
        local icon = outdated and M.config.icons.outdated or M.config.icons.up_to_date
        local hl = outdated and "PnpmCatalogOutdated" or "PnpmCatalogUpToDate"
        local text = icon .. latest
        set_vtext(bufnr, entry.line, text, hl)
      end)
    end)
  end
end

-- Hide virtual text
local function hide(bufnr)
  bufnr = bufnr or vim.api.nvim_get_current_buf()
  clear_vtext(bufnr)
end

-- Toggle virtual text
local function toggle(bufnr)
  bufnr = bufnr or vim.api.nvim_get_current_buf()
  local marks = vim.api.nvim_buf_get_extmarks(bufnr, ns, 0, -1, {})
  if #marks > 0 then
    hide(bufnr)
  else
    show(bufnr)
  end
end

-- Update package on current cursor line using vim.ui.select
local function update_current_line()
  local bufnr = vim.api.nvim_get_current_buf()
  local row = vim.api.nvim_win_get_cursor(0)[1] - 1 -- 0-indexed
  local entries = parse_catalog(bufnr)

  local entry = nil
  for _, e in ipairs(entries) do
    if e.line == row then
      entry = e
      break
    end
  end

  if not entry then
    vim.notify("[pnpm-catalog-info] No catalog package on this line", vim.log.levels.WARN)
    return
  end

  -- Fetch versions list from npm
  vim.notify("[pnpm-catalog-info] Fetching versions for " .. entry.pkg .. "...", vim.log.levels.INFO)
  vim.system(
    { "npm", "view", entry.pkg, "versions", "--json" },
    { text = true },
    function(result)
      vim.schedule(function()
        if result.code ~= 0 or not result.stdout then
          vim.notify("[pnpm-catalog-info] Failed to fetch versions for " .. entry.pkg, vim.log.levels.ERROR)
          return
        end

        -- Parse JSON array of versions
        local ok, versions = pcall(vim.json.decode, result.stdout)
        if not ok or type(versions) ~= "table" then
          vim.notify("[pnpm-catalog-info] Could not parse versions", vim.log.levels.ERROR)
          return
        end

        -- Reverse so latest is first
        local reversed = {}
        for i = #versions, 1, -1 do
          table.insert(reversed, versions[i])
        end

        vim.ui.select(reversed, {
          prompt = "Select version for " .. entry.pkg .. " (current: " .. entry.version .. "):",
        }, function(choice)
          if not choice then return end

          -- Replace version in the buffer line
          local line = vim.api.nvim_buf_get_lines(bufnr, row, row + 1, false)[1]
          local new_line = line:gsub(
            vim.pesc(entry.version),
            choice,
            1
          )
          vim.api.nvim_buf_set_lines(bufnr, row, row + 1, false, { new_line })

          -- Invalidate cache so next show() re-fetches
          cache[entry.pkg] = nil

          -- Refresh virtual text
          show(bufnr)
          vim.notify(
            "[pnpm-catalog-info] Updated " .. entry.pkg .. " → " .. choice,
            vim.log.levels.INFO
          )
        end)
      end)
    end
  )
end

-- Setup function called by Lazy
function M.setup(opts)
  M.config = vim.tbl_deep_extend("force", M.config, opts or {})
  setup_highlights(M.config.colors)

  -- Re-apply highlights on colorscheme change
  vim.api.nvim_create_autocmd("ColorScheme", {
    callback = function() setup_highlights(M.config.colors) end,
  })

  -- Autostart when opening pnpm-workspace.yaml
  if M.config.autostart then
    vim.api.nvim_create_autocmd("BufRead", {
      pattern = "pnpm-workspace.yaml",
      callback = function()
        local bufnr = vim.api.nvim_get_current_buf()
        -- Small delay to let the buffer fully load
        vim.defer_fn(function()
          show(bufnr)
        end, 100)
      end,
    })
  end
end

-- Public API
M.show = show
M.hide = hide
M.toggle = toggle
M.update = update_current_line

return M
