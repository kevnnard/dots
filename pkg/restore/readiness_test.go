package restore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/kevnnard/dots/pkg/manifest"
)

// readinessHome is a temporary target home plus fake environment and PATH.
type readinessHome struct {
	t    *testing.T
	home string
	env  map[string]string
	path map[string]bool
}

func newReadinessHome(t *testing.T) *readinessHome {
	t.Helper()
	return &readinessHome{t: t, home: t.TempDir(), env: map[string]string{}, path: map[string]bool{}}
}

func (h *readinessHome) write(rel, content string) string {
	h.t.Helper()
	p := filepath.Join(h.home, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		h.t.Fatal(err)
	}
	return p
}

func (h *readinessHome) opts() ReadinessOptions {
	return ReadinessOptions{
		Home: h.home,
		LookupEnv: func(name string) (string, bool) {
			v, ok := h.env[name]
			return v, ok
		},
		LookPath: func(cmd string) (string, error) {
			if h.path[cmd] {
				return "/fake/bin/" + cmd, nil
			}
			return "", errors.New("not found")
		},
	}
}

// scan runs the public scanner over the given apps and renders the report.
func (h *readinessHome) scan(m *manifest.Manifest, apps ...string) (*ReadinessReport, string) {
	var steps []StepResult
	for _, a := range apps {
		steps = append(steps, StepResult{Step: Step{AppName: a}, Install: OutcomeAlreadyInstalled})
	}
	r := ScanReadiness(m, steps, h.opts())
	return r, strings.Join(r.Lines(false), "\n")
}

func piManifest() *manifest.Manifest {
	return &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"pi": {Paths: []manifest.PathMapping{
			{Src: "~/.pi/agent/mcp.json", Dest: "configs/pi/mcp.json"},
			{Src: "~/.pi/agent/auth.json", Dest: "configs/pi/auth.json"},
		}},
		"codex": {Paths: []manifest.PathMapping{
			{Src: "~/.codex/config.toml", Dest: "configs/codex/config.toml"},
			{Src: "~/.codex/auth.json", Dest: "configs/codex/auth.json"},
		}},
		"claude": {Paths: []manifest.PathMapping{{Src: "~/.claude/settings.json", Dest: "configs/claude/settings.json"}}},
	}}
}

func findItem(r *ReadinessReport, app, name string) (ReadinessItem, bool) {
	for _, a := range r.Apps {
		if a.App != app {
			continue
		}
		for _, it := range a.Items {
			if it.Name == name {
				return it, true
			}
		}
	}
	return ReadinessItem{}, false
}

func findDependency(r *ReadinessReport, app, field string) (ReadinessItem, bool) {
	for _, a := range r.Apps {
		for _, it := range a.Items {
			if a.App == app && it.Kind == "dependency" && it.Field == field {
				return it, true
			}
		}
	}
	return ReadinessItem{}, false
}

