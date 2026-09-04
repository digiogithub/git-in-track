package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/config"
)

func TestAddRegistersTheRepository(t *testing.T) {
	h := newHarness(t)

	stdout := h.mustRun("add", h.Repo)
	if !strings.Contains(stdout, "added project repository acme-api") {
		t.Errorf("stdout = %q", stdout)
	}
	if !strings.Contains(stdout, "docs: docs") || !strings.Contains(stdout, "5 items") {
		t.Errorf("the detection was not reported: %q", stdout)
	}
	if !strings.Contains(stdout, "DEMO") {
		t.Errorf("the project key was not reported: %q", stdout)
	}

	cfg, err := config.Load(h.Config)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Repos) != 1 {
		t.Fatalf("repos = %+v", cfg.Repos)
	}
	repo := cfg.Repos[0]
	if repo.ID != "acme-api" || repo.Role != config.RoleProject || repo.DocsFolder != "docs" || !repo.Enabled {
		t.Errorf("registration = %+v", repo)
	}
	info, err := os.Stat(h.Config)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestAddJSON(t *testing.T) {
	h := newHarness(t)

	payload := decode[addPayload](t, h.mustRun("add", h.Repo, "--json"))
	if payload.Repo.ID != "acme-api" || payload.Repo.Docs != "docs" {
		t.Errorf("payload = %+v", payload)
	}
	if payload.Items != 5 {
		t.Errorf("items = %d, want the 5 items of the fixture", payload.Items)
	}
	if len(payload.Projects) != 1 || payload.Projects[0] != "DEMO" {
		t.Errorf("projects = %v", payload.Projects)
	}
	if !payload.Git {
		t.Error("the .git marker was not detected")
	}
}

func TestAddRefusesAFolderThatIsNotAGitRepository(t *testing.T) {
	h := newHarness(t)
	if err := os.RemoveAll(filepath.Join(h.Repo, ".git")); err != nil {
		t.Fatalf("remove .git: %v", err)
	}

	_, stderr, code := h.run("add", h.Repo)
	if code != exitFailure {
		t.Fatalf("exit = %d, want %d\n%s", code, exitFailure, stderr)
	}
	if _, _, code := h.run("add", h.Repo, "--no-git"); code != exitOK {
		t.Errorf("--no-git: exit %d", code)
	}
}

func TestAddRefusesADuplicate(t *testing.T) {
	h := newHarness(t)
	h.register()

	if _, _, code := h.run("add", h.Repo); code != exitFailure {
		t.Errorf("exit = %d, want %d for a duplicate", code, exitFailure)
	}
	cfg, err := config.Load(h.Config)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Repos) != 1 {
		t.Errorf("repos = %+v, want the duplicate refused", cfg.Repos)
	}
}

func TestAddNeedsAPath(t *testing.T) {
	h := newHarness(t)
	if _, _, code := h.run("add"); code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
}

func TestLsTable(t *testing.T) {
	h := newHarness(t)
	h.register()

	stdout := h.mustRun("ls")
	rows := lines(stdout)
	if len(rows) != 2 {
		t.Fatalf("rows = %q", rows)
	}
	if got := columns(rows[0]); strings.Join(got, "|") != "ID|ROLE|PATH|DOCS|KEYS|ITEMS" {
		t.Errorf("headers = %v", got)
	}
	cells := columns(rows[1])
	if len(cells) != 6 {
		t.Fatalf("cells = %v", cells)
	}
	if cells[0] != "acme-api" || cells[1] != "project" || cells[3] != "docs" || cells[4] != "DEMO" || cells[5] != "5" {
		t.Errorf("row = %v", cells)
	}
}

func TestLsJSON(t *testing.T) {
	h := newHarness(t)
	h.register()

	payload := decode[lsPayload](t, h.mustRun("ls", "--json"))
	if payload.Workspace != config.DefaultWorkspaceName {
		t.Errorf("workspace = %q", payload.Workspace)
	}
	if len(payload.Repos) != 1 {
		t.Fatalf("repos = %+v", payload.Repos)
	}
	repo := payload.Repos[0]
	if repo.ID != "acme-api" || repo.Items != 5 || len(repo.Projects) != 1 || repo.Projects[0] != "DEMO" {
		t.Errorf("repo = %+v", repo)
	}
	if repo.Pages == 0 {
		t.Errorf("the knowledge base pages were not counted: %+v", repo)
	}
}

func TestLsWithoutARegistration(t *testing.T) {
	h := newHarness(t)

	stdout := h.mustRun("ls")
	if !strings.Contains(stdout, "no repository is registered") {
		t.Errorf("stdout = %q", stdout)
	}
	payload := decode[lsPayload](t, h.mustRun("ls", "--json"))
	if len(payload.Repos) != 0 {
		t.Errorf("repos = %+v", payload.Repos)
	}
}

