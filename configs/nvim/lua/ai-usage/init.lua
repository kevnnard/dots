local M = {}

M.config = {
  cli = "/Applications/CodexBar.app/Contents/Helpers/CodexBarCLI",
  cache_file = vim.fn.stdpath("cache") .. "/codexbar_usage.json",
  cache_ttl = 300, -- 5 minutos
}

local ICONS = {
  claude = "󰛄",
  codex = "󰚩",
  antigravity = "󱚟",
  minimax = "󰭹",
  copilot = "󰊤",
  gemini = "󱚝",
  openai = "󰚩",
}

local ns_id = vim.api.nvim_create_namespace("ai_usage_modal")
local current_win = nil
local is_fetching = false

local function make_bar(percent, width)
  width = width or 12
  local pct = math.max(0, math.min(100, math.floor((percent or 0) + 0.5)))
  local filled = math.floor((pct / 100) * width + 0.5)
  local empty = width - filled
  return string.rep("█", filled) .. string.rep("░", empty)
end

local function parse_iso_to_epoch(iso_str)
  if not iso_str then return nil end
  local year, month, day, hour, min, sec = iso_str:match("(%d+)-(%d+)-(%d+)T(%d+):(%d+):(%d+)")
  if not year then return nil end
  local target_utc = os.time({
    year = tonumber(year),
    month = tonumber(month),
    day = tonumber(day),
    hour = tonumber(hour),
    min = tonumber(min),
    sec = tonumber(sec),
    isdst = false,
  })
  local now = os.time()
  local now_utc_table = os.date("!*t", now)
  local now_utc_epoch = os.time(now_utc_table)
  local tz_offset = os.difftime(now, now_utc_epoch)
  return target_utc + tz_offset
end

local function clean_reset_desc(desc)
  if not desc or desc == "" then
    return ""
  end
  local clean = desc:match("refresh (in .-)%.?$")
  if clean then
    return clean
  end
  clean = desc:match("^Resets? in (.*)$")
  if clean then
    return "en " .. clean
  end
  return desc
end

local function format_reset_time(iso_str, fallback_desc)
  local epoch = parse_iso_to_epoch(iso_str)
  if not epoch then
    return clean_reset_desc(fallback_desc)
  end
  local now = os.time()
  local diff = epoch - now
  if diff <= 0 then
    return "Reiniciando..."
  end
  local mins = math.floor(diff / 60)
  local hours = math.floor(mins / 60)
  local rem_mins = mins % 60
  local days = math.floor(hours / 24)
  local rem_hours = hours % 24

  local rem_str = ""
  if days > 0 then
    rem_str = string.format("en %dd %dh", days, rem_hours)
  elseif hours > 0 then
    rem_str = string.format("en %dh %dm", hours, rem_mins)
  else
    rem_str = string.format("en %dm", rem_mins)
  end

  local now_date = os.date("%Y-%m-%d", now)
  local target_date = os.date("%Y-%m-%d", epoch)
  local time_str = os.date("%H:%M", epoch)

  if now_date == target_date then
    return string.format("%s (%s)", time_str, rem_str)
  else
    local date_str = os.date("%b %d", epoch)
    return string.format("%s %s (%s)", date_str, time_str, rem_str)
  end
end

local function load_cached_data()
  local f = io.open(M.config.cache_file, "r")
  if not f then
    return nil, 0
  end
  local content = f:read("*a")
  f:close()
  if not content or content == "" then
    return nil, 0
  end
  local ok, data = pcall(vim.json.decode, content)
  if not ok or type(data) ~= "table" then
    return nil, 0
  end
  local mtime = vim.fn.getftime(M.config.cache_file)
  return data, mtime
end

local function save_cache(content)
  local f = io.open(M.config.cache_file, "w")
  if f then
    f:write(content)
    f:close()
  end
end