func TestReadinessNamesMissingFieldsEnvAndCommandsWithoutValues(t *testing.T) {
	h := newReadinessHome(t)
	h.env["SET_TOKEN"] = "env-secret-value-123"
	h.path["engram"] = true
	h.write(".pi/agent/mcp.json", `{"mcpServers": {
		"linear":  {"url": "https://mcp.linear.app/mcp", "headers": {"Authorization": "YOUR_AUTHORIZATION_HERE"}},
		"github":  {"command": "engram", "env": {"GITHUB_TOKEN": "${GITHUB_PERSONAL_ACCESS_TOKEN}", "OTHER": "${SET_TOKEN}", "DEF": "${WITH_DEFAULT:-x}"}},
		"stitch":  {"headers": {"X-Goog-Api-Key": "AIzaRealLookingSecretValue987"}},
		"codegraph": {"command": "codegraph", "args": ["serve"]}
	}}`)
	h.write(".codex/config.toml", "[model_providers.azure]\nenv_key = \"AZURE_OPENAI_API_KEY\"\n\n[mcp_servers.docs]\ncommand = \"docs-mcp\"\nbearer_token_env_var = \"SET_TOKEN\"\n")

	r, out := h.scan(piManifest(), "pi", "codex")

	if it, ok := findItem(r, "pi", "mcpServers.linear.headers.Authorization"); !ok || it.State != ReadinessMissing || it.File != "~/.pi/agent/mcp.json" {
		t.Errorf("placeholder field not reported")
	}
	if it, ok := findItem(r, "pi", "GITHUB_PERSONAL_ACCESS_TOKEN"); !ok || it.Kind != "env" || it.Field != "mcpServers.github.env.GITHUB_TOKEN" || !strings.Contains(it.Action, "export GITHUB_PERSONAL_ACCESS_TOKEN") {
		t.Errorf("unset env reference not reported")
	}
	if it, ok := findDependency(r, "pi", "mcpServers.codegraph.command"); !ok || it.State != ReadinessDependencyMissing || it.File != "~/.pi/agent/mcp.json" {
		t.Errorf("missing MCP command not reported")
	}
	if it, ok := findItem(r, "codex", "AZURE_OPENAI_API_KEY"); !ok || it.File != "~/.codex/config.toml" || it.Field != "model_providers.azure.env_key" {
		t.Errorf("TOML env_key not reported")
	}
	if _, ok := findDependency(r, "codex", "mcp_servers.<key #1>.command"); !ok {
		t.Errorf("TOML MCP command not reported")
	}
	if _, ok := findDependency(r, "pi", "mcpServers.github.command"); ok {
		t.Errorf("a command found on PATH must not be reported")
	}
	for _, name := range []string{"SET_TOKEN", "WITH_DEFAULT", "mcpServers.stitch.headers.X-Goog-Api-Key"} {
		if _, ok := findItem(r, "pi", name); ok {
			t.Errorf("%s is satisfied and must not be reported", name)
		}
	}
	for _, secret := range []string{"env-secret-value-123", "AIzaRealLookingSecretValue987", "YOUR_AUTHORIZATION_HERE", "https://", "codegraph serve", "docs-mcp"} {
		if strings.Contains(out, secret) {
			t.Errorf("report leaks a fixture value")
		}
	}
}

func TestReadinessLoginPendingOnlyFromTemplateEvidence(t *testing.T) {
	h := newReadinessHome(t)
	// codex: the dots template is still in place, so no login was recorded.
	h.write(".codex/auth.json", `{"OPENAI_API_KEY": null, "tokens": {"access_token": "YOUR_ACCESS_TOKEN_HERE"}}`)
	// pi: one provider still templated, one with a real-looking session.
	h.write(".pi/agent/auth.json", `{"anthropic": {"access": "YOUR_ACCESS_HERE"}, "openai|https://x": {"access": "real-session-token-456"}}`)

	r, out := h.scan(piManifest(), "codex", "pi", "claude")

	if it, ok := findItem(r, "codex", "Codex session"); !ok || it.State != ReadinessLoginPending || it.Action != "run `codex login`" {
		t.Errorf("templated codex auth must be login-pending")
	}
	if it, ok := findItem(r, "pi", "anthropic"); !ok || it.State != ReadinessLoginPending {
		t.Errorf("templated pi provider must be login-pending")
	}
	if _, ok := findItem(r, "pi", "openai"); ok {
		t.Errorf("a provider with recorded credentials must not be login-pending")
	}
	if it, ok := findItem(r, "claude", "Claude Code session"); !ok || it.State != ReadinessNotVerified || !strings.Contains(it.Action, "/login") {
		t.Errorf("claude session lives outside managed files and must be not-verified")
	}
	if strings.Contains(out, "real-session-token-456") || strings.Contains(out, "https://x") {
		t.Errorf("report leaks credential data")
	}
}

func TestReadinessAbsentCredentialsAreNotVerifiedNotInvalid(t *testing.T) {
	h := newReadinessHome(t)
	h.write(".codex/auth.json", `{"auth_mode": "chatgpt", "tokens": {"access_token": "eyJrealtoken"}}`)

	r, out := h.scan(piManifest(), "pi", "codex")

	it, ok := findItem(r, "pi", "Pi provider login")
	if !ok || it.State != ReadinessNotVerified || !strings.Contains(it.Action, "/login") {
		t.Errorf("absent pi auth file must be not-verified with the login action")
	}
	if it, ok := findItem(r, "codex", "Codex session"); !ok || it.State != ReadinessNotVerified || !strings.Contains(it.Detail, "validity not checked") {
		t.Errorf("present credentials must be reported as unvalidated, never as ready or pending")
	}
	for _, word := range []string{"login-pending", "invalid", "expired", "eyJrealtoken"} {
		if strings.Contains(out, word) {
			t.Errorf("report must not claim a state or value without evidence")
		}
	}
}

