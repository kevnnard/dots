package restore

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kevnnard/dots/pkg/manifest"
	"gopkg.in/yaml.v3"
)

// ReadinessState classifies one thing a user still has to do before an app
// is ready. The scanner is offline: it never validates a key or a session.
type ReadinessState string

const (
	// ReadinessMissing: a field still holds a dots placeholder, or a
	// referenced environment variable is not set.
	ReadinessMissing ReadinessState = "missing"
	// ReadinessLoginPending: a credentials file still holds the placeholders
	// of the dots template, so no login has been recorded yet.
	ReadinessLoginPending ReadinessState = "login-pending"
	// ReadinessDependencyMissing: a command the config launches is not found.
	ReadinessDependencyMissing ReadinessState = "dependency-missing"
	// ReadinessNotVerified: dots cannot tell; the action says how to check.
	ReadinessNotVerified ReadinessState = "not-verified"
)

// ReadinessItem is one action for one app. It carries names and locations
// only, never a value or file content. Names read from a file are shown only
// when they are trusted schema identifiers or allow-listed variable and
// header names (safeVarName); any other key is shown as a typed placeholder
// such as "<key #2>" or "<variable>". Files are shown only when the built-in
// inventory declares them; any other file is "path #N of <app>".
type ReadinessItem struct {
	State  ReadinessState `json:"state"`
	Kind   string         `json:"kind"`            // field, env, auth, dependency, file
	Name   string         `json:"name"`            // field path, variable name, provider label or path
	File   string         `json:"file,omitempty"`  // ~/relative path in the target home
	Field  string         `json:"field,omitempty"` // field that references Name, for env and dependency items
	Detail string         `json:"detail,omitempty"`
	Action string         `json:"action"`
}

// AppReadiness lists the actions found for one app.
type AppReadiness struct {
	App   string          `json:"app"`
	Items []ReadinessItem `json:"items"`
}

// ReadinessReport lists, per app, what still needs the user's action.
type ReadinessReport struct {
	Apps []AppReadiness `json:"apps"`
}

// ReadinessOptions tunes ScanReadiness. Zero values use the current home,
// environment and PATH.
type ReadinessOptions struct {
	// Home is the target home; only files declared by the apps under it are
	// read, and nothing resolving outside it is followed.
	Home      string
	LookupEnv func(string) (string, bool)
	LookPath  func(string) (string, error)
	// MaxFileSize bounds every file read; larger files are not scanned.
	MaxFileSize int64
}

const defaultReadinessMaxFileSize = 1 << 20

// authRule describes where an app keeps its login, as far as dots knows.
type authRule struct {
	provider string
	// file is the ~/ path of the credentials file; empty means the session
	// lives outside the files dots manages.
	file string
	// perKey reports a pending login per top-level key (one per provider).
	perKey bool
	// marker, when set, makes the rule apply only if file contains it; the
	// file is then evidence of use, not a credentials store.
	marker string
	action string
}

var authRules = map[string][]authRule{
	"claude": {{provider: "Claude Code session", action: "run `claude` and use /login if prompted"}},
	"codex":  {{provider: "Codex session", file: "~/.codex/auth.json", action: "run `codex login`"}},
	"gh":     {{provider: "GitHub CLI session", file: "~/.config/gh/hosts.yml", action: "run `gh auth login` (check with `gh auth status`)"}},
	"pi": {
		{provider: "Pi provider login", file: "~/.pi/agent/auth.json", perKey: true, action: "run `pi` and use /login"},
		{provider: "Pi MCP OAuth", file: "~/.pi/agent/mcp-auth.json", perKey: true, action: "run `pi` and authenticate the MCP server when prompted"},
	},
	"gentle-shell": {
		{provider: "Gentle Shell provider login", file: "~/.gentle-shell/agent/auth.json", perKey: true, action: "run `gentle-shell` and use /login"},
		{provider: "Gentle Shell MCP OAuth", file: "~/.gentle-shell/agent/mcp-auth.json", perKey: true, action: "run `gentle-shell` and authenticate the MCP server when prompted"},
	},
	"nvim": {{provider: "GitHub Copilot (Neovim)", file: "~/.config/nvim/lua/config/lazy.lua", marker: "copilot", action: "open nvim and run :Copilot auth"}},
}