local function build_display_lines(data, mtime)
  local lines = {}
  local hls = {} -- { line = n, col_start = x, col_end = y, group = "..." }

  local function add_line(text, group, col_start, col_end)
    table.insert(lines, text)
    local line_idx = #lines - 1
    if group then
      table.insert(hls, {
        line = line_idx,
        col_start = col_start or 0,
        col_end = col_end or -1,
        group = group,
      })
    end
    return line_idx
  end

  add_line(" 󰚩  CUOTAS Y CONSUMO DE SUBSCRIPCIONES IA", "Title")
  
  if mtime and mtime > 0 then
    local diff = os.time() - mtime
    local ago = ""
    if diff < 60 then
      ago = "hace unos segundos"
    elseif diff < 3600 then
      ago = string.format("hace %d min", math.floor(diff / 60))
    else
      ago = string.format("hace %d h", math.floor(diff / 3600))
    end
    add_line("    Última actualización: " .. ago .. " • [r] Recargar", "Comment")
  else
    add_line("    [r] Recargar • [q / Esc] Cerrar", "Comment")
  end

  add_line("")

  if not data or #data == 0 then
    add_line("  No se encontraron datos de proveedores en CodexBar.", "Comment")
    return lines, hls
  end

  for _, item in ipairs(data) do
    local provider_id = (item.provider or "unknown"):lower()
    local icon = ICONS[provider_id] or "󰚩"
    local name = provider_id:sub(1, 1):upper() .. provider_id:sub(2)
    local u = item.usage or {}
    local login = u.loginMethod or (u.identity and u.identity.loginMethod) or ""
    local email = u.accountEmail or (u.identity and u.identity.accountEmail) or ""

    local header_text = string.format("  %s %s", icon, name)
    if login ~= "" then
      header_text = header_text .. string.format(" (%s)", login)
    end
    if email ~= "" and not header_text:find(email, 1, true) then
      header_text = header_text .. string.format(" - %s", email)
    end

    add_line(header_text, "Special")

    -- Antigravity special handling (extra rate windows)
    if provider_id == "antigravity" and u.extraRateWindows and #u.extraRateWindows > 0 then
      local seen_titles = {}
      for _, rw in ipairs(u.extraRateWindows) do
        local title = rw.title or ""
        local w = rw.window or {}
        local pct = w.usedPercent
        if pct and not seen_titles[title .. "_" .. (w.windowMinutes or 0)] then
          seen_titles[title .. "_" .. (w.windowMinutes or 0)] = true
          local left = math.max(0, math.floor(100 - pct + 0.5))
          local bar = make_bar(pct, 12)
          local reset = format_reset_time(w.resetsAt, w.resetDescription)
          local reset_str = reset ~= "" and ("  ↳ " .. reset) or ""
          
          local stat_line = string.format("    %-18s [%s] %2d%% (%2d%% libre)%s", title, bar, math.floor(pct + 0.5), left, reset_str)
          local hl_group = "DiagnosticOk"
          if pct >= 85 then
            hl_group = "DiagnosticError"
          elseif pct >= 60 then
            hl_group = "DiagnosticWarn"
          end

          local l_idx = add_line(stat_line)
          -- Highlight the bar and percentage
          local bar_start = stat_line:find("%[")
          local bar_end = stat_line:find("%]")
          if bar_start and bar_end then
            table.insert(hls, { line = l_idx, col_start = bar_start - 1, col_end = bar_end, group = hl_group })
          end
        end
      end
    else
      -- Standard Primary (5-hour window)
      if u.primary and u.primary.usedPercent then
        local pct = u.primary.usedPercent
        local left = math.max(0, math.floor(100 - pct + 0.5))
        local bar = make_bar(pct, 12)
        local reset = format_reset_time(u.primary.resetsAt, u.primary.resetDescription)
        local reset_str = reset ~= "" and ("  ↳ " .. reset) or ""

        local stat_line = string.format("    %-18s [%s] %2d%% (%2d%% libre)%s", "5h Session", bar, math.floor(pct + 0.5), left, reset_str)
        local hl_group = "DiagnosticOk"
        if pct >= 85 then
          hl_group = "DiagnosticError"
        elseif pct >= 60 then
          hl_group = "DiagnosticWarn"
        end

        local l_idx = add_line(stat_line)
        local bar_start = stat_line:find("%[")
        local bar_end = stat_line:find("%]")
        if bar_start and bar_end then
          table.insert(hls, { line = l_idx, col_start = bar_start - 1, col_end = bar_end, group = hl_group })
        end
      end

      -- Standard Secondary (Weekly window)
      if u.secondary and u.secondary.usedPercent then
        local pct = u.secondary.usedPercent
        local left = math.max(0, math.floor(100 - pct + 0.5))
        local bar = make_bar(pct, 12)
        local reset = format_reset_time(u.secondary.resetsAt, u.secondary.resetDescription)
        local reset_str = reset ~= "" and ("  ↳ " .. reset) or ""

        local stat_line = string.format("    %-18s [%s] %2d%% (%2d%% libre)%s", "Límite Semanal", bar, math.floor(pct + 0.5), left, reset_str)
        local hl_group = "DiagnosticOk"
        if pct >= 85 then
          hl_group = "DiagnosticError"
        elseif pct >= 60 then
          hl_group = "DiagnosticWarn"
        end

        local l_idx = add_line(stat_line)
        local bar_start = stat_line:find("%[")
        local bar_end = stat_line:find("%]")
        if bar_start and bar_end then
          table.insert(hls, { line = l_idx, col_start = bar_start - 1, col_end = bar_end, group = hl_group })
        end
      end

      -- Reset credits if available (e.g. Codex / Claude reset credits)
      if u.codexResetCredits and u.codexResetCredits.availableCount then
        add_line(string.format("    Reset Credits      : %d disponible(s)", u.codexResetCredits.availableCount), "Comment")
      end
    end

    add_line("")
  end

  add_line("───────────────────────────────────────────────────────────────────", "Comment")
  add_line("  Atajos: [r] Recargar métricas  •  [q / Esc] Cerrar ventana", "Comment")

  return lines, hls
