package mapping

import (
	"fmt"
	"sort"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/youtrack"
)

// SubtaskLinkType is the name of the hierarchy-forming link type on a stock
// YouTrack. It is also the type whose LinkType.Aggregation flag is true, and
// MapLinks accepts either signal so a renamed aggregation type still forms the
// hierarchy.
const SubtaskLinkType = "Subtask"

// linkKinds maps a YouTrack link-type name and direction onto one of the five
// kinds core.LinkKind accepts. YouTrack's stock directed types are "Depend"
// ("depends on" / "is required for") and "Duplicate" ("duplicates" /
// "is duplicated by"); "Relates" is undirected and comes back as BOTH.
//
// The outward half of a link is the relation as seen from the source issue.
// For "Depend", YouTrack's sourceToTarget reads "is required for", so the
// outward half is blocks and the inward half is blocked_by.
func linkKind(typeName, direction string) (core.LinkKind, bool) {
	switch normalizeKey(typeName) {
	case "relates", "relates to", "related":
		return core.LinkRelatesTo, true
	case "depend", "depends", "dependency":
		if direction == youtrack.DirectionInward {
			return core.LinkBlockedBy, true
		}
		return core.LinkBlocks, true
	case "duplicate", "duplicates":
		if direction == youtrack.DirectionInward {
			return core.LinkDuplicatedBy, true
		}
		return core.LinkDuplicates, true
	default:
		return "", false
	}
}

// MapLinks reads the hierarchy and the typed relations out of an issue's
// links.
//
// YouTrack returns one entry per (linkType, direction) pair, including the
// pairs that hold no issues: an issue with a single "relates to" link comes
// back with roughly twenty entries, nineteen of them with an empty "issues"
// array. Every one of those is filtered out before anything else happens.
//
// The "Subtask" link type — or any type YouTrack marks as an aggregation — is
// the hierarchy: its outward half names this issue's children, its inward half
// names its parent. An issue with more than one inward Subtask link keeps the
// first and warns, because a git-in-track item has exactly one parent.
//
// Every other link type maps onto core.LinkKind, and a type this function does
// not know produces a warning and no link. It never invents a kind
// core.LinkKind.Valid rejects.
//
// The returned parent, children and link targets are all YouTrack idReadable
// values, not git-in-track item ids; see Relations.
func MapLinks(issue youtrack.Issue) (parent string, children []string, links []core.Link, warnings []Warning) {
	seenChild := make(map[string]bool)
	seenLink := make(map[core.Link]bool)
	for _, link := range youtrack.NonEmptyLinks(issue.Links) {
		name := link.LinkType.Name
		if isHierarchyLink(link) {
			p, c, w := mapHierarchyLink(link, parent, seenChild)
			if p != "" {
				parent = p
			}
			children = append(children, c...)
			warnings = append(warnings, w...)
			continue
		}
		kind, ok := linkKind(name, link.Direction)
		if !ok {
			warnings = append(warnings, Warning{
				Field:  "links",
				Value:  name,
				Reason: fmt.Sprintf("the link type %q has no equivalent among the five git-in-track link kinds and was not imported", name),
			})
			continue
		}
		if !kind.Valid() {
			// Unreachable with the table above, and cheap insurance against a
			// future entry that is not one of the five.
			warnings = append(warnings, Warning{
				Field:  "links",
				Value:  name,
				Reason: fmt.Sprintf("the link type %q mapped to the unknown kind %q and was not imported", name, kind),
			})
			continue
		}
		for _, ref := range link.Issues {
			target := strings.TrimSpace(ref.IDReadable)
			if target == "" || target == strings.TrimSpace(issue.IDReadable) {
				continue
			}
			l := core.Link{Kind: kind, Target: target}
			if seenLink[l] {
				continue
			}
			seenLink[l] = true
			links = append(links, l)
		}
	}
	sort.SliceStable(links, func(i, j int) bool {
		if links[i].Kind != links[j].Kind {
			return links[i].Kind < links[j].Kind
		}
		return links[i].Target < links[j].Target
	})
	sort.Strings(children)
	return parent, children, links, sortWarnings(warnings)
}

// isHierarchyLink reports whether a link entry forms the parent/child
// hierarchy: the stock "Subtask" type, or whatever type the instance marks as
// an aggregation.
func isHierarchyLink(link youtrack.IssueLink) bool {
	return link.LinkType.Aggregation || normalizeKey(link.LinkType.Name) == normalizeKey(SubtaskLinkType)
}

// mapHierarchyLink reads one Subtask entry. Outward names children, inward
// names the parent; a BOTH direction on an aggregation type is nonsense and is
// reported rather than guessed at.
func mapHierarchyLink(link youtrack.IssueLink, parent string, seen map[string]bool) (newParent string, children []string, warnings []Warning) {
	switch link.Direction {
	case youtrack.DirectionOutward:
		for _, ref := range link.Issues {
			id := strings.TrimSpace(ref.IDReadable)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			children = append(children, id)
		}
	case youtrack.DirectionInward:
		for _, ref := range link.Issues {
			id := strings.TrimSpace(ref.IDReadable)
			if id == "" {
				continue
			}
			if parent != "" && parent != id {
				warnings = append(warnings, Warning{
					Field:    "links",
					Value:    id,
					Fallback: parent,
					Reason:   fmt.Sprintf("the issue has more than one parent in YouTrack; %q was kept and %q ignored", parent, id),
				})
				continue
			}
			parent = id
		}
	default:
		warnings = append(warnings, Warning{
			Field:  "links",
			Value:  link.LinkType.Name,
			Reason: fmt.Sprintf("the hierarchy link type %q came back with direction %q, which names neither a parent nor a child, and was skipped", link.LinkType.Name, link.Direction),
		})
	}
	return parent, children, warnings
}