func TestReadinessMalformedConfigReportsSafeError(t *testing.T) {
	h := newReadinessHome(t)
	h.write(".pi/agent/mcp.json", `{"mcpServers": {"pg": {"env": {"URL": "postgres://admin:hunter2@db"}},,}}`)

	r, out := h.scan(piManifest(), "pi")

	it, ok := findItem(r, "pi", "~/.pi/agent/mcp.json")
	if !ok || it.State != ReadinessNotVerified || it.Detail != "not valid JSON; not scanned" {
		t.Errorf("malformed config must be reported as not scanned")
	}
	for _, leak := range []string{"hunter2", "postgres://", "invalid character", "offset"} {
		if strings.Contains(out, leak) {
			t.Errorf("report leaks a fixture value")
		}
	}
}

func TestReadinessScansOnlyDeclaredFilesInsideTargetHome(t *testing.T) {
	h := newReadinessHome(t)
	outside := filepath.Join(t.TempDir(), "elsewhere.json")
	if err := os.WriteFile(outside, []byte(`{"key": "YOUR_OUTSIDE_KEY_HERE"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(h.home, ".pi", "agent"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(h.home, ".pi", "agent", "mcp.json")); err != nil {
		t.Fatal(err)
	}
	h.write(".pi/agent/undeclared.json", `{"key": "YOUR_UNDECLARED_KEY_HERE"}`)
	h.write(".claude/settings.json", `{"a": "`+strings.Repeat("x", 64)+`", "b": "YOUR_BIG_KEY_HERE"}`)

	o := h.opts()
	o.MaxFileSize = 32
	r := ScanReadiness(piManifest(), []StepResult{
		{Step: Step{AppName: "pi"}, Install: OutcomeAlreadyInstalled},
		{Step: Step{AppName: "claude"}, Install: OutcomeAlreadyInstalled},
		{Step: Step{AppName: "codex"}, Install: OutcomeUnsupported},
	}, o)
	out := strings.Join(r.Lines(false), "\n")

	if it, ok := findItem(r, "pi", "~/.pi/agent/mcp.json"); !ok || !strings.Contains(it.Detail, "outside the target home") {
		t.Errorf("symlink out of the target home must not be followed")
	}
	if it, ok := findItem(r, "claude", "~/.claude/settings.json"); !ok || !strings.Contains(it.Detail, "larger than 32 bytes") {
		t.Errorf("oversized file must not be scanned")
	}
	for _, name := range []string{"key", "b", "YOUR_"} {
		if strings.Contains(out, "field "+name) || strings.Contains(out, "YOUR_") {
			t.Errorf("scanned an undeclared, outside or oversized file (%s)", name)
		}
	}
	for _, a := range r.Apps {
		if a.App == "codex" {
			t.Errorf("unsupported app must not be reported")
		}
	}
}

func TestRunAddsReadinessToSharedSummaryAndDryRunWritesNothing(t *testing.T) {
	h := newReadinessHome(t)
	t.Setenv("HOME", h.home)
	h.write(".pi/agent/mcp.json", `{"mcpServers": {"stitch": {"headers": {"X-Goog-Api-Key": "YOUR_X_GOOG_API_KEY_HERE"}}}}`)
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "configs", "pi"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "configs", "pi", "auth.json.template"), []byte(`{"anthropic": {"access": "YOUR_ACCESS_HERE"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	m := piManifest()
	pi := m.Apps["pi"]
	pi.Install = pacmanRecipe("pi")
	m.Apps["pi"] = pi
	before := listTree(t, h.home)

	fx := &fakeExec{}
	res, err := Run(m, []string{"pi"}, plainArch(), RunOptions{
		RepoDir: repo, WithConfig: true, InstallPackages: true, DryRun: true,
		Probe: noneInstalled, Exec: fx.run, Readiness: h.opts(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fx.calls) != 0 {
		t.Errorf("dry run executed %s", fx.apps())
	}
	if after := listTree(t, h.home); after != before {
		t.Errorf("dry run wrote to the target home:\nbefore %s\nafter  %s", before, after)
	}
	if _, ok := findItem(res.Readiness, "pi", "mcpServers.stitch.headers.X-Goog-Api-Key"); !ok {
		t.Fatalf("run result has no readiness item")
	}
	summary := strings.Join(res.Summary(), "\n")
	if !strings.Contains(summary, "🔑 Readiness (dry run") || !strings.Contains(summary, "field mcpServers.stitch.headers.X-Goog-Api-Key in ~/.pi/agent/mcp.json") {
		t.Errorf("shared summary must render the readiness report")
	}
	if strings.Contains(summary, "YOUR_X_GOOG_API_KEY_HERE") {
		t.Errorf("summary leaks the placeholder value")
	}
}

func listTree(t *testing.T, root string) string {
	t.Helper()
	var paths []string
	filepath.Walk(root, func(p string, _ os.FileInfo, err error) error {
		if err == nil {
			rel, _ := filepath.Rel(root, p)
			paths = append(paths, rel)
		}
		return nil
	})
	return strings.Join(paths, ",")
}

// Synthetic canaries planted in key names, provider keys, command paths, the
// declared file path and the app name. They must never reach the structured
// result or the rendered report. Failures name the field only, never the
// report, so a canary is never printed.
const readinessCanary = "SYNTHETIC_CANARY"

var readinessForged = []string{readinessCanary, "FORGED", "SYNTHPREFIXSECRET", "\x1b", "\x07", "\u202e"}

// canaryMCP is the verifier fixture plus a long key whose first 64 bytes
// would survive truncation and an env map key with a control sequence.
const canaryMCP = `{"[SYNTHETIC_CANARY\nFORGED]":"YOUR_TEST_HERE",` +
	`"SYNTHPREFIXSECRET` + `abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz0123456789SYNTHETIC_CANARY":"YOUR_LONG_HERE",` +
	`"mcpServers":{"safe":{"command":"/fake/SYNTHETIC_CANARY"},` +
	`"stitch":{"headers":{"X-Goog-Api-Key":"YOUR_X_GOOG_API_KEY_HERE"}},` +
	`"ghp_SYNTHETIC_CANARY_server":{"env":{"SYNTHETIC_CANARY\u001b[2J\u202e":"YOUR_ENV_HERE","GITHUB_TOKEN":"${GITHUB_PERSONAL_ACCESS_TOKEN}"}}}}`

const canaryAuth = `{"SYNTHETIC_CANARY_PROVIDER":{"access":"YOUR_ACCESS_HERE"},` +
	`"[SYNTHETIC_CANARY\nFORGED]|https://x":{"access":"YOUR_ACCESS_HERE"},` +
	`"anthropic":{"access":"YOUR_ACCESS_HERE"}}`

const canaryMCPAuth = `{"mcp__linear|https://mcp.linear.app/mcp":{"tokens":{"access_token":"YOUR_ACCESS_TOKEN_HERE"}},` +
	`"mcp__SYNTHETIC_CANARY|https://x":{"tokens":{"access_token":"YOUR_ACCESS_TOKEN_HERE"}}}`

// canaryLeaks returns the labels of the report parts that carry a canary or
// a control character, without their content.
func canaryLeaks(r *ReadinessReport, lines []string) []string {
	var leaks []string
	bad := func(s string) bool {
		for _, f := range readinessForged {
			if strings.Contains(s, f) {
				return true
			}
		}
		for _, c := range s {
			if c < 0x20 || c == 0x7f {
				return true
			}
		}
		return false
	}
	for ai, a := range r.Apps {
		if bad(a.App) {
			leaks = append(leaks, "app")
		}
		for ii, it := range a.Items {
			for label, v := range map[string]string{"state": string(it.State), "kind": it.Kind, "name": it.Name, "file": it.File, "field": it.Field, "detail": it.Detail, "action": it.Action} {
				if bad(v) {
					leaks = append(leaks, fmt.Sprintf("apps[%d].items[%d].%s", ai, ii, label))
				}
			}
		}
	}
	for i, l := range lines {
		if bad(l) {
			leaks = append(leaks, fmt.Sprintf("line %d", i))
		}
	}
	return leaks
}

func TestReadinessNeverEchoesDynamicKeysProvidersOrCommands(t *testing.T) {
	h := newReadinessHome(t)
	h.write(".pi/agent/mcp.json", canaryMCP)
	h.write(".pi/agent/auth.json", canaryAuth)
	h.write(".pi/agent/mcp-auth.json", canaryMCPAuth)
	// A declared path and app name carrying control sequences.
	h.write(".agent/x\x1b]0;SYNTHETIC_CANARY\x07.json", `{"broken":`)
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"pi": {Paths: []manifest.PathMapping{
			{Src: "~/.pi/agent/mcp.json", Dest: "configs/pi/mcp.json"},
			{Src: "~/.pi/agent/auth.json", Dest: "configs/pi/auth.json"},
			{Src: "~/.pi/agent/mcp-auth.json", Dest: "configs/pi/mcp-auth.json"},
		}},
		"agent\x1b[2J": {Paths: []manifest.PathMapping{{Src: "~/.agent/x\x1b]0;SYNTHETIC_CANARY\x07.json", Dest: "configs/agent/x.json"}}},
	}}

	r := ScanReadiness(m, []StepResult{
		{Step: Step{AppName: "pi"}, Install: OutcomeAlreadyInstalled},
		{Step: Step{AppName: "agent\x1b[2J"}, Install: OutcomeAlreadyInstalled},
	}, h.opts())
	lines := r.Lines(false)

	if leaks := canaryLeaks(r, lines); len(leaks) > 0 {
		t.Errorf("canary or control character escaped into: %v", leaks)
	}
	// The user still learns what to do and where.
	if it, ok := findItem(r, "pi", "mcpServers.stitch.headers.X-Goog-Api-Key"); !ok || it.State != ReadinessMissing || it.File != "~/.pi/agent/mcp.json" {
		t.Errorf("trusted field path must stay readable")
	}
	if it, ok := findItem(r, "pi", "GITHUB_PERSONAL_ACCESS_TOKEN"); !ok || it.Kind != "env" || !strings.Contains(it.Action, "export GITHUB_PERSONAL_ACCESS_TOKEN") {
		t.Errorf("referenced variable name must stay readable")
	}
	if it, ok := findItem(r, "pi", "anthropic"); !ok || it.State != ReadinessLoginPending || it.File != "~/.pi/agent/auth.json" {
		t.Errorf("known provider must stay readable")
	}
	if it, ok := findItem(r, "pi", "MCP server linear"); !ok || it.State != ReadinessLoginPending || it.File != "~/.pi/agent/mcp-auth.json" {
		t.Errorf("known MCP OAuth entry must stay readable")
	}
	var deps, redactedFields, redactedProviders int
	for _, a := range r.Apps {
		for _, it := range a.Items {
			switch {
			case it.Kind == "dependency" && it.File == "~/.pi/agent/mcp.json" && strings.HasPrefix(it.Field, "mcpServers.") && strings.HasSuffix(it.Field, ".command"):
				deps++
			case it.Kind == "field" && strings.Contains(it.Name, "<key #"):
				redactedFields++
			case it.Kind == "auth" && it.State == ReadinessLoginPending && strings.HasPrefix(it.Name, "provider entry #"):
				redactedProviders++
			}
		}
	}
	if deps != 1 {
		t.Errorf("missing MCP command must be reported by field and file, got %d items", deps)
	}
	if redactedFields < 3 {
		t.Errorf("unsafe keys must be reported under redacted labels, got %d", redactedFields)
	}
	if redactedProviders != 3 {
		t.Errorf("unknown provider keys must be reported as indexed entries, got %d", redactedProviders)
	}
	agentFile := false
	for _, a := range r.Apps {
		for _, it := range a.Items {
			if it.Kind == "file" && it.File == "path #1 of app #2" && it.Detail == "not valid JSON; not scanned" {
				agentFile = true
			}
		}
	}
	if !agentFile {
		t.Errorf("malformed declared file must still be reported by its manifest position")
	}
}

func TestReadinessLinesEscapeControlsOnEveryDisplayedPart(t *testing.T) {
	r := &ReadinessReport{Apps: []AppReadiness{{App: "app\x1b[2J", Items: []ReadinessItem{{
		State: ReadinessMissing, Kind: "field", Name: "a\nb", File: "~/x\x07", Field: "f\u202e", Detail: "d\x00", Action: "act\xff\r",
	}}}}}
	for i, l := range r.Lines(false) {
		for _, c := range l {
			if c < 0x20 || c == 0x7f || c == '\u202e' || c == utf8.RuneError {
				t.Errorf("line %d keeps a control, format or invalid character", i)
				break
			}
		}
	}
}

// The shipped Pi credential templates must hold placeholders, never an
// account identity or an OAuth secret. Failures name the field only.
func TestPiCredentialTemplatesHoldNoIdentityValues(t *testing.T) {
	for file, fields := range map[string][]string{
		"../../configs/pi/auth.json.template":     {"antigravity.email", "antigravity.projectId", "openai.clientId", "openai-codex.accountId"},
		"../../configs/pi/mcp-auth.json.template": {"*.codeVerifier", "*.clientInformation.client_id"},
	} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("%s is not valid JSON", file)
		}
		for _, field := range fields {
			found := 0
			values := []any{any(doc)}
			for _, part := range strings.Split(field, ".") {
				var next []any
				for _, v := range values {
					m, _ := v.(map[string]any)
					if part != "*" {
						next = append(next, m[part])
						continue
					}
					for _, c := range m {
						next = append(next, c)
					}
				}
				values = next
			}
			for _, v := range values {
				if v == nil {
					continue
				}
				found++
				if s, ok := v.(string); !ok || !placeholderRe.MatchString(s) {
					t.Errorf("%s: %s must be a YOUR_…_HERE placeholder", file, field)
				}
			}
			if found == 0 {
				t.Errorf("%s: %s not found", file, field)
			}
		}
	}
}

// Secret-shaped canaries that are lexically valid variable, header and path
// names. Shape alone cannot prove a name is not a secret, so none of them may
// reach the structured result or the rendered report.
const secretNameCanary = "SYNTHCANARY"

const secretNameMCP = `{"mcpServers":{
	"linear":{"url":"https://mcp.linear.app/mcp","headers":{
		"sk-SYNTHCANARY-a1b2c3d4e5f6":"YOUR_HEADER_HERE",
		"Authorization":"Bearer ${ghp_SYNTHCANARYa1b2c3d4e5f6a7b8c9d0e1f2a3b4}"}},
	"postgres":{"command":"pg-mcp","env":{
		"AKIASYNTHCANARY7EXAMPLE":"YOUR_ENV_HERE",
		"PGSQL_MCP_CONNECTION_STRING":"${DATABASE_URL}",
		"LINEAR_API_KEY":"${LINEAR_API_KEY}"}}}}`

const secretNameTOML = "[model_providers.azure]\nenv_key = \"xoxbSYNTHCANARY0123456789\"\n\n" +
	"[mcp_servers.github]\ncommand = \"gh-mcp\"\nenv_vars = [\"ghs_SYNTHCANARY9z8y7x\", \"GITHUB_PERSONAL_ACCESS_TOKEN\"]\n"

func TestReadinessNeverEchoesSecretShapedNamesOrPaths(t *testing.T) {
	h := newReadinessHome(t)
	h.path["pg-mcp"], h.path["gh-mcp"] = true, true
	h.write(".pi/agent/mcp.json", secretNameMCP)
	h.write(".codex/config.toml", secretNameTOML)
	secretPath := "~/.config/ghp_SYNTHCANARYpath0123/mcp.json"
	h.write(".config/ghp_SYNTHCANARYpath0123/mcp.json", `{"key":"YOUR_KEY_HERE"}`)
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"pi":    {Paths: []manifest.PathMapping{{Src: "~/.pi/agent/mcp.json", Dest: "configs/pi/mcp.json"}}},
		"codex": {Paths: []manifest.PathMapping{{Src: "~/.codex/config.toml", Dest: "configs/codex/config.toml"}}},
		"tool": {Paths: []manifest.PathMapping{
			{Src: "~/.config/tool/absent.json", Dest: "configs/tool/absent.json"},
			{Src: secretPath, Dest: "configs/tool/mcp.json"},
		}},
	}}

	r, out := h.scan(m, "pi", "codex", "tool")

	// Failures name the part only: the report may carry the canary.
	var leaks []string
	for ai, a := range r.Apps {
		for ii, it := range a.Items {
			for label, v := range map[string]string{"name": it.Name, "file": it.File, "field": it.Field, "detail": it.Detail, "action": it.Action} {
				if strings.Contains(v, secretNameCanary) {
					leaks = append(leaks, fmt.Sprintf("apps[%d].items[%d].%s", ai, ii, label))
				}
			}
		}
	}
	if strings.Contains(out, secretNameCanary) {
		leaks = append(leaks, "rendered lines")
	}
	if len(leaks) > 0 {
		t.Errorf("secret-shaped name escaped into: %v", leaks)
	}

	// Conventional and known variable names stay useful.
	if it, ok := findItem(r, "pi", "DATABASE_URL"); !ok || it.Field != "mcpServers.postgres.env.PGSQL_MCP_CONNECTION_STRING" ||
		it.File != "~/.pi/agent/mcp.json" || !strings.Contains(it.Action, "export DATABASE_URL") {
		t.Errorf("DATABASE_URL must be reported with its field, file and action")
	}
	if it, ok := findItem(r, "pi", "LINEAR_API_KEY"); !ok || it.Field != "mcpServers.postgres.env.LINEAR_API_KEY" {
		t.Errorf("known service variable must be reported by name and field")
	}
	if _, ok := findItem(r, "codex", "GITHUB_PERSONAL_ACCESS_TOKEN"); !ok {
		t.Errorf("env_vars entry with a conventional name must be reported by name")
	}
	// Untrusted names are still reported, by field and file, under a generic label.
	var redactedEnv, redactedFields int
	for _, a := range r.Apps {
		for _, it := range a.Items {
			switch {
			case it.Kind == "env" && it.Name == "<variable>" && it.File != "" && it.Field != "":
				redactedEnv++
			case it.Kind == "field" && strings.Contains(it.Name, "<key #") && it.File == "~/.pi/agent/mcp.json":
				redactedFields++
			}
		}
	}
	if redactedEnv != 3 {
		t.Errorf("secret-shaped variable references must be reported redacted with field and file, got %d", redactedEnv)
	}
	if redactedFields != 2 {
		t.Errorf("secret-shaped header and env keys must be reported as <key #N>, got %d", redactedFields)
	}
	// A path the built-in inventory does not declare is named by its position.
	toolItems := 0
	for _, a := range r.Apps {
		for _, it := range a.Items {
			if a.App == "app #3" && it.Kind == "field" && it.File == "path #2 of app #3" {
				toolItems++
			}
		}
	}
	if toolItems != 1 {
		t.Errorf("a path outside the built-in inventory must be reported by its manifest position, got %d", toolItems)
	}
}

