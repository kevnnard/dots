return {
  name = "ai-usage",
  dir = vim.fn.stdpath("config") .. "/lua/ai-usage",
  lazy = false,
  config = function()
    require("ai-usage").setup()
    local wk_ok, wk = pcall(require, "which-key")
    if wk_ok then
      wk.add({
        { "<leader>au", desc = "AI Subs Usage (CodexBar)" },
        { "<leader>as", desc = "AI Subs Usage (CodexBar)" },
      })
    end
  end,
  keys = {
    {
      "<leader>au",
      function()
        require("ai-usage").open()
      end,
      desc = "AI Subscriptions Usage",
    },
    {
      "<leader>as",
      function()
        require("ai-usage").open()
      end,
      desc = "AI Subscriptions Usage",
    },
    {
      "<D-a>",
      function()
        require("ai-usage").open()
      end,
      desc = "AI Subscriptions Usage",
    },
  },
}
