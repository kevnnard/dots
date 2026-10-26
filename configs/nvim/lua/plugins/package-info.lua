return {
  "vuki656/package-info.nvim",
  dependencies = { "MunifTanjim/nui.nvim" },
  event = { "BufRead package.json" },
  opts = {
    colors = {
      up_to_date = "#3C4048",
      outdated = "#d19a66",
    },
    icons = {
      enable = true,
      style = {
        up_to_date = "|  ",
        outdated = "|  ",
      },
    },
    autostart = true,
    hide_up_to_date = false,
    hide_unstable_versions = false,
    package_manager = "npm",
  },
  config = function(_, opts)
    require("package-info").setup(opts)
    local wk_ok, wk = pcall(require, "which-key")
    if wk_ok then
      wk.add({ { "<leader>p", group = "+packages" } })
    end
  end,
  keys = {
    { "<leader>ps", function() require("package-info").show() end,           ft = "json", silent = true, noremap = true, desc = "Show dependency versions" },
    { "<leader>ph", function() require("package-info").hide() end,           ft = "json", silent = true, noremap = true, desc = "Hide dependency versions" },
    { "<leader>pt", function() require("package-info").toggle() end,         ft = "json", silent = true, noremap = true, desc = "Toggle dependency versions" },
    { "<leader>pu", function() require("package-info").update() end,         ft = "json", silent = true, noremap = true, desc = "Update dependency on line" },
    { "<leader>pd", function() require("package-info").delete() end,         ft = "json", silent = true, noremap = true, desc = "Delete dependency on line" },
    { "<leader>pi", function() require("package-info").install() end,        ft = "json", silent = true, noremap = true, desc = "Install new dependency" },
    { "<leader>pv", function() require("package-info").change_version() end, ft = "json", silent = true, noremap = true, desc = "Change dependency version" },
  },
}