var (
	placeholderRe = regexp.MustCompile(`(?i)^your_.*_here$`)
	envRefRe      = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-[^}]*)?\}|\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)
	// envNameRe is a whole, bounded variable name; anything else is redacted.
	envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	// pathPartRe is one printable component of a declared ~/ path.
	pathPartRe = regexp.MustCompile(`^[A-Za-z0-9._@+-]{1,128}$`)
)

// knownEnvNames are the environment variables of the services dots ships
// configs for. They are the only variable names ever printed.
var knownEnvNames = setOf(
	"PGSQL_MCP_CONNECTION_STRING", "DATABASE_URL", "OPENAI_API_KEY", "ANTHROPIC_API_KEY",
	"GITHUB_TOKEN", "GITHUB_PERSONAL_ACCESS_TOKEN", "GH_TOKEN", "LINEAR_API_KEY",
	"CONTEXT7_API_KEY", "NEON_API_KEY", "SUPABASE_ACCESS_TOKEN", "NOTION_TOKEN",
	"AZURE_OPENAI_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY", "OPENROUTER_API_KEY",
)

// knownHeaderNames are the HTTP headers of the shipped MCP configs, in lower
// case. They are the only header names ever printed.
var knownHeaderNames = setOf(
	"authorization", "x-goog-api-key", "x-api-key", "api-key", "notion-version",
)

// safeVarName reports whether a variable (header false) or header (header
// true) name may be printed. No rule can prove from its shape that a name is
// not a secret, so the policy is a fixed allow-list: only knownEnvNames (exact)
// and knownHeaderNames (any case) are printed; every other name is shown
// under a fixed or positional label.
func safeVarName(name string, header bool) bool {
	if header {
		return knownHeaderNames[strings.ToLower(name)]
	}
	return knownEnvNames[name]
}

// trustedFields are schema identifiers of the configs dots manages. Only
// these keys, entries of trustedEntries and variable names are printed.
var trustedFields = setOf(
	"mcpServers", "mcp_servers", "mcp", "servers", "model_providers", "providers",
	"headers", "http_headers", "env_http_headers", "env", "environment", "env_vars",
	"env_key", "bearer_token_env_var", "command", "args", "url", "type",
	"auth", "auth_mode", "tokens", "token", "access", "refresh", "access_token",
	"refresh_token", "id_token", "token_type", "OPENAI_API_KEY", "apiKey", "api_key",
	"Authorization", "key", "username", "password", "baseURL", "base_url",
)

// trustedEntries are the MCP server and model provider names dots ships;
// they are printed only as entries of a server or provider table.
var trustedEntries = setOf(
	"codegraph", "context7", "engram", "linear", "neon", "postgres", "stitch",
	"supabase", "notion", "pencil", "github",
	"azure", "openai", "anthropic", "ollama", "openrouter", "lmstudio", "mistral",
	"groq", "xai", "gemini", "deepseek",
)

// trustedProviders are the login providers of the Pi credentials template.
var trustedProviders = setOf(
	"anthropic", "antigravity", "openai", "openai-codex", "github-copilot",
	"google-gemini-cli", "google-antigravity", "google", "groq", "xai",
	"openrouter", "mistral", "cerebras",
)

// varMaps hold variable names as keys (environment and header tables).
var varMaps = setOf("env", "environment", "headers", "http_headers", "env_http_headers")

