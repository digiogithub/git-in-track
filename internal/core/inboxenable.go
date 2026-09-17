package core

import (
	"bytes"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Enabling the inbox of a project created before the inbox existed (story
// GIT-US-0100, ADR-033). A project has an inbox when its workflow declares a
// status of category `triage`; enabling one adds exactly that status, first in
// the list, and nothing else.

// Sentinel errors of EnableInbox.
var (
	// ErrInboxEnabled reports a workflow that already declares a triage status:
	// the project has an inbox and there is nothing to add.
	ErrInboxEnabled = errors.New("the workflow already declares a triage status")
	// ErrTriageIDTaken reports a workflow with a status called `triage` whose
	// category is not triage. Adding a second one would break R-PROJ status
	// uniqueness, and recategorizing the existing one would silently move every
	// item in it into the inbox.
	ErrTriageIDTaken = errors.New("a status with id triage already exists and is not a triage status")
)

// TriageStatusDef is the status EnableInbox adds, and the one DefaultWorkflow
// starts with.
func TriageStatusDef() StatusDef {
	return StatusDef{ID: "triage", Name: "Triage", Category: CategoryTriage}
}

// EnableInbox returns project.yaml with the inbox status of TriageStatusDef added
// as the first workflow status. `initial` and `transitions` are left alone:
// triage is never a transition target, work leaves it by being accepted.
//
// The edit is made on the text itself whenever the status list is an ordinary
// block sequence, so that every other byte of the file — comments, key order,
// quoting, folded scalars — survives unchanged. Any other shape (a flow
// sequence, an empty list, no list at all) falls back to editing the YAML node
// tree, which still keeps comments and key order.
func EnableInbox(data []byte) ([]byte, error) {
	var cfg ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", ProjectFileName, err)
	}
	for _, s := range cfg.Workflow.Statuses {
		if s.Category == CategoryTriage {
			return nil, ErrInboxEnabled
		}
	}
	status := TriageStatusDef()
	for _, s := range cfg.Workflow.Statuses {
		if s.ID == status.ID {
			return nil, ErrTriageIDTaken
		}
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", ProjectFileName, err)
	}
	root := documentMapping(&doc)
	if root == nil {
		return nil, fmt.Errorf("parse %s: not a mapping", ProjectFileName)
	}
	if out, ok := insertStatusLine(data, root, status); ok {
		return out, nil
	}
	return insertStatusNode(&doc, root, status)
}

// insertStatusLine inserts the status as a new line above the first entry of a
// block `workflow.statuses` sequence, in the style that entry is written in. It
// reports false when the list has no such entry or the result does not parse
// back to the expected workflow, and the caller then edits the node tree.
func insertStatusLine(data []byte, root *yaml.Node, status StatusDef) ([]byte, bool) {
	workflow, ok := yamlMapGet(root, "workflow")
	if !ok || workflow.Kind != yaml.MappingNode {
		return nil, false
	}
	statuses, ok := yamlMapGet(workflow, "statuses")
	if !ok || statuses.Kind != yaml.SequenceNode || statuses.Style&yaml.FlowStyle != 0 ||
		len(statuses.Content) == 0 {
		return nil, false
	}
	first := statuses.Content[0]
	lines := bytes.SplitAfter(data, []byte("\n"))
	if first.Line < 1 || first.Line > len(lines) {
		return nil, false
	}
	line := lines[first.Line-1]
	dash := bytes.IndexByte(line, '-')
	if dash < 0 || len(bytes.TrimLeft(line[:dash], " ")) != 0 {
		return nil, false
	}
	indent := string(line[:dash])
	eol := "\n"
	if bytes.HasSuffix(line, []byte("\r\n")) {
		eol = "\r\n"
	}

	var entry string
	if first.Kind == yaml.MappingNode && first.Style&yaml.FlowStyle != 0 {
		entry = fmt.Sprintf("%s- {id: %s, name: %s, category: %s}%s",
			indent, status.ID, status.Name, status.Category, eol)
	} else {
		// A block entry keeps its keys aligned under the first one.
		pad := indent + "  "
		if first.Column-1 > dash {
			pad = string(bytes.Repeat([]byte(" "), first.Column-1))
		}
		entry = fmt.Sprintf("%s- id: %s%s%sname: %s%s%scategory: %s%s",
			indent, status.ID, eol, pad, status.Name, eol, pad, status.Category, eol)
	}

	var out bytes.Buffer
	for i, l := range lines {
		if i == first.Line-1 {
			out.WriteString(entry)
		}
		out.Write(l)
	}
	if !insertedFirst(out.Bytes(), status, len(statuses.Content)) {
		return nil, false
	}
	return out.Bytes(), true
}

// insertStatusNode is the fallback of EnableInbox: it prepends the status to
// the node tree, creating `workflow.statuses` when it is absent, and re-encodes
// the document.
func insertStatusNode(doc, root *yaml.Node, status StatusDef) ([]byte, error) {
	workflow, err := yamlEnsureMapping(root, "workflow")
	if err != nil {
		return nil, err
	}
	statuses, ok := yamlMapGet(workflow, "statuses")
	if !ok || statuses.Tag == "!!null" || (statuses.Kind == yaml.ScalarNode && statuses.Value == "") {
		statuses = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		yamlMapSet(workflow, "statuses", statuses)
	}
	if statuses.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("enable inbox: workflow.statuses is not a list")
	}
	entry := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Style: yaml.FlowStyle}
	yamlMapSet(entry, "id", yamlScalar(string(status.ID)))
	yamlMapSet(entry, "name", yamlScalar(status.Name))
	yamlMapSet(entry, "category", yamlScalar(string(status.Category)))
	statuses.Content = append([]*yaml.Node{entry}, statuses.Content...)
	return encodeYAMLNode(doc)
}

// insertedFirst reports whether data parses to a workflow whose first status is
// the one just inserted, followed by the previous ones.
func insertedFirst(data []byte, status StatusDef, previous int) bool {
	var cfg ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return false
	}
	got := cfg.Workflow.Statuses
	return len(got) == previous+1 && got[0] == status
}
