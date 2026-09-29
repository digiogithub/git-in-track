package config

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// ErrRepoNotInFile means the configuration file registers no repository with
// the requested id, so there is no `semanticSearch` key to write.
var ErrRepoNotInFile = errors.New("the repository is not registered in the configuration file")

// ErrChangedOnDisk means the configuration file was modified by another writer
// between the read and the write of an edit. Nothing was written.
var ErrChangedOnDisk = errors.New("the configuration file changed while it was being edited")

// semanticMu serializes the read-modify-write of SetSemanticSearch inside this
// process; the modification-time guard covers other processes.
var semanticMu sync.Mutex

// afterSemanticLoad is a test seam called between the read and the guard
// check of SetSemanticSearch, where another writer could get in.
var afterSemanticLoad = func() {}

// SetSemanticSearch writes `repos[].semanticSearch` of the repository with the
// given id and leaves every other key as the loader read it (ADR-039). It is
// guarded: the file's size and modification time are compared just before the
// atomic replace, and a file that moved underneath the edit is refused with
// [ErrChangedOnDisk] rather than overwritten.
func SetSemanticSearch(path, repoID string, enabled bool) error {
	if path == "" {
		return errors.New("save configuration: empty path")
	}
	semanticMu.Lock()
	defer semanticMu.Unlock()

	before, err := stamp(path)
	if err != nil {
		return err
	}
	cfg, err := Load(path)
	if err != nil {
		return err
	}
	afterSemanticLoad()
	found := false
	for i := range cfg.Repos {
		if cfg.Repos[i].ID == repoID {
			cfg.Repos[i].SemanticSearch = enabled
			found = true
		}
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrRepoNotInFile, repoID)
	}
	after, err := stamp(path)
	if err != nil {
		return err
	}
	if before != after {
		return fmt.Errorf("%w: %s", ErrChangedOnDisk, path)
	}
	return Save(path, cfg)
}

type fileStamp struct {
	exists bool
	size   int64
	mtime  time.Time
}

func stamp(path string) (fileStamp, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileStamp{}, nil
	}
	if err != nil {
		return fileStamp{}, fmt.Errorf("stat %s: %w", path, err)
	}
	return fileStamp{exists: true, size: info.Size(), mtime: info.ModTime()}, nil
}