func setOf(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// ScanReadiness inspects the files declared by the apps of a run (in the
// target home) and lists missing keys, pending logins and missing commands.
// Unsupported apps are skipped. It only reads; it never runs a command or
// contacts a service. Apps of the built-in inventory are named; any other
// app is "app #N", its position in steps.
func ScanReadiness(m *manifest.Manifest, steps []StepResult, opts ReadinessOptions) *ReadinessReport {
	sc := newReadinessScanner(opts)
	builtin := manifest.DefaultManifest()
	trusted := trustedPaths(builtin)
	report := &ReadinessReport{}
	for i, s := range steps {
		if s.Install == OutcomeUnsupported {
			continue
		}
		label := s.AppName
		if _, ok := builtin.Apps[s.AppName]; !ok {
			label = fmt.Sprintf("app #%d", i+1)
		}
		if items := sc.scanApp(s.AppName, label, m.Apps[s.AppName], trusted); len(items) > 0 {
			report.Apps = append(report.Apps, AppReadiness{App: label, Items: items})
		}
	}
	return report
}

type readinessScanner struct {
	home, root string // home as given and with symlinks resolved
	lookupEnv  func(string) (string, bool)
	lookPath   func(string) (string, error)
	max        int64
}

func newReadinessScanner(opts ReadinessOptions) *readinessScanner {
	sc := &readinessScanner{home: opts.Home, lookupEnv: opts.LookupEnv, lookPath: opts.LookPath, max: opts.MaxFileSize}
	if sc.home == "" {
		sc.home, _ = os.UserHomeDir()
	}
	if sc.lookupEnv == nil {
		sc.lookupEnv = os.LookupEnv
	}
	if sc.lookPath == nil {
		sc.lookPath = exec.LookPath
	}
	if sc.max <= 0 {
		sc.max = defaultReadinessMaxFileSize
	}
	if sc.home != "" {
		if r, err := filepath.EvalSymlinks(sc.home); err == nil {
			sc.root = r
		}
	}
	return sc
}

// trustedPaths are the ~/ files whose location may be printed: those the
// built-in inventory and the auth rules declare. Any other declared path may
// carry an arbitrary name, so it is shown by its position in the manifest.
func trustedPaths(builtin *manifest.Manifest) map[string]bool {
	trusted := map[string]bool{}
	for _, app := range builtin.Apps {
		for _, p := range app.Paths {
			trusted[p.Src] = true
		}
	}
	for _, rules := range authRules {
		for _, rule := range rules {
			if rule.file != "" {
				trusted[rule.file] = true
			}
		}
	}
	return trusted
}

// scanApp scans one app; name selects its auth rules, label is how the
// report names it.
func (sc *readinessScanner) scanApp(name, label string, app manifest.AppConfig, trusted map[string]bool) []ReadinessItem {
	var items []ReadinessItem
	authFiles := map[string]bool{}
	for _, rule := range authRules[name] {
		if rule.file != "" && declares(app, rule.file) {
			authFiles[rule.file] = true
		}
		items = append(items, sc.scanAuth(rule, declares(app, rule.file))...)
	}
	for i, p := range app.Paths {
		if !strings.HasPrefix(p.Src, "~/") || authFiles[p.Src] {
			continue
		}
		where := displayPath(p.Src)
		if !trusted[p.Src] {
			where = fmt.Sprintf("path #%d of %s", i+1, label)
		}
		items = append(items, sc.scanConfig(p.Src, where)...)
	}
	return items
}

// declares reports whether file is one of the app's ~/ paths or inside one.
func declares(app manifest.AppConfig, file string) bool {
	if file == "" {
		return false
	}
	for _, p := range app.Paths {
		if !strings.HasPrefix(p.Src, "~/") {
			continue
		}
		if file == p.Src || strings.HasPrefix(file, strings.TrimSuffix(p.Src, "/")+"/") {
			return true
		}
	}
	return false
}

// fileState is why a declared file could or could not be read.
type fileState int

const (
	fileRead fileState = iota
	fileAbsent
	fileUnreadable
)

// read returns the content of a ~/ path when it is a regular file inside the
// target home and within the size limit. The reason never quotes content.
func (sc *readinessScanner) read(tilde string) ([]byte, fileState, string) {
	if sc.root == "" {
		return nil, fileUnreadable, "target home not available"
	}
	rel := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(tilde, "~/")))
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, fileUnreadable, "path leaves the target home; not scanned"
	}
	path := filepath.Join(sc.home, rel)
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil, fileAbsent, ""
	} else if err != nil {
		return nil, fileUnreadable, "cannot be inspected; not scanned"
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fileAbsent, ""
		}
		return nil, fileUnreadable, "cannot be resolved; not scanned"
	}
	if r, err := filepath.Rel(sc.root, resolved); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return nil, fileUnreadable, "resolves outside the target home; not scanned"
	}
	info, err := os.Stat(resolved)
	switch {
	case err != nil:
		return nil, fileUnreadable, "cannot be inspected; not scanned"
	case info.IsDir():
		return nil, fileUnreadable, "is a directory; not scanned"
	case !info.Mode().IsRegular():
		return nil, fileUnreadable, "is not a regular file; not scanned"
	case info.Size() > sc.max:
		return nil, fileUnreadable, fmt.Sprintf("larger than %d bytes; not scanned", sc.max)
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, fileUnreadable, "cannot be read; not scanned"
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, sc.max+1))
	if err != nil {
		return nil, fileUnreadable, "cannot be read; not scanned"
	}
	if int64(len(data)) > sc.max {
		return nil, fileUnreadable, fmt.Sprintf("larger than %d bytes; not scanned", sc.max)
	}
	return data, fileRead, ""
}

