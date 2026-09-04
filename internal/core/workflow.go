package core

import "fmt"

// TransitionAllowed reports whether moving an item from one status to another is
// permitted by the workflow declared in project.yaml. It is the boolean form of
// ValidateTransition.
func TransitionAllowed(cfg *ProjectConfig, from, to Status) bool {
	return ValidateTransition(cfg, from, to).Code == ""
}

// ValidateTransition checks a status change against workflow.transitions
// (docs/03 section 6.1). It returns the zero Diagnostic — the one whose Code is
// empty — when the transition is allowed.
//
// An undeclared target status is the error E-STATUS-UNKNOWN. A transition the
// workflow does not list is the warning W-WORKFLOW-TRANSITION: a git repository
// may receive a file from anywhere, so files are only flagged. The API, the MCP
// server and the CLI escalate that warning to a refusal on write unless the
// caller forces the change.
//
// An empty from means "the item is being created": only the existence of the
// target status is checked. An absent or empty transitions mapping means every
// transition is allowed.
func ValidateTransition(cfg *ProjectConfig, from, to Status) Diagnostic {
	transition := Diagnostic{Path: ProjectFileName, Field: "status"}
	if to == "" {
		transition.Code = CodeFieldRequired
		transition.Severity = SeverityError
		transition.Message = "a transition needs a target status"
		return transition
	}
	if cfg == nil || len(cfg.Workflow.Statuses) == 0 {
		return Diagnostic{}
	}
	if _, ok := cfg.StatusDef(to); !ok {
		transition.Code = CodeStatusUnknown
		transition.Severity = SeverityError
		transition.Message = fmt.Sprintf("%q is not declared in the workflow", to)
		return transition
	}
	if from == "" || from == to {
		return Diagnostic{}
	}
	if _, ok := cfg.StatusDef(from); !ok {
		transition.Code = CodeStatusUnknown
		transition.Severity = SeverityError
		transition.Message = fmt.Sprintf("%q is not declared in the workflow", from)
		return transition
	}
	if len(cfg.Workflow.Transitions) == 0 {
		return Diagnostic{}
	}
	for _, candidate := range cfg.Workflow.Transitions[from] {
		if candidate == to {
			return Diagnostic{}
		}
	}
	transition.Code = CodeWarnWorkflowTransition
	transition.Severity = SeverityWarning
	transition.Message = fmt.Sprintf("the workflow does not declare the transition %q -> %q", from, to)
	return transition
}

// statusReachable reports whether a status can be reached from the initial status
// by following the declared transitions. A workflow without transitions declares
// nothing, so every status is reachable.
func statusReachable(cfg *ProjectConfig, target Status) bool {
	if cfg == nil || len(cfg.Workflow.Transitions) == 0 {
		return true
	}
	initial := cfg.InitialStatus()
	if initial == "" || initial == target {
		return true
	}
	seen := map[Status]bool{initial: true}
	queue := []Status{initial}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range cfg.Workflow.Transitions[current] {
			if next == target {
				return true
			}
			if seen[next] {
				continue
			}
			seen[next] = true
			queue = append(queue, next)
		}
	}
	return false
}
