package clean

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"

	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/manifest"
)

// Outcome is what the runner did with one planned package item.
type Outcome string

const (
	OutcomeRemoved Outcome = "removed"
	OutcomeFailed  Outcome = "failed"
	OutcomeNotRun  Outcome = "not-run" // an earlier failure stopped the run
)

// ItemResult reports one executed or stopped removal.
type ItemResult struct {
	AppName string  `json:"app_name"`
	Outcome Outcome `json:"outcome"`
	Detail  string  `json:"detail,omitempty"`
}

// Report is the result of a package run. DryRun and Cancelled runs change
// nothing and have no Items.
type Report struct {
	Items     []ItemResult `json:"items"`
	DryRun    bool         `json:"dry_run"`
	Cancelled bool         `json:"cancelled"`
}

// PackageRunner executes the remove items of a plan. It is shared by the CLI
// and TUI, which supply Confirm and Out.
type PackageRunner struct {
	Manifest *manifest.Manifest
	Sys      *detector.SystemInfo
	Exec     Executor
	// Recheck re-queries ownership right before each removal; the owner must
	// still be exactly the planned one.
	Recheck OwnershipProbe
	DryRun  bool
	// Confirm receives the printed summary. A nil Confirm denies.
	Confirm func(summary string) bool
	Out     io.Writer
}

// Run validates the plan, prints its summary and, only when confirmed,
// removes packages in plan order. The first failure stops every later
// removal, so a runtime is never removed after a dependent failed. Command
// output is never printed.
func (r PackageRunner) Run(ctx context.Context, p *Plan) (*Report, error) {
	if r.Manifest == nil || r.Sys == nil || r.Exec == nil || r.Recheck == nil || r.Out == nil || p == nil {
		return nil, fmt.Errorf("package runner is not fully configured")
	}
	var remove []PackageItem
	for _, it := range p.Packages {
		if it.Status != PackageRemove {
			continue
		}
		if err := r.verify(it); err != nil {
			return nil, fmt.Errorf("refusing plan item %q: %w", it.AppName, err)
		}
		remove = append(remove, it)
	}
	summary := Summary(p)
	fmt.Fprint(r.Out, summary)
	rep := &Report{Items: []ItemResult{}}
	switch {
	case r.DryRun:
		rep.DryRun = true
		fmt.Fprintln(r.Out, "Dry run: nothing was changed.")
		return rep, nil
	case len(remove) == 0:
		fmt.Fprintln(r.Out, "Nothing to remove.")
		return rep, nil
	case r.Confirm == nil || !r.Confirm(summary):
		rep.Cancelled = true
		fmt.Fprintln(r.Out, "Cancelled: nothing was changed.")
		return rep, nil
	}
	var failed string
	for _, it := range remove {
		res := ItemResult{AppName: it.AppName, Outcome: OutcomeRemoved}
		switch {
		case failed != "":
			res.Outcome, res.Detail = OutcomeNotRun, "stopped after an earlier failure"
		default:
			if detail := r.remove(ctx, it); detail != "" {
				res.Outcome, res.Detail, failed = OutcomeFailed, detail, it.AppName
			}
		}
		rep.Items = append(rep.Items, res)
		line := string(res.Outcome) + " " + res.AppName
		if res.Detail != "" {
			line += ": " + res.Detail
		}
		fmt.Fprintln(r.Out, line)
	}
	if failed != "" {
		return rep, fmt.Errorf("package cleanup stopped: %s failed", failed)
	}
	return rep, nil
}

// verify re-derives the argv from the owner so a stale or edited plan can
// never run a command the planner would not have produced.
func (r PackageRunner) verify(it PackageItem) error {
	app, ok := r.Manifest.Apps[it.AppName]
	switch {
	case !ok:
		return fmt.Errorf("not a declared app")
	case it.Owner == nil || !declared(app, *it.Owner):
		return fmt.Errorf("owner does not match a declared recipe")
	}
	argv, _, reason := uninstallArgv(*it.Owner, r.Sys)
	if argv == nil {
		return fmt.Errorf("%s", reason)
	}
	if !reflect.DeepEqual(argv, it.Argv) {
		return fmt.Errorf("command does not match the tested uninstall")
	}
	return nil
}

func (r PackageRunner) remove(ctx context.Context, it PackageItem) string {
	ev, err := r.Recheck(it.AppName, r.Manifest.Apps[it.AppName])
	if err != nil {
		return "ownership could not be rechecked"
	}
	if len(ev.Owners) != 1 || ev.Owners[0] != *it.Owner {
		return "ownership changed since the plan"
	}
	res, err := r.Exec.Run(ctx, it.Argv)
	switch {
	case err != nil:
		return "could not run " + it.Argv[0]
	case res.ExitCode != 0:
		return "exit status " + strconv.Itoa(res.ExitCode)
	}
	return ""
}

// Summary renders every package item, including blocked reasons and
// warnings, as the text shown before confirmation.
func Summary(p *Plan) string {
	var b strings.Builder
	b.WriteString("Package cleanup plan:\n")
	counts := map[PackageStatus]int{}
	for _, it := range p.Packages {
		counts[it.Status]++
		detail := it.Reason
		if it.Status == PackageRemove {
			detail = quoteArgv(it.Argv)
		}
		b.WriteString("  " + string(it.Status) + " " + it.AppName)
		if detail != "" {
			b.WriteString(": " + detail)
		}
		b.WriteString("\n")
		for _, w := range it.Warnings {
			b.WriteString("  warning " + it.AppName + ": " + w + "\n")
		}
	}
	fmt.Fprintf(&b, "%d to remove, %d blocked\n", counts[PackageRemove], counts[PackageBlocked])
	return b.String()
}

func quoteArgv(argv []string) string {
	out := make([]string, len(argv))
	for i, a := range argv {
		out[i] = a
		if a == "" || strings.ContainsAny(a, " \t\n'\"\\$`;&|<>()*?[]{}~#!") {
			out[i] = strconv.Quote(a)
		}
	}
	return strings.Join(out, " ")
}