func (sc *readinessScanner) scanAuth(rule authRule, declared bool) []ReadinessItem {
	notVerified := func(file, detail string) []ReadinessItem {
		return []ReadinessItem{{State: ReadinessNotVerified, Kind: "auth", Name: rule.provider, File: displayPath(file), Detail: detail, Action: rule.action}}
	}
	if rule.file == "" || !declared {
		if rule.marker != "" {
			return nil // no evidence the feature is used
		}
		return notVerified("", "stored outside the files dots manages")
	}
	data, state, reason := sc.read(rule.file)
	if rule.marker != "" {
		if state != fileRead || !bytes.Contains(bytes.ToLower(data), []byte(rule.marker)) {
			return nil
		}
		return notVerified(rule.file, "enabled by this config; sign-in is stored elsewhere")
	}
	switch state {
	case fileAbsent:
		return notVerified(rule.file, "no credentials file yet; a session may live elsewhere")
	case fileUnreadable:
		return notVerified(rule.file, reason)
	}
	doc, err := parseConfig(rule.file, data)
	if err != nil {
		return notVerified(rule.file, err.Error())
	}
	var pending []string
	if top, ok := doc.(map[string]any); ok && rule.perKey {
		for i, k := range sortedKeys(top) {
			if hasPlaceholder(top[k]) {
				pending = append(pending, providerLabel(k, i+1))
			}
		}
	} else if hasPlaceholder(doc) {
		pending = append(pending, rule.provider)
	}
	if len(pending) == 0 {
		return notVerified(rule.file, "credentials file present; validity not checked")
	}
	var items []ReadinessItem
	for _, name := range pending {
		items = append(items, ReadinessItem{State: ReadinessLoginPending, Kind: "auth", Name: name, File: displayPath(rule.file),
			Detail: "still holds the dots template placeholders", Action: rule.action})
	}
	return items
}

// scanConfig scans a declared ~/ file; label is how the report names it.
func (sc *readinessScanner) scanConfig(tilde, label string) []ReadinessItem {
	format := configFormat(tilde)
	if format == "" {
		return nil
	}
	data, state, reason := sc.read(tilde)
	switch state {
	case fileAbsent:
		return nil
	case fileUnreadable:
		if reason == "is a directory; not scanned" {
			return nil
		}
		return []ReadinessItem{{State: ReadinessNotVerified, Kind: "file", Name: label, File: label, Detail: reason, Action: "check this file by hand"}}
	}
	doc, err := parseConfig(tilde, data)
	if err != nil {
		return []ReadinessItem{{State: ReadinessNotVerified, Kind: "file", Name: label, File: label, Detail: err.Error(), Action: "fix the file syntax"}}
	}
	w := &configWalker{sc: sc, file: label, seen: map[string]bool{}}
	w.walk(doc, nil)
	return w.items
}

