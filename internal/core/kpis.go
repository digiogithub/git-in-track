package core

// This file computes the at-a-glance figures the workspace home shows for every
// project (GIT-US-0205, docs/05-web-app.md section 3.1.2). They are derived
// from the index and never stored, so the companion and browser-only mode —
// both of which answer them through the vault method "project.kpis" — agree.

// KPICount is a total and how many of it are still open: neither in a done
// nor in a cancelled category.
type KPICount struct {
	Total int `json:"total"`
	Open  int `json:"open"`
}

// StoryKPIs counts the stories of a project by status category.
type StoryKPIs struct {
	Total     int `json:"total"`
	Open      int `json:"open"`
	Done      int `json:"done"`
	Cancelled int `json:"cancelled"`
	// Estimated is how many non-cancelled stories carry an estimate.
	Estimated int `json:"estimated"`
}

// SpecKPIs counts the specs of a project and those that still hold a
// requirement nobody has verified on its current text.
type SpecKPIs struct {
	Total int `json:"total"`
	// Unverified is the number of specs with at least one unverified
	// requirement.
	Unverified   int `json:"unverified"`
	Requirements int `json:"requirements"`
	// UnverifiedRequirements are the requirements, of a spec that is not
	// cancelled, that are not cancelled themselves and carry no `verified`
	// stamp whose rev is their current block rev (R-REQ-11a, §21.5).
	UnverifiedRequirements int `json:"unverifiedRequirements"`
}

// KPIProgress is the estimated share of a project that is closed: done stories
// over non-cancelled stories. When at least one of those stories carries an
// estimate the ratio is weighted by it, and a story without one weighs the
// mean estimate of the stories that have one.
type KPIProgress struct {
	// Ratio is Done / Scope, in [0, 1]; 0 when Scope is 0.
	Ratio float64 `json:"ratio"`
	// Weighted reports whether Done and Scope are story points rather than
	// story counts.
	Weighted bool    `json:"weighted"`
	Done     float64 `json:"done"`
	Scope    float64 `json:"scope"`
}

// ProjectKPIs are the workspace-home figures of one project. Soft-deleted
// items and items in the triage category (the inbox) are left out of every
// count: neither is part of the backlog.
type ProjectKPIs struct {
	Project    ProjectKey  `json:"project"`
	KBPages    int         `json:"kbPages"`
	Milestones KPICount    `json:"milestones"`
	Epics      KPICount    `json:"epics"`
	Stories    StoryKPIs   `json:"stories"`
	Specs      SpecKPIs    `json:"specs"`
	Progress   KPIProgress `json:"progress"`
}

// ProjectKPIs computes the KPIs of one project of the index. The second result
// is false when the index holds no such project.
func (ix *Index) ProjectKPIs(key ProjectKey) (ProjectKPIs, bool) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()

	var cfg *ProjectConfig
	found := false
	for _, p := range ix.projects {
		if p.Key == key && !p.Team {
			cfg, found = p.Config, true
			break
		}
	}
	if !found {
		return ProjectKPIs{}, false
	}
	out := ProjectKPIs{Project: key}

	for _, page := range ix.pagesByPath {
		if page.Project == key {
			out.KBPages++
		}
	}

	var stories []storyWeight
	for _, id := range sortedIDs(ix.byID) {
		it := ix.byID[id]
		if it.Deleted || ix.projectOf(it) != key {
			continue
		}
		category := ix.categoryOf(it)
		if category == CategoryTriage {
			continue
		}
		closed := category == CategoryDone || category == CategoryCancelled
		switch it.Type {
		case TypeMilestone:
			out.Milestones.Total++
			if !closed {
				out.Milestones.Open++
			}
		case TypeEpic:
			out.Epics.Total++
			if !closed {
				out.Epics.Open++
			}
		case TypeStory:
			out.Stories.Total++
			switch category {
			case CategoryDone:
				out.Stories.Done++
			case CategoryCancelled:
				out.Stories.Cancelled++
			default:
				out.Stories.Open++
			}
			if category != CategoryCancelled {
				var estimate *float64
				if it.Estimate != nil && *it.Estimate > 0 {
					estimate = it.Estimate
					out.Stories.Estimated++
				}
				stories = append(stories, storyWeight{done: category == CategoryDone, estimate: estimate})
			}
		case TypeSpec:
			out.Specs.Total++
			if category == CategoryCancelled {
				continue
			}
			views, err := SpecRequirements(it, cfg)
			if err != nil {
				continue
			}
			pending := 0
			for _, v := range views {
				out.Specs.Requirements++
				if cfg != nil && cfg.CategoryOf(v.Status) == CategoryCancelled {
					continue
				}
				if v.Verified == nil || v.Verified.Rev == "" || v.Verified.Rev != v.BlockRev {
					pending++
				}
			}
			out.Specs.UnverifiedRequirements += pending
			if pending > 0 {
				out.Specs.Unverified++
			}
		}
	}

	out.Progress = storyProgress(stories, out.Stories.Estimated)
	return out, true
}

// storyWeight is one non-cancelled story as the progress ratio reads it.
type storyWeight struct {
	done     bool
	estimate *float64
}

// storyProgress folds the non-cancelled stories into a KPIProgress: by count,
// or by estimate when estimated > 0, an unestimated story weighing the mean.
func storyProgress(stories []storyWeight, estimated int) KPIProgress {
	var p KPIProgress
	mean := 0.0
	if estimated > 0 {
		p.Weighted = true
		sum := 0.0
		for _, s := range stories {
			if s.estimate != nil {
				sum += *s.estimate
			}
		}
		mean = sum / float64(estimated)
	}
	for _, s := range stories {
		weight := 1.0
		if p.Weighted {
			weight = mean
			if s.estimate != nil {
				weight = *s.estimate
			}
		}
		p.Scope += weight
		if s.done {
			p.Done += weight
		}
	}
	if p.Scope > 0 {
		p.Ratio = p.Done / p.Scope
	}
	return p
}