end

local function apply_highlights(buf, hls)
  vim.api.nvim_buf_clear_namespace(buf, ns_id, 0, -1)
  for _, h in ipairs(hls) do
    pcall(vim.api.nvim_buf_add_highlight, buf, ns_id, h.group, h.line, h.col_start, h.col_end)
  end
end

local function update_window_content(win, lines, hls)
  if not win or not win.buf or not vim.api.nvim_buf_is_valid(win.buf) then
    return
  end
  vim.api.nvim_set_option_value("modifiable", true, { buf = win.buf })
  vim.api.nvim_buf_set_lines(win.buf, 0, -1, false, lines)
  vim.api.nvim_set_option_value("modifiable", false, { buf = win.buf })
  apply_highlights(win.buf, hls)
end

function M.refresh(win)
  if is_fetching then
    vim.notify("CodexBar ya se está consultando en segundo plano...", vim.log.levels.INFO)
    return
  end

  if not vim.fn.executable(M.config.cli) == 1 then
    vim.notify("CodexBarCLI no encontrado en: " .. M.config.cli, vim.log.levels.ERROR)
    return
  end

  is_fetching = true

  if win and win.buf and vim.api.nvim_buf_is_valid(win.buf) then
    local loading_lines = {
      " 󰚩  CUOTAS Y CONSUMO DE SUBSCRIPCIONES IA",
      "    󰑐 Consultando métricas a CodexBar en segundo plano...",
      "",
      "    Por favor esperá unos segundos...",
      "",
      "───────────────────────────────────────────────────────────────────",
      "  Atajos: [q / Esc] Cerrar ventana",
    }
    vim.api.nvim_set_option_value("modifiable", true, { buf = win.buf })
    vim.api.nvim_buf_set_lines(win.buf, 0, -1, false, loading_lines)
    vim.api.nvim_set_option_value("modifiable", false, { buf = win.buf })
  end

  vim.system({ M.config.cli, "usage", "--format", "json" }, { text = true }, function(obj)
    is_fetching = false
    vim.schedule(function()
      if obj.code ~= 0 or not obj.stdout or obj.stdout == "" then
        local err_msg = obj.stderr or "Error desconocido al ejecutar CodexBarCLI"
        vim.notify("Error al obtener cuotas de CodexBar: " .. err_msg, vim.log.levels.WARN)
        return
      end

      save_cache(obj.stdout)
      local ok, data = pcall(vim.json.decode, obj.stdout)
      if not ok or type(data) ~= "table" then
        vim.notify("Error al decodificar JSON de CodexBar", vim.log.levels.ERROR)
        return
      end

      if win and win.buf and vim.api.nvim_buf_is_valid(win.buf) then
        local lines, hls = build_display_lines(data, os.time())
        update_window_content(win, lines, hls)
      end
    end)
  end)
