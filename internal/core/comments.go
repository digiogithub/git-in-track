package core

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

// CommentTimestampLayout is the compact UTC form the comment file names use
// (docs/03 section 11.1).
const CommentTimestampLayout = "20060102T150405Z"

// commentAuthorFallback is the handle used when a draft carries none. A comment
// file name must always have an author part.
const commentAuthorFallback = "unknown"

// CommentDraft is the input of Store.AddComment. The path, the file name and the
// created timestamp are derived by the store; a caller may pin Created to
// reproduce a fixture.
type CommentDraft struct {
	Author      string              `json:"author"`
	Body        string              `json:"body"`
	InReplyTo   string              `json:"inReplyTo,omitempty"`
	Kind        CommentKind         `json:"kind,omitempty"`
	Reactions   map[string][]string `json:"reactions,omitempty"`
	Attachments []string            `json:"attachments,omitempty"`
	Created     Timestamp           `json:"created,omitempty"`
}

// AddComment appends one comment to an item as a new file under
// .pmngr/comments/<ITEM-ID>/, named "<YYYYMMDDTHHMMSSZ>-<author>.md". One file
// per comment is what keeps concurrent replies from ever conflicting in git
// (docs/03 section 11).
func (s *FileStore) AddComment(ctx context.Context, id ItemID, c CommentDraft) (*Comment, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContext("add comment", err)
	}
	item, err := s.locate(id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(c.Body) == "" {
		return nil, errors.New("add comment: body is empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	created := c.Created
	if created.IsZero() {
		created = s.now()
	}
	author := SanitizeHandle(c.Author)
	kind := c.Kind
	if kind == "" {
		kind = CommentKindComment
	}
	if !kind.Valid() {
		return nil, fmt.Errorf("add comment: unknown kind %q", kind)
	}

	dir := s.CommentsDir(item.ID)
	name, err := s.freeCommentName(dir, created, author)
	if err != nil {
		return nil, err
	}
	comment := &Comment{
		Item:        item.ID,
		Author:      author,
		Created:     created,
		InReplyTo:   c.InReplyTo,
		Kind:        kind,
		Reactions:   c.Reactions,
		Attachments: c.Attachments,
		Body:        strings.Trim(c.Body, "\n"),
		Path:        path.Join(dir, name),
	}
	data, err := SerializeComment(comment)
	if err != nil {
		return nil, err
	}
	if err := s.fs.MkdirAll(dir); err != nil {
		return nil, fmt.Errorf("add comment: %w", err)
	}
	if err := writeFileAtomic(s.fs, comment.Path, data); err != nil {
		return nil, err
	}
	comment.Rev = ComputeRev(data)
	return comment, nil
}

// ListComments returns the comments of an item, oldest first. Readers sort by
// the created field and use the file name only as a tie-break (R-CMT-3).
func (s *FileStore) ListComments(ctx context.Context, id ItemID) ([]Comment, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContext("list comments", err)
	}
	resolved := id
	if item, err := s.locate(id); err == nil {
		resolved = item.ID
	} else if !errors.Is(err, ErrItemNotFound) {
		return nil, err
	}

	dir := s.CommentsDir(resolved)
	entries, err := s.fs.ReadDir(dir)
	if err != nil {
		if errors.Is(err, ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("list comments %s: %w", id, err)
	}
	out := make([]Comment, 0, len(entries))
	for _, e := range entries {
		if e.IsDir || !isItemFileName(e.Name) {
			continue
		}
		p := path.Join(dir, e.Name)
		data, err := s.fs.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		c, err := ParseComment(p, data)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Created.Equal(out[j].Created.Time) {
			return out[i].Created.Before(out[j].Created.Time)
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

// CommentsDir returns the folder holding the comments of an item.
func (s *FileStore) CommentsDir(id ItemID) string {
	return path.Join(s.backlog, CommentsDirName, string(id))
}

// freeCommentName returns a file name that is not taken yet. Two comments by the
// same author in the same second get the suffixes -2, -3, … (R-CMT-1).
func (s *FileStore) freeCommentName(dir string, created Timestamp, author string) (string, error) {
	stem := created.UTC().Format(CommentTimestampLayout) + "-" + author
	for i := 1; i < 1000; i++ {
		name := stem + ".md"
		if i > 1 {
			name = stem + "-" + strconv.Itoa(i) + ".md"
		}
		switch _, err := s.fs.Stat(path.Join(dir, name)); {
		case errors.Is(err, ErrNotExist):
			return name, nil
		case err != nil:
			return "", fmt.Errorf("add comment: %w", err)
		}
	}
	return "", fmt.Errorf("add comment: no free name for %s in %s", stem, dir)
}

// SanitizeHandle reduces a person's name to the handle grammar used in comment
// file names: lowercase [a-z0-9-]+ (docs/03 sections 3.7 and 11.1).
func SanitizeHandle(handle string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(handle)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		default:
			if folded, ok := latinFolding[r]; ok {
				if dash && b.Len() > 0 {
					b.WriteByte('-')
				}
				dash = false
				b.WriteString(folded)
				continue
			}
			dash = b.Len() > 0
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return commentAuthorFallback
	}
	return out
}

// CommentRef builds the "<ITEM-ID>#<file-stem>" reference of a comment file
// (R-ID-4).
func CommentRef(item ItemID, fileName string) string {
	stem := strings.TrimSuffix(path.Base(fileName), ".md")
	return string(item) + "#" + stem
}
