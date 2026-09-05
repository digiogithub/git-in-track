package core

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// Sentinel errors of project scaffolding.
var (
	// ErrProjectExists reports a documentation folder that already holds a
	// project.yaml. Scaffolding never overwrites a backlog: a repository that
	// already has one is opened, not created.
	ErrProjectExists = errors.New("the folder already holds a project")
	// ErrProjectKey reports a key that does not match [A-Z][A-Z0-9]{1,9}.
	ErrProjectKey = errors.New("invalid project key")
)

// gitignoreFileName is the ignore file written inside a fresh backlog so that
// the derived index.json of R-LOC-5 never reaches a commit.
const gitignoreFileName = ".gitignore"

// backlogGitignore is the snippet of R-LOC-5: index.json is derived data and can
// be rebuilt from the files at any time.
const backlogGitignore = "# Derived index, rebuilt from the files (docs/03 R-LOC-5).\n" +
	indexFileName + "\n"

// NewProject is the input of CreateProject: everything the scaffolder needs
// that is not a workflow default.
type NewProject struct {
	// Key is the ID prefix, matching [A-Z][A-Z0-9]{1,9}.
	Key ProjectKey
	// Name is the human name shown in project pickers. It defaults to the key.
	Name string
	// Description is one optional paragraph.
	Description string
	// Timezone is an IANA name used to present date-only fields. It defaults to
	// UTC.
	Timezone string
}

// DefaultWorkflow returns the status machine a new project starts with: the six
// statuses of docs/03 section 6.2, with `backlog` as the initial one.
//
// It is deliberately the same list the shipped project.yaml of this repository
// uses, so that a project created by the tool and a project written by hand
// from the documentation are the same file.
func DefaultWorkflow() Workflow {
	return Workflow{
		Initial: "backlog",
		Statuses: []StatusDef{
			{ID: "backlog", Name: "Backlog", Category: CategoryTodo},
			{ID: "todo", Name: "To Do", Category: CategoryTodo},
			{ID: "in_progress", Name: "In Progress", Category: CategoryInProgress},
			{ID: "in_review", Name: "In Review", Category: CategoryInProgress},
			{ID: "done", Name: "Done", Category: CategoryDone, Terminal: true},
			{ID: "cancelled", Name: "Cancelled", Category: CategoryCancelled, Terminal: true},
		},
	}
}

// NewProjectConfig returns the configuration a fresh project starts with: every
// documented default, the default workflow, and the identity spec carries.
func NewProjectConfig(spec NewProject, docsPath string) ProjectConfig {
	cfg := DefaultProjectConfig()
	cfg.Key = spec.Key
	cfg.Name = strings.TrimSpace(spec.Name)
	if cfg.Name == "" {
		cfg.Name = string(spec.Key)
	}
	cfg.Description = strings.TrimSpace(spec.Description)
	if tz := strings.TrimSpace(spec.Timezone); tz != "" {
		cfg.Timezone = tz
	}
	cfg.Workflow = DefaultWorkflow()
	if clean := path.Clean(docsPath); clean != "." {
		// `docs.path` is informational (docs/03 section 6.1): the real path is
		// where the file was found. It is written so that a human reading the
		// file knows what the project was created against.
		cfg.Docs.Path = clean
	}
	return cfg
}

// marshalProjectConfig encodes a project.yaml with the two-space indentation
// every other YAML emitter of the core uses, so a file written by the tool and
// a file written by hand from docs/03 section 6.2 diff as one block.
func marshalProjectConfig(cfg ProjectConfig) ([]byte, error) {
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cfg); err != nil {
		return nil, fmt.Errorf("create project: encode %s: %w", ProjectFileName, err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("create project: encode %s: %w", ProjectFileName, err)
	}
	return []byte(buf.String()), nil
}

// CreateProject writes a new backlog under docsPath: the project.yaml of
// docs/03 section 6, the four item folders plus comments/ and attachments/ of
// section 2, and the .gitignore of R-LOC-5.
//
// It is pure core: it touches nothing but the FS the caller supplies, so the
// browser creates a project through the very same code the CLI runs.
//
// It fails with ErrProjectKey on a key the grammar refuses and with
// ErrProjectExists when the folder already holds a project.yaml.
func CreateProject(fs FS, docsPath string, spec NewProject) (ProjectRef, error) {
	if fs == nil {
		return ProjectRef{}, errors.New("create project: nil file system")
	}
	if !ValidProjectKey(spec.Key) {
		return ProjectRef{}, fmt.Errorf("create project: %w: %q does not match [A-Z][A-Z0-9]{1,9}",
			ErrProjectKey, spec.Key)
	}
	docs := path.Clean(docsPath)
	if docs == "" {
		docs = "."
	}
	if docs == ".." || strings.HasPrefix(docs, "../") || path.IsAbs(docs) {
		return ProjectRef{}, fmt.Errorf("create project: %q is outside the repository", docsPath)
	}

	backlog := joinPath(docs, BacklogDirName)
	configPath := joinPath(backlog, ProjectFileName)
	if _, err := fs.Stat(configPath); err == nil {
		return ProjectRef{}, fmt.Errorf("create project: %w: %s", ErrProjectExists, configPath)
	} else if !errors.Is(err, ErrNotExist) {
		return ProjectRef{}, fmt.Errorf("create project: stat %s: %w", configPath, err)
	}

	cfg := NewProjectConfig(spec, docs)
	data, err := marshalProjectConfig(cfg)
	if err != nil {
		return ProjectRef{}, err
	}

	// The item folders are created lazily by the data model (R-LOC-3), but a
	// fresh backlog that shows them is a fresh backlog a human can drop a file
	// into without guessing the names.
	folders := []string{backlog}
	for _, f := range itemFolders {
		folders = append(folders, joinPath(backlog, f.Dir))
	}
	folders = append(folders, joinPath(backlog, commentsDirName), joinPath(backlog, attachmentsDirName))
	for _, dir := range folders {
		if err := fs.MkdirAll(dir); err != nil {
			return ProjectRef{}, fmt.Errorf("create project: make %s: %w", dir, err)
		}
	}
	if err := fs.WriteFile(joinPath(backlog, gitignoreFileName), []byte(backlogGitignore)); err != nil {
		return ProjectRef{}, fmt.Errorf("create project: write %s: %w", gitignoreFileName, err)
	}
	if err := fs.WriteFile(configPath, data); err != nil {
		return ProjectRef{}, fmt.Errorf("create project: write %s: %w", configPath, err)
	}

	return ProjectRef{
		Key:         cfg.Key,
		Name:        cfg.Name,
		DocsPath:    docs,
		BacklogPath: backlog,
		ConfigPath:  configPath,
		Config:      &cfg,
	}, nil
}