func TestSafeVarNameAllowsOnlyTheFixedAllowList(t *testing.T) {
	for i, c := range []struct {
		name   string
		header bool
		want   bool
	}{
		{"DATABASE_URL", false, true},
		{"LINEAR_API_KEY", false, true},
		{"GITHUB_PERSONAL_ACCESS_TOKEN", false, true},
		{"PGSQL_MCP_CONNECTION_STRING", false, true},
		{"X-Goog-Api-Key", true, true},
		{"authorization", true, true},
		{"X-Api-Key", true, true},
		// Conventional-looking names outside the allow-list are never printed.
		{"xoxb-SYNTH-CANARY-key", true, false},
		{"SYNTH_CANARY_API_KEY", false, false},
		{"SYNTH_CANARY_ACCESS_TOKEN", false, false},
		{"Crafted-Looking-Key", true, false},
		{"GH_MCP_TOKEN", false, false},
		{"database_url", false, false},
		{"ghp_SYNTHCANARYa1b2c3d4e5f6", false, false},
		{"AKIASYNTHCANARY7EXAMPLE", false, false},
		{"sk-SYNTHCANARY-a1b2c3d4e5f6", true, false},
		{"X-Api-Key", false, false},
		{"DATABASE_URL", true, false},
		{"", false, false},
	} {
		if got := safeVarName(c.name, c.header); got != c.want {
			t.Errorf("safeVarName case #%d header=%v = %v, want %v", i, c.header, got, c.want)
		}
	}
}