func TestRm(t *testing.T) {
	h := newHarness(t)
	h.register()

	stdout := h.mustRun("rm", "acme-api")
	if !strings.Contains(stdout, "removed project repository acme-api") {
		t.Errorf("stdout = %q", stdout)
	}
	cfg, err := config.Load(h.Config)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Repos) != 0 {
		t.Errorf("repos = %+v", cfg.Repos)
	}
	if _, err := os.Stat(h.Repo); err != nil {
		t.Errorf("rm touched the working tree: %v", err)
	}
}

func TestRmUnknownRepository(t *testing.T) {
	h := newHarness(t)
	if _, _, code := h.run("rm", "ghost"); code != exitNotFound {
		t.Errorf("exit = %d, want %d", code, exitNotFound)
	}
}

func TestIndex(t *testing.T) {
	h := newHarness(t)
	h.register()

	stdout := h.mustRun("index", "--full")
	if !strings.Contains(stdout, "acme-api  5 items") {
		t.Errorf("stdout = %q", stdout)
	}
	if !strings.Contains(stdout, "1 epic, 2 stories, 1 task, 1 milestone") {
		t.Errorf("the per-type breakdown is missing: %q", stdout)
	}

	payload := decode[indexPayload](t, h.mustRun("index", "--json"))
	if len(payload.Repos) != 1 || payload.Items != 5 {
		t.Fatalf("payload = %+v", payload)
	}
	row := payload.Repos[0]
	if row.ByType["story"] != 2 || row.ByType["task"] != 1 {
		t.Errorf("byType = %v", row.ByType)
	}
	if row.Files == 0 || row.Comments != 1 {
		t.Errorf("row = %+v", row)
	}
}

func TestIndexOneRepository(t *testing.T) {
	h := newHarness(t)
	h.register()

	if _, _, code := h.run("index", "ghost"); code != exitNotFound {
		t.Errorf("exit = %d, want %d", code, exitNotFound)
	}
	if stdout := h.mustRun("index", "acme-api"); !strings.Contains(stdout, "acme-api") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestConfigPath(t *testing.T) {
	h := newHarness(t)

	stdout := h.mustRun("config", "path")
	if strings.TrimSpace(stdout) != h.Config {
		t.Errorf("path = %q, want %q", strings.TrimSpace(stdout), h.Config)
	}
	payload := decode[configPathPayload](t, h.mustRun("config", "path", "--json"))
	if payload.Path != h.Config || payload.Exists {
		t.Errorf("payload = %+v", payload)
	}
	if payload.StateDir != filepath.Dir(h.Config) || payload.CacheDir != filepath.Dir(h.Config) {
		t.Errorf("payload = %+v", payload)
	}
}

func TestConfigInitAndShow(t *testing.T) {
	h := newHarness(t)

	if stdout := h.mustRun("config", "init"); !strings.Contains(stdout, h.Config) {
		t.Errorf("stdout = %q", stdout)
	}
	if _, _, code := h.run("config", "init"); code != exitFailure {
		t.Error("a second init overwrote the file")
	}
	if _, _, code := h.run("config", "init", "--force"); code != exitOK {
		t.Error("--force did not replace the file")
	}

	stdout := h.mustRun("config", "show")
	for _, want := range []string{"bind: 127.0.0.1", "port: 7317", "backend: auto", "debounce: 250ms"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("config show lost %q:\n%s", want, stdout)
		}
	}
	payload := decode[config.Config](t, h.mustRun("config", "show", "--json"))
	if payload.Server.Port != config.DefaultPort || payload.Git.Backend != config.BackendAuto {
		t.Errorf("payload = %+v", payload)
	}
}

func TestConfigShowAppliesTheEnvironment(t *testing.T) {
	h := newHarness(t)
	t.Setenv("GINTRACK_PORT", "9999")

	payload := decode[config.Config](t, h.mustRun("config", "show", "--json"))
	if payload.Server.Port != 9999 {
		t.Errorf("port = %d, want the environment to win", payload.Server.Port)
	}
}

func TestDoctorOnAHealthyWorkspace(t *testing.T) {
	h := newHarness(t)
	h.register()

	stdout := h.mustRun("doctor")
	if !strings.Contains(stdout, "0 errors") {
		t.Errorf("stdout = %q", stdout)
	}
	if !strings.Contains(stdout, "project.yaml valid") {
		t.Errorf("the repository check is missing: %q", stdout)
	}

	payload := decode[doctorPayload](t, h.mustRun("doctor", "--json"))
	if payload.Errors != 0 {
		t.Errorf("errors = %d\n%+v", payload.Errors, payload)
	}
	if len(payload.Repos) != 1 || payload.Repos[0].ID != "acme-api" {
		t.Errorf("repos = %+v", payload.Repos)
	}
}

