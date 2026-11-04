package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/kevnnard/dots/pkg/setup"
	"github.com/spf13/cobra"
)

var setupDryRunFlag bool

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Guide the post-install steps in priority order",
	Long: `Runs the guided post-install steps in priority order: the default shell first
(zsh or bash), then zsh with powerlevel10k, then the logins.

Logins run in this terminal, so password prompts and login links show up here;
you complete them. dots never reads or stores credentials. A login is verified
only through its tool's own status command; without one it is reported as manual.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		steps := setup.Plan()
		if setupDryRunFlag {
			fmt.Fprintln(out, "🧭 Setup plan (in order):")
			for i, s := range steps {
				fmt.Fprintf(out, "  %d. %-30s [%s]\n", i+1, s.Title, stepKind(s))
			}
			return nil
		}
		shell, err := askShell(cmd.InOrStdin(), out)
		if err != nil {
			return err
		}
		results := setup.RunAll(setup.OSEnv{}, steps, map[string]string{"shell": shell})
		return printSetupResults(out, results)
	},
}

func init() {
	setupCmd.Flags().BoolVarP(&setupDryRunFlag, "dry-run", "n", false, "Show the plan without changing anything")
	rootCmd.AddCommand(setupCmd)
}

func stepKind(s setup.Step) string {
	if s.Login {
		return "login"
	}
	return "no token"
}

// askShell offers zsh and bash for now; Enter keeps zsh.
func askShell(in io.Reader, out io.Writer) (string, error) {
	fmt.Fprintln(out, "Default shell: 1) zsh (Enter)  2) bash")
	line, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.TrimSpace(line) {
	case "", "1":
		return "zsh", nil
	case "2":
		return "bash", nil
	}
	return "", fmt.Errorf("choose 1 (zsh) or 2 (bash)")
}

func printSetupResults(out io.Writer, results []setup.Result) error {
	counts := map[setup.Status]int{}
	fmt.Fprintln(out, "\n🧭 Setup results:")
	for _, r := range results {
		counts[r.Status]++
		icon := map[setup.Status]string{setup.StatusDone: "✓", setup.StatusFailed: "✗", setup.StatusManual: "◇"}[r.Status]
		fmt.Fprintf(out, "  %s %-30s %s\n", icon, r.Title, r.Detail)
	}
	fmt.Fprintf(out, "\n📊 Done: %d · Manual: %d · Failed: %d\n", counts[setup.StatusDone], counts[setup.StatusManual], counts[setup.StatusFailed])
	if counts[setup.StatusFailed] > 0 {
		return fmt.Errorf("setup finished with %d failed step(s)", counts[setup.StatusFailed])
	}
	return nil
}