// Original adversarial inputs of the strict policy: names that look
// conventional (a vendor-prefixed header ending in "key", an env variable
// ending in API_KEY or TOKEN) and a printable custom app name. None is in
// the built-in allow-list, so none may be echoed; each is reported under a
// fixed or positional label. Failures name the part only.
const strictCanary = "SYNTH"

const strictMCP = `{"mcpServers":{
	"linear":{"headers":{"xoxb-SYNTH-CANARY-key":"YOUR_HEADER_HERE","Authorization":"YOUR_AUTHORIZATION_HERE"}},
	"postgres":{"env":{
		"SYNTH_CANARY_API_KEY":"YOUR_ENV_HERE",
		"PGSQL_MCP_CONNECTION_STRING":"${DATABASE_URL}",
		"LINEAR_API_KEY":"${SYNTH_CANARY_ACCESS_TOKEN}"}}}}`

const strictTOML = "[model_providers.azure]\nenv_key = \"SYNTH_CANARY_API_KEY\"\n\n" +
	"[mcp_servers.github]\nenv_vars = [\"GITHUB_PERSONAL_ACCESS_TOKEN\", \"SYNTH_CANARY_TOKEN\"]\n"

func TestReadinessStrictPolicyEchoesOnlyAllowListedNames(t *testing.T) {
	h := newReadinessHome(t)
	h.write(".pi/agent/mcp.json", strictMCP)
	h.write(".codex/config.toml", strictTOML)
	h.write(".config/synthcanaryapp/mcp.json", `{"key":"YOUR_KEY_HERE"}`)
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"pi":             {Paths: []manifest.PathMapping{{Src: "~/.pi/agent/mcp.json", Dest: "configs/pi/mcp.json"}}},
		"codex":          {Paths: []manifest.PathMapping{{Src: "~/.codex/config.toml", Dest: "configs/codex/config.toml"}}},
		"SYNTHCANARYAPP": {Paths: []manifest.PathMapping{{Src: "~/.config/synthcanaryapp/mcp.json", Dest: "configs/x/mcp.json"}}},
	}}

	r, out := h.scan(m, "pi", "codex", "SYNTHCANARYAPP")

	var leaks []string
	for ai, a := range r.Apps {
		if strings.Contains(a.App, strictCanary) {
			leaks = append(leaks, fmt.Sprintf("apps[%d].app", ai))
		}
		for ii, it := range a.Items {
			for label, v := range map[string]string{"name": it.Name, "file": it.File, "field": it.Field, "detail": it.Detail, "action": it.Action} {
				if strings.Contains(v, strictCanary) {
					leaks = append(leaks, fmt.Sprintf("apps[%d].items[%d].%s", ai, ii, label))
				}
			}
		}
	}
	if strings.Contains(out, strictCanary) {
		leaks = append(leaks, "rendered lines")
	}
	if len(leaks) > 0 {
		t.Errorf("non-allow-listed name escaped into: %v", leaks)
	}

	// Allow-listed names stay readable.
	if it, ok := findItem(r, "pi", "DATABASE_URL"); !ok || it.Field != "mcpServers.postgres.env.PGSQL_MCP_CONNECTION_STRING" || !strings.Contains(it.Action, "export DATABASE_URL") {
		t.Errorf("DATABASE_URL must stay readable with its field and action")
	}
	if _, ok := findItem(r, "pi", "mcpServers.linear.headers.Authorization"); !ok {
		t.Errorf("known header must stay readable")
	}
	if _, ok := findItem(r, "codex", "GITHUB_PERSONAL_ACCESS_TOKEN"); !ok {
		t.Errorf("GITHUB_PERSONAL_ACCESS_TOKEN must stay readable")
	}
	// Unknown names: fixed or positional labels, still with field and file.
	var envLabels, keyLabels int
	for _, a := range r.Apps {
		for _, it := range a.Items {
			switch {
			case it.Kind == "env" && it.Name == "<variable>" && it.Field != "" && it.File != "":
				envLabels++
			case it.Kind == "field" && strings.Contains(it.Name, "<key #") && it.File == "~/.pi/agent/mcp.json":
				keyLabels++
			}
		}
	}
	if envLabels != 3 {
		t.Errorf("unknown env names must be reported as <variable>, got %d", envLabels)
	}
	if keyLabels != 2 {
		t.Errorf("unknown header and env keys must be reported as <key #N>, got %d", keyLabels)
	}
	// An app outside the built-in inventory is named by its position.
	custom := false
	for _, a := range r.Apps {
		if a.App != "app #3" {
			continue
		}
		for _, it := range a.Items {
			if it.Kind == "field" && it.File == "path #1 of app #3" {
				custom = true
			}
		}
	}
	if !custom {
		t.Errorf("custom app must be reported as app #3 with its path by position")
	}
}