func TestDoctorReportsDuplicateIDs(t *testing.T) {
	h := newHarness(t)
	h.register()

	original := h.readFile("docs/.pmngr/tasks/DEMO-T-0001-add-address-validation.md")
	clone := filepath.Join(h.Repo, "docs", ".pmngr", "tasks", "DEMO-T-0001-add-address-validation-2.md")
	if err := os.WriteFile(clone, []byte(original), 0o644); err != nil {
		t.Fatalf("write the duplicate: %v", err)
	}

	stdout, _, code := h.run("doctor")
	if code != exitValidation {
		t.Fatalf("exit = %d, want %d\n%s", code, exitValidation, stdout)
	}
	if !strings.Contains(stdout, "duplicate id DEMO-T-0001") {
		t.Errorf("stdout = %q", stdout)
	}
	if !strings.Contains(stdout, "gintrack doctor --renumber") {
		t.Errorf("no fix was suggested: %q", stdout)
	}

	payload := decode[doctorPayload](t, mustJSON(t, h, exitValidation, "doctor", "--json"))
	if len(payload.Repos) != 1 || len(payload.Repos[0].Duplicates) != 1 {
		t.Fatalf("duplicates = %+v", payload.Repos)
	}
	if payload.Repos[0].Duplicates[0].ID != "DEMO-T-0001" {
		t.Errorf("duplicate = %+v", payload.Repos[0].Duplicates[0])
	}
}