end

function M.open()
  if current_win and current_win.win and vim.api.nvim_win_is_valid(current_win.win) then
    current_win:close()
    current_win = nil
    return
  end

  local cached_data, mtime = load_cached_data()
  local lines, hls = {}, {}

  if cached_data then
    lines, hls = build_display_lines(cached_data, mtime)
  else
    lines = {
      " 󰚩  CUOTAS Y CONSUMO DE SUBSCRIPCIONES IA",
      "    󰑐 Consultando métricas a CodexBar por primera vez...",
      "",
      "    Por favor esperá unos segundos...",
      "",
      "───────────────────────────────────────────────────────────────────",
      "  Atajos: [q / Esc] Cerrar ventana",
    }
  end

  local snacks_ok, Snacks = pcall(require, "snacks")
  if snacks_ok and Snacks.win then
    current_win = Snacks.win({
      title = " 󰚩 AI Subscriptions (CodexBar) ",
      title_pos = "center",
      border = "rounded",
      width = 82,
      height = math.min(26, math.max(12, #lines + 2)),
      bo = {
        buftype = "nofile",
        filetype = "ai-usage",
      },
      keys = {
        q = "close",
        ["<Esc>"] = "close",
        r = function(self)
          M.refresh(self)
        end,
      },
      text = lines,
    })

    apply_highlights(current_win.buf, hls)

    -- Stale cache check (> 5 min) or first time
    if not cached_data or (os.time() - mtime > M.config.cache_ttl) then
      M.refresh(current_win)
    end
  else
    -- Fallback simple floating window if Snacks is unavailable
    local buf = vim.api.nvim_create_buf(false, true)
    vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines)
    local width = 76
    local height = math.min(26, math.max(12, #lines + 2))
    local win = vim.api.nvim_open_win(buf, true, {
      relative = "editor",
      width = width,
      height = height,
      col = math.floor((vim.o.columns - width) / 2),
      row = math.floor((vim.o.lines - height) / 2),
      border = "rounded",
      title = " AI Subscriptions ",
      title_pos = "center",
    })
    vim.keymap.set("n", "q", function() vim.api.nvim_win_close(win, true) end, { buffer = buf })
    vim.keymap.set("n", "<Esc>", function() vim.api.nvim_win_close(win, true) end, { buffer = buf })
    apply_highlights(buf, hls)
  end
end

function M.setup(opts)
  M.config = vim.tbl_deep_extend("force", M.config, opts or {})

  vim.api.nvim_create_user_command("AIUsage", function()
    M.open()
  end, { desc = "Abrir modal de cuotas y consumo de IA (CodexBar)" })

  vim.api.nvim_create_user_command("AISubs", function()
    M.open()
  end, { desc = "Abrir modal de cuotas y consumo de IA (CodexBar)" })
end

return M