func configFormat(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return "json"
	case ".toml":
		return "toml"
	case ".yml", ".yaml":
		return "yaml"
	}
	return ""
}

// parseConfig decodes a declared file. Errors name the format only: decoder
// messages may quote the content.
func parseConfig(path string, data []byte) (any, error) {
	var doc any
	switch configFormat(path) {
	case "json":
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("not valid JSON; not scanned")
		}
	case "yaml":
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("not valid YAML; not scanned")
		}
	case "toml":
		doc = parseTOMLStrings(data)
	default:
		return nil, fmt.Errorf("unsupported format; not scanned")
	}
	return doc, nil
}

// parseTOMLStrings keeps the single-line string and string-array values of a
// TOML file, keyed by table. It is enough to find placeholders, environment
// variable names and MCP commands; other values are ignored.
func parseTOMLStrings(data []byte) any {
	root := map[string]any{}
	table := root
	s := bufio.NewScanner(bytes.NewReader(data))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			header := strings.TrimPrefix(strings.TrimPrefix(line, "["), "[")
			header, _, _ = strings.Cut(header, "]")
			table = root
			for _, part := range tomlKeyPath(header) {
				next, ok := table[part].(map[string]any)
				if !ok {
					next = map[string]any{}
					table[part] = next
				}
				table = next
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		parts := tomlKeyPath(key)
		if len(parts) == 0 {
			continue
		}
		var v any
		value = strings.TrimSpace(value)
		if str, ok := tomlString(value); ok {
			v = str
		} else if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
			var list []any
			for _, el := range strings.Split(strings.Trim(value, "[]"), ",") {
				if str, ok := tomlString(strings.TrimSpace(el)); ok {
					list = append(list, str)
				}
			}
			v = list
		} else {
			continue
		}
		t := table
		for _, part := range parts[:len(parts)-1] {
			next, ok := t[part].(map[string]any)
			if !ok {
				next = map[string]any{}
				t[part] = next
			}
			t = next
		}
		t[parts[len(parts)-1]] = v
	}
	return root
}

