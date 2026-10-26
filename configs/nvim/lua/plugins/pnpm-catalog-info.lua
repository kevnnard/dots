return {
  dir = vim.fn.stdpath("config") .. "/lua/pnpm-catalog-info",
  name = "pnpm-catalog-info",
  ft = "yaml",
  opts = {
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
  },
  config = function(_, opts)
    require("pnpm-catalog-info").setup(opts)
    local wk_ok, wk = pcall(require, "which-key")
    if wk_ok then
      wk.add({ { "<leader>p", group = "+packages" } })
    end
  end,
  keys = {
    {
      "<leader>ps",
      function() require("pnpm-catalog-info").show() end,
      ft = "yaml",
      silent = true,
      noremap = true,
      desc = "Show catalog versions",
    },
    {
      "<leader>ph",
      function() require("pnpm-catalog-info").hide() end,
      ft = "yaml",
      silent = true,
      noremap = true,
      desc = "Hide catalog versions",
    },
    {
      "<leader>pt",
      function() require("pnpm-catalog-info").toggle() end,
      ft = "yaml",
      silent = true,
      noremap = true,
      desc = "Toggle catalog versions",
    },
    {
      "<leader>pv",
      function() require("pnpm-catalog-info").update() end,
      ft = "yaml",
      silent = true,
      noremap = true,
      desc = "Change version on cursor line",
    },
  },
}