func TestDoctorRenumber(t *testing.T) {
	h := newHarness(t)
	h.register()

	original := h.readFile("docs/.pmngr/tasks/DEMO-T-0001-add-address-validation.md")
	clone := filepath.Join(h.Repo, "docs", ".pmngr", "tasks", "DEMO-T-0001-add-address-validation-2.md")
	if err := os.WriteFile(clone, []byte(original), 0o644); err != nil {
		t.Fatalf("write the duplicate: %v", err)
	}

	stdout, stderr, code := h.run("doctor", "--renumber", "--yes")
	if code != exitValidation {
		t.Fatalf("exit = %d, want the duplicate reported before the repair\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "renumbered DEMO-T-0001 -> DEMO-T-0002") {
		t.Errorf("stdout = %q\nstderr = %q", stdout, stderr)
	}
	if _, _, code := h.run("doctor"); code != exitOK {
		t.Errorf("the duplicate survived the renumbering: exit %d", code)
	}
}

func TestDoctorRenumberAsksFirst(t *testing.T) {
	h := newHarness(t)
	h.register()

	original := h.readFile("docs/.pmngr/tasks/DEMO-T-0001-add-address-validation.md")
	clone := filepath.Join(h.Repo, "docs", ".pmngr", "tasks", "DEMO-T-0001-add-address-validation-2.md")
	if err := os.WriteFile(clone, []byte(original), 0o644); err != nil {
		t.Fatalf("write the duplicate: %v", err)
	}

	h.Stdin = strings.NewReader("n\n")
	_, stderr, _ := h.run("doctor", "--renumber")
	if !strings.Contains(stderr, "apply this plan?") {
		t.Errorf("no confirmation was asked: %q", stderr)
	}
	if !strings.Contains(stderr, "skipped DEMO") {
		t.Errorf("the answer was ignored: %q", stderr)
	}
	if _, err := os.Stat(clone); err != nil {
		t.Errorf("a refused renumbering touched the files: %v", err)
	}
}

func TestDoctorFixNormalizesFrontMatter(t *testing.T) {
	h := newHarness(t)
	h.register()

	target := filepath.Join(h.Repo, "docs", ".pmngr", "stories", "DEMO-US-0002-save-payment-methods.md")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	scrambled := strings.Replace(string(data), "id: DEMO-US-0002\ntype: story\n", "type: story\nid: DEMO-US-0002\n", 1)
	if scrambled == string(data) {
		t.Fatal("the fixture changed shape; adjust the test")
	}
	if err := os.WriteFile(target, []byte(scrambled), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	stdout := h.mustRun("doctor", "--fix")
	if !strings.Contains(stdout, "front matter normalized") {
		t.Errorf("stdout = %q", stdout)
	}
	fixed := h.readFile("docs/.pmngr/stories/DEMO-US-0002-save-payment-methods.md")
	if !strings.HasPrefix(fixed, "---\nid: DEMO-US-0002\ntype: story\n") {
		t.Errorf("the key order was not restored:\n%s", fixed[:80])
	}
}

func TestConfigFileIsHonored(t *testing.T) {
	h := newHarness(t)
	other := filepath.Join(t.TempDir(), "elsewhere.yaml")

	if _, _, code := h.run("--config", other, "add", h.Repo); code != exitOK {
		t.Fatal("add with --config failed")
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("--config was ignored: %v", err)
	}
	if _, err := os.Stat(h.Config); err == nil {
		t.Error("the environment path was written to despite --config")
	}
}

func TestUnknownFlagIsAUsageError(t *testing.T) {
	h := newHarness(t)
	if _, _, code := h.run("ls", "--nope"); code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
}

// mustJSON runs a command expected to end with a given exit code and returns
// its stdout, which is where the JSON payload goes even when the check failed.
func mustJSON(t *testing.T, h *harness, want int, args ...string) string {
	t.Helper()
	stdout, stderr, code := h.run(args...)
	if code != want {
		t.Fatalf("%v: exit %d, want %d\n%s\n%s", args, code, want, stdout, stderr)
	}
	return stdout
}

// secondRepo creates another registered repository holding an empty project.
func (h *harness) secondRepo(key string) string {
	h.t.Helper()

	dir := filepath.Join(filepath.Dir(h.Repo), strings.ToLower(key)+"-service")
	backlog := filepath.Join(dir, "docs", ".pmngr")
	if err := os.MkdirAll(backlog, 0o755); err != nil {
		h.t.Fatalf("create %s: %v", backlog, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		h.t.Fatalf("create .git: %v", err)
	}
	body := "schema: 1\nkey: " + key + "\nname: " + key + " service\n" +
		"workflow:\n  initial: todo\n  statuses:\n" +
		"    - { id: todo, name: To Do, category: todo }\n" +
		"    - { id: done, name: Done, category: done, terminal: true }\n" +
		"  transitions:\n    todo: [done]\n"
	if err := os.WriteFile(filepath.Join(backlog, "project.yaml"), []byte(body), 0o644); err != nil {
		h.t.Fatalf("write project.yaml: %v", err)
	}
	return dir
}

func TestTwoRepositoriesAreQueriedTogether(t *testing.T) {
	h := newHarness(t)
	h.register()
	second := h.secondRepo("OPS")
	h.mustRun("add", second)

	payload := decode[lsPayload](t, h.mustRun("ls", "--json"))
	if len(payload.Repos) != 2 {
		t.Fatalf("repos = %+v", payload.Repos)
	}

	items := decode[itemListPayload](t, h.mustRun("item", "list", "--json"))
	if items.Total != 5 {
		t.Errorf("total = %d, want the five items of the only populated repository", items.Total)
	}
	first := items.Items[0].(map[string]any)
	if first["repo"] != "acme-api" {
		t.Errorf("repo = %v", first["repo"])
	}

	// With two projects registered, a create has to say which one it means.
	if _, _, code := h.run("item", "new", "--type", "task", "--title", "Ambiguous"); code != exitUsage {
		t.Errorf("exit = %d, want %d without --project", code, exitUsage)
	}
	if _, _, code := h.run("item", "new", "--type", "task", "--title", "Explicit", "--project", "OPS"); code != exitOK {
		t.Errorf("--project was refused: exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(second, "docs/.pmngr/tasks/OPS-T-0001-explicit.md")); err != nil {
		t.Errorf("the item did not land in the second repository: %v", err)
	}
}

func TestWorkspaceFlagSelectsTheRepositories(t *testing.T) {
	h := newHarness(t)
	h.register()
	second := h.secondRepo("OPS")
	if _, _, code := h.run("--workspace", "oss", "add", second); code != exitOK {
		t.Fatal("add into a new workspace failed")
	}

	cfg, err := config.Load(h.Config)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := cfg.WorkspaceNamed("oss"); !ok {
		t.Errorf("the workspace was not created: %+v", cfg.Workspaces)
	}
	if cfg.DefaultWorkspace != "oss" {
		t.Errorf("defaultWorkspace = %q", cfg.DefaultWorkspace)
	}
}

func TestDoctorWarnsAboutLoosePermissions(t *testing.T) {
	h := newHarness(t)
	h.register()
	if err := os.Chmod(h.Config, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	stdout, _, code := h.run("doctor")
	if code != exitOK {
		t.Fatalf("exit = %d, want a warning rather than an error\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "should not be readable by other users") {
		t.Errorf("stdout = %q", stdout)
	}
	if _, _, code := h.run("doctor", "--strict"); code != exitValidation {
		t.Errorf("--strict: exit %d, want %d", code, exitValidation)
	}
}