func tomlKeyPath(key string) []string {
	var out []string
	for _, part := range strings.Split(key, ".") {
		part = strings.Trim(strings.TrimSpace(part), `"'`)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func tomlString(v string) (string, bool) {
	if len(v) >= 2 && v[0] == '"' {
		if end := strings.LastIndex(v, `"`); end > 0 {
			if s, err := strconv.Unquote(v[:end+1]); err == nil {
				return s, true
			}
			return v[1:end], true
		}
	}
	if len(v) >= 2 && v[0] == '\'' {
		if end := strings.LastIndex(v, "'"); end > 0 {
			return v[1:end], true
		}
	}
	return "", false
}

// configWalker collects readiness items from a decoded config document.
type configWalker struct {
	sc    *readinessScanner
	file  string
	items []ReadinessItem
	seen  map[string]bool
}

func (w *configWalker) add(item ReadinessItem) {
	key := string(item.State) + "\x00" + item.Kind + "\x00" + item.Name + "\x00" + item.Field
	if w.seen[key] {
		return
	}
	w.seen[key] = true
	item.File = w.file
	w.items = append(w.items, item)
}

// pathSeg is one step of a field path: a map key or an array index. The raw
// key drives detection only; fieldPath decides what may be printed.
type pathSeg struct {
	key     string
	ord     int // 1-based position among the sorted keys of its map
	index   int
	isIndex bool
}

func withSeg(path []pathSeg, seg pathSeg) []pathSeg {
	return append(append([]pathSeg(nil), path...), seg)
}

// lastKey is the raw key of the last step, or "" for an array element.
func lastKey(path []pathSeg) string {
	if len(path) == 0 || path[len(path)-1].isIndex {
		return ""
	}
	return path[len(path)-1].key
}

func (w *configWalker) walk(v any, path []pathSeg) {
	switch t := v.(type) {
	case map[string]any:
		for i, k := range sortedKeys(t) {
			w.walk(t[k], withSeg(path, pathSeg{key: k, ord: i + 1}))
		}
	case []any:
		key := lastKey(path)
		for i, el := range t {
			if s, ok := el.(string); ok && key == "env_vars" {
				w.envName(s, path)
				continue
			}
			w.walk(el, withSeg(path, pathSeg{index: i, isIndex: true}))
		}
	case string:
		w.value(t, path)
	}
}

func (w *configWalker) value(s string, path []pathSeg) {
	field := fieldPath(path)
	key := strings.ToLower(lastKey(path))
	if placeholderRe.MatchString(strings.TrimSpace(s)) {
		w.add(ReadinessItem{State: ReadinessMissing, Kind: "field", Name: field, Action: "edit this field and set the real value"})
		return
	}
	for _, m := range envRefRe.FindAllStringSubmatch(s, -1) {
		name, hasDefault := m[1], m[2] != ""
		if name == "" {
			name = m[3]
		}
		if !hasDefault {
			w.env(name, field)
		}
	}
	switch {
	case key == "env_key" || strings.HasSuffix(key, "_env_var"):
		w.envName(s, path)
	case key == "command" && len(path) == 3 && !path[0].isIndex && !path[1].isIndex && isMCPTable(path[0].key):
		w.command(s, field)
	}
}

func (w *configWalker) envName(s string, path []pathSeg) {
	if envNameRe.MatchString(s) {
		w.env(s, fieldPath(path))
	}
}

func (w *configWalker) env(name, field string) {
	if _, ok := w.sc.lookupEnv(name); ok {
		return
	}
	if !safeVarName(name, false) {
		w.add(ReadinessItem{State: ReadinessMissing, Kind: "env", Name: "<variable>", Field: field,
			Detail: "not set in the environment dots runs in", Action: "set the variable this field references"})
		return
	}
	w.add(ReadinessItem{State: ReadinessMissing, Kind: "env", Name: name, Field: field,
		Detail: "not set in the environment dots runs in", Action: fmt.Sprintf("export %s=… (e.g. in your shell profile)", name)})
}

// command reports an MCP command missing from PATH by its field only: the
// configured command may embed a path or arguments, so it is never printed.
func (w *configWalker) command(cmd, field string) {
	if cmd == "" {
		return
	}
	if _, err := w.sc.lookPath(cmd); err == nil {
		return
	}
	w.add(ReadinessItem{State: ReadinessDependencyMissing, Kind: "dependency", Name: "MCP server command", Field: field,
		Detail: "command not found on PATH", Action: "install the command this field launches, or fix the field"})
}

func isMCPTable(key string) bool {
	return key == "mcpServers" || key == "mcp_servers" || key == "mcp"
}

func hasPlaceholder(v any) bool {
	switch t := v.(type) {
	case map[string]any:
		for _, c := range t {
			if hasPlaceholder(c) {
				return true
			}
		}
	case []any:
		for _, c := range t {
			if hasPlaceholder(c) {
				return true
			}
		}
	case string:
		return placeholderRe.MatchString(strings.TrimSpace(t))
	}
	return false
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// fieldPath renders a field path. A key is printed only when it is a
// trusted schema identifier, a shipped server or provider entry, or an
// allow-listed variable or header name inside an environment or header
// table; any other key becomes
// "<key #N>", its position among the sorted keys of its table.
func fieldPath(path []pathSeg) string {
	var b strings.Builder
	for i, p := range path {
		if p.isIndex {
			fmt.Fprintf(&b, "[%d]", p.index)
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		parent := ""
		if i > 0 {
			parent = lastKey(path[:i])
		}
		switch {
		case trustedFields[p.key],
			trustedEntries[p.key] && (isMCPTable(parent) || parent == "model_providers" || parent == "providers"),
			varMaps[parent] && safeVarName(p.key, strings.Contains(parent, "header")):
			b.WriteString(p.key)
		default:
			fmt.Fprintf(&b, "<key #%d>", p.ord)
		}
	}
	return b.String()
}

// providerLabel names a top-level credentials entry: a known provider, or a
// shipped MCP server for "mcp__<name>|<url>" entries; any other entry is
// "provider entry #N", its position among the sorted keys.
func providerLabel(key string, ord int) string {
	if trustedProviders[key] {
		return key
	}
	if name, _, ok := strings.Cut(key, "|"); ok && strings.HasPrefix(name, "mcp__") {
		if name = strings.TrimPrefix(name, "mcp__"); trustedEntries[name] {
			return "MCP server " + name
		}
	}
	return fmt.Sprintf("provider entry #%d", ord)
}

// displayPath renders a trusted ~/ path, replacing every component that is
// not plainly printable so manifest data cannot forge report lines.
func displayPath(p string) string {
	if p == "" {
		return ""
	}
	parts := strings.Split(p, "/")
	for i, part := range parts {
		if (i == 0 && part == "~") || pathPartRe.MatchString(part) {
			continue
		}
		parts[i] = "<unprintable name>"
	}
	return strings.Join(parts, "/")
}

// escapeControls replaces control, format and invalid bytes with escapes so
// a rendered line can never move the cursor, retitle or reorder the terminal.
func escapeControls(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size <= 1:
			fmt.Fprintf(&b, "\\x%02x", s[i])
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// Count returns the number of items in the report.
func (r *ReadinessReport) Count() int {
	if r == nil {
		return 0
	}
	n := 0
	for _, a := range r.Apps {
		n += len(a.Items)
	}
	return n
}

// Lines renders the report for the shared run summary. Only names, paths
// and actions are printed.
func (r *ReadinessReport) Lines(dryRun bool) []string {
	title := "🔑 Readiness (offline check: names only, values never shown, no key or login validated):"
	if dryRun {
		title = "🔑 Readiness (dry run: current files, nothing deployed; values never shown, nothing validated):"
	}
	lines := []string{title}
	if r.Count() == 0 {
		return append(lines, "  No missing keys, pending logins or missing commands found in the scanned files.")
	}
	for _, a := range r.Apps {
		lines = append(lines, "  "+escapeControls(a.App))
		for _, it := range a.Items {
			lines = append(lines, "    "+escapeControls(it.line()))
		}
	}
	return lines
}

func (it ReadinessItem) line() string {
	var what string
	switch it.Kind {
	case "field":
		what = fmt.Sprintf("field %s", it.Name)
	case "env":
		what = fmt.Sprintf("env %s", it.Name)
	case "dependency":
		what = fmt.Sprintf("command %s", it.Name)
	case "file":
		what = "file"
	default:
		what = it.Name
	}
	where := ""
	switch {
	case it.File != "" && it.Field != "":
		where = fmt.Sprintf(" (%s in %s)", it.Field, it.File)
	case it.File != "":
		where = " in " + it.File
	}
	detail := ""
	if it.Detail != "" {
		detail = " — " + it.Detail
	}
	return fmt.Sprintf("%s %s: %s%s%s → %s", readinessIcon(it.State), it.State, what, where, detail, it.Action)
}

func readinessIcon(s ReadinessState) string {
	switch s {
	case ReadinessMissing:
		return "✗"
	case ReadinessLoginPending:
		return "⏳"
	case ReadinessDependencyMissing:
		return "⊘"
	default:
		return "?"
	}
}
