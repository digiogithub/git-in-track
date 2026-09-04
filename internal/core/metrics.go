package core

import (
	"math"
	"sort"
	"time"
)

// This file computes the delivery metrics of a sprint: the burndown, the
// cumulative flow diagram, and the cycle time, lead time and throughput that go
// with them (docs/04 section 12, story GIT-US-0028).
//
// It is pure, like boardview.go and sprintview.go: the adapters gather the
// history, this decides what the charts say, and the same numbers therefore
// come out in the browser and in the companion process.
//
// Nothing here is stored. A metric is a function of the item files as they were
// at a moment in time, and the only question the product had to answer is where
// "as they were" comes from. The answer is ItemHistory: a list of observations
// somebody else reconstructed — from the git history of the item files in the
// companion (ADR-017), or approximated from their `updated` stamps where no git
// history can be read. MetricsProvenance travels with every result so that the
// UI can say which of the two it is showing, and from when.

// MetricsSource names where the observations of a metric came from.
type MetricsSource string

// The three sources a metric can be reconstructed from, best first.
const (
	// MetricsSourceGit is the real thing: every revision of every item file in
	// the window, read from the git history of its repository. It is exact.
	MetricsSourceGit MetricsSource = "git"
	// MetricsSourceUpdated is the approximation available where no history can
	// be read: each item is assumed to have held its current status since its
	// `updated` stamp, and to have been unknown before it. It can place the
	// last transition of an item and nothing else.
	MetricsSourceUpdated MetricsSource = "updated"
	// MetricsSourceNone is no history at all: only today is known.
	MetricsSourceNone MetricsSource = "none"
)

// MetricsProvenance is the honesty half of a metric: where its observations
// came from, how far back they reach, and what the resulting numbers may not be
// asked. It is carried by every result and shown by the UI, because a burndown
// whose source is unstated is a burndown nobody can act on.
type MetricsProvenance struct {
	Source MetricsSource `json:"source"`
	// Approximate reports a series that is not a reconstruction of what
	// actually happened. It is true for every source but MetricsSourceGit.
	Approximate bool `json:"approximate"`
	// From is the earliest day the history can speak for. A day before it is
	// reported as unknown rather than as an empty backlog.
	From Date `json:"from,omitempty"`
	// Commits is how many commits were read, and Truncated reports a history
	// that was cut off by a limit before it reached From.
	Commits   int  `json:"commits,omitempty"`
	Truncated bool `json:"truncated,omitempty"`
	// Items is the size of the scope, Covered how many references the history
	// could say anything about at all.
	Items   int `json:"items"`
	Covered int `json:"covered"`
	// Note is one sentence for the UI, always present.
	Note string `json:"note"`
}

// ItemObservation is one reading of an item at an instant: what its front
// matter said the last time somebody wrote it before At.
type ItemObservation struct {
	At       Timestamp      `json:"at"`
	Status   Status         `json:"status,omitempty"`
	Category StatusCategory `json:"category,omitempty"`
	Estimate *float64       `json:"estimate,omitempty"`
	// Deleted reports a revision that removed the file. The item counts as
	// absent from that instant on rather than as finished.
	Deleted bool `json:"deleted,omitempty"`
}

// Points is the observed estimate, 0 when the item carried none.
func (o ItemObservation) Points() float64 {
	if o.Estimate == nil {
		return 0
	}
	return *o.Estimate
}

// ItemHistory is everything known about one `<projectKey>/<itemId>` reference
// over time, oldest observation first.
type ItemHistory struct {
	Ref string `json:"ref"`
	// Observations are sorted by At ascending; two observations may share an
	// instant, in which case the later entry wins.
	Observations []ItemObservation `json:"observations"`
	// Complete reports that the first observation is the item's creation, so
	// that "before it" means "did not exist" rather than "not known". A history
	// that was truncated, or approximated from an `updated` stamp, is not
	// complete and its earlier days are reported as unknown.
	Complete bool `json:"complete"`
}

// itemState is where one reference stood at the end of one day.
type itemState int

const (
	// stateUnknown: the history cannot say. It is never counted as work.
	stateUnknown itemState = iota
	// stateAbsent: the item did not exist yet, or had been deleted.
	stateAbsent
	// statePresent: the observation applies.
	statePresent
)

// at returns the observation in force at instant t.
func (h ItemHistory) at(t time.Time) (ItemObservation, itemState) {
	var found ItemObservation
	ok := false
	for _, obs := range h.Observations {
		if obs.At.IsZero() || obs.At.After(t) {
			continue
		}
		found, ok = obs, true
	}
	switch {
	case ok && found.Deleted:
		return ItemObservation{}, stateAbsent
	case ok:
		return found, statePresent
	case h.Complete:
		return ItemObservation{}, stateAbsent
	default:
		return ItemObservation{}, stateUnknown
	}
}

// firstEntering returns the instant the item first reached one of the given
// categories, and whether it ever did.
func (h ItemHistory) firstEntering(categories ...StatusCategory) (time.Time, bool) {
	for _, obs := range h.Observations {
		if obs.Deleted || obs.At.IsZero() {
			continue
		}
		for _, want := range categories {
			if obs.Category == want {
				return obs.At.Time, true
			}
		}
	}
	return time.Time{}, false
}

// MetricsInput is everything BuildSprintMetrics needs beyond the sprint: the
// cards the scope currently resolves to, the reconstructed history, and where
// that history came from.
type MetricsInput struct {
	// Cards are the sprint's cards as BuildSprintView rendered them. They fill
	// in the titles the data table shows and the estimate of a reference the
	// history could not read.
	Cards []BoardCard
	// History is one entry per reference, in any order.
	History []ItemHistory
	// Provenance describes History. Its Items and Covered counters are filled
	// in here rather than by the caller.
	Provenance MetricsProvenance
	// Now bounds the observed part of the series: a day after it is future and
	// carries no measurement.
	Now time.Time
}

// FlowBand is one band of a cumulative flow diagram.
type FlowBand string

// The bands of a cumulative flow diagram, in stacking order, bottom first: the
// finished work accumulates at the bottom and the top edge of the stack is the
// scope, so scope growth is visible as the whole shape rising.
const (
	FlowDone       FlowBand = "done"
	FlowCancelled  FlowBand = "cancelled"
	FlowInProgress FlowBand = "in_progress"
	FlowTodo       FlowBand = "todo"
	// FlowUnknown holds the references whose state on that day the history
	// cannot state. It is drawn as a hatched band and never silently merged
	// into another one.
	FlowUnknown FlowBand = "unknown"
)

// FlowBands returns the bands in stacking order, bottom first.
func FlowBands() []FlowBand {
	return []FlowBand{FlowDone, FlowCancelled, FlowInProgress, FlowTodo, FlowUnknown}
}

// bandOf maps a status category onto a band.
func bandOf(category StatusCategory) FlowBand {
	switch category {
	case CategoryDone:
		return FlowDone
	case CategoryCancelled:
		return FlowCancelled
	case CategoryInProgress:
		return FlowInProgress
	case CategoryTodo:
		return FlowTodo
	default:
		return FlowUnknown
	}
}

// BurndownPoint is one day of a burndown.
type BurndownPoint struct {
	Date Date `json:"date"`
	// Day is 1-based, so that day 1 is the first day of the sprint.
	Day int `json:"day"`
	// Ideal is the straight line from the commitment on day 1 to zero on the
	// last day. It exists for every day, past or future.
	Ideal float64 `json:"ideal"`
	// Observed reports a day the history can speak for: on or before today and
	// on or after the sprint start. Only observed days carry Remaining, Scope,
	// Done and the counters; a chart must not plot the others.
	Observed bool `json:"observed"`
	// Remaining is the estimate of the scope that was not finished by the end
	// of that day, Scope the estimate of everything in the sprint that day, and
	// Done their difference.
	Remaining float64 `json:"remaining"`
	Scope     float64 `json:"scope"`
	Done      float64 `json:"done"`
	// Items, Completed and Unknown count references rather than points.
	Items     int `json:"items"`
	Completed int `json:"completed"`
	// Unknown counts the references whose state that day the history cannot
	// state. A day with Unknown > 0 is an approximation and says so.
	Unknown int `json:"unknown"`
}

// Burndown is the remaining work of a sprint per day against the ideal line.
type Burndown struct {
	Sprint string `json:"sprint"`
	Start  Date   `json:"start,omitempty"`
	End    Date   `json:"end,omitempty"`
	// CommittedPoints is where the ideal line starts: the estimate of the
	// references the sprint committed to, falling back to the scope on day 1
	// for a sprint that never started.
	CommittedPoints float64         `json:"committedPoints"`
	Points          []BurndownPoint `json:"points"`
}

// FlowPoint is one day of a cumulative flow diagram: how many references stood
// in each band at the end of it.
type FlowPoint struct {
	Date     Date             `json:"date"`
	Day      int              `json:"day"`
	Observed bool             `json:"observed"`
	Counts   map[FlowBand]int `json:"counts"`
	Total    int              `json:"total"`
}

// CumulativeFlow is the item counts by status band over the sprint window.
type CumulativeFlow struct {
	Bands []FlowBand  `json:"bands"`
	Days  []FlowPoint `json:"days"`
}

// Stat summarizes a sample of durations in days.
type Stat struct {
	Count  int     `json:"count"`
	Mean   float64 `json:"mean"`
	Median float64 `json:"median"`
	P85    float64 `json:"p85"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
}

// FlowStats are the three numbers a team reads next to the charts. Each carries
// the sample it was computed from, because a mean over two items is a story and
// not a measurement.
type FlowStats struct {
	// Throughput is how many references reached a terminal status inside the
	// sprint window.
	Throughput int `json:"throughput"`
	// ThroughputPerWeek scales it to a week, so that sprints of different
	// lengths can be compared.
	ThroughputPerWeek float64 `json:"throughputPerWeek"`
	// CycleTime measures first entry into `in_progress` to first entry into a
	// terminal status; LeadTime measures the item's first observation to the
	// same instant, so it needs a complete history and is empty without one.
	CycleTime Stat `json:"cycleTime"`
	LeadTime  Stat `json:"leadTime"`
	// Excluded counts the finished references no duration could be measured
	// for, because their history does not reach far enough back.
	Excluded int `json:"excluded"`
}

// SprintMetricsView is one sprint's metrics as the UI reads them: the header it
// already knows, the two charts, the flow numbers, and where all of it came
// from.
type SprintMetricsView struct {
	Sprint     SprintSummary     `json:"sprint"`
	Burndown   Burndown          `json:"burndown"`
	Flow       CumulativeFlow    `json:"flow"`
	Stats      FlowStats         `json:"stats"`
	Provenance MetricsProvenance `json:"provenance"`
	// Items is the scope, so that every chart has the table of numbers behind
	// it without a second call.
	Items []BoardCard `json:"items"`
}

// ApproximateHistories builds the fallback history of a scope from the cards
// alone: an item is assumed to have held its current status since its `updated`
// stamp and to be unknown before it. It is the honest floor for a host that
// cannot read git — it places the last transition of each item and claims
// nothing else, which is why the histories it returns are never Complete.
func ApproximateHistories(cards []BoardCard) []ItemHistory {
	out := make([]ItemHistory, 0, len(cards))
	for _, card := range cards {
		if card.Status == "" {
			continue
		}
		at := card.Updated
		if at.IsZero() {
			continue
		}
		out = append(out, ItemHistory{
			Ref: card.Ref,
			Observations: []ItemObservation{{
				At: at, Status: card.Status, Category: card.Category,
				Estimate: clonePtr(card.Estimate),
			}},
		})
	}
	return out
}

// BuildSprintMetrics computes the burndown, the cumulative flow and the flow
// statistics of one sprint. It never fails and never invents a number: a day no
// history covers is reported as unknown, and the provenance says why.
func BuildSprintMetrics(s *Sprint, in MetricsInput) SprintMetricsView {
	out := SprintMetricsView{
		Burndown: Burndown{Points: []BurndownPoint{}},
		Flow:     CumulativeFlow{Bands: FlowBands(), Days: []FlowPoint{}},
		Items:    []BoardCard{},
	}
	if s == nil {
		out.Provenance = describeProvenance(in.Provenance)
		return out
	}
	out.Sprint = SummarizeSprint(s, in.Cards, in.Now)
	out.Burndown.Sprint, out.Burndown.Start, out.Burndown.End = s.ID, s.Start, s.End

	byRef := map[string]ItemHistory{}
	for _, h := range in.History {
		byRef[h.Ref] = h
	}
	cardByRef := map[string]BoardCard{}
	for _, card := range in.Cards {
		cardByRef[card.Ref] = card
	}
	for _, ref := range s.Items {
		if card, ok := cardByRef[ref]; ok {
			out.Items = append(out.Items, card)
			continue
		}
		out.Items = append(out.Items, BoardCard{Ref: ref})
	}

	provenance := in.Provenance
	provenance.Items = len(s.Items)
	for _, ref := range s.Items {
		if h, ok := byRef[ref]; ok && len(h.Observations) > 0 {
			provenance.Covered++
		}
	}
	out.Provenance = describeProvenance(provenance)

	days := s.TotalDays()
	if days <= 0 {
		out.Stats = buildFlowStats(s, byRef, in.Now)
		return out
	}
	today := NewDate(in.Now)
	committed := map[string]bool{}
	for _, ref := range s.Committed {
		committed[ref] = true
	}

	for i := 0; i < days; i++ {
		date := Date{Time: s.Start.AddDate(0, 0, i)}
		// A day is measured at its end: the instant midnight of the next day.
		cutoff := date.AddDate(0, 0, 1)
		observed := !in.Now.IsZero() && !date.After(today.Time)
		point := BurndownPoint{Date: date, Day: i + 1, Observed: observed}
		flow := FlowPoint{Date: date, Day: i + 1, Observed: observed, Counts: map[FlowBand]int{}}
		for _, band := range FlowBands() {
			flow.Counts[band] = 0
		}
		for _, ref := range s.Items {
			obs, state := byRef[ref].at(cutoff)
			switch state {
			case stateUnknown:
				point.Unknown++
				point.Items++
				flow.Counts[FlowUnknown]++
				flow.Total++
			case stateAbsent:
				// The item did not exist that day: it is neither scope nor
				// work, and the scope line rises when it appears.
			case statePresent:
				point.Items++
				point.Scope += obs.Points()
				band := bandOf(obs.Category)
				flow.Counts[band]++
				flow.Total++
				if band == FlowDone || band == FlowCancelled {
					point.Completed++
					point.Done += obs.Points()
				} else {
					point.Remaining += obs.Points()
				}
			}
		}
		if !observed {
			point.Remaining, point.Scope, point.Done = 0, 0, 0
			point.Items, point.Completed, point.Unknown = 0, 0, 0
			for _, band := range FlowBands() {
				flow.Counts[band] = 0
			}
			flow.Total = 0
		}
		out.Burndown.Points = append(out.Burndown.Points, point)
		out.Flow.Days = append(out.Flow.Days, flow)
	}

	out.Burndown.CommittedPoints = idealStart(s, in.Cards, committed, out.Burndown.Points)
	for i := range out.Burndown.Points {
		out.Burndown.Points[i].Ideal = idealAt(out.Burndown.CommittedPoints, i, days)
	}
	out.Stats = buildFlowStats(s, byRef, in.Now)
	return out
}

// idealStart is where the ideal line begins: the estimate of what the sprint
// committed to, falling back to the scope it was observed to hold on its first
// day for a sprint that never started.
func idealStart(s *Sprint, cards []BoardCard, committed map[string]bool, points []BurndownPoint) float64 {
	if len(committed) > 0 {
		total := 0.0
		for _, card := range cards {
			if committed[card.Ref] {
				total += card.Points()
			}
		}
		return total
	}
	if len(points) > 0 && points[0].Observed {
		return points[0].Scope
	}
	total := 0.0
	members := s.Members()
	for _, card := range cards {
		if members[card.Ref] {
			total += card.Points()
		}
	}
	return total
}

// idealAt is the straight line from start on day 1 to zero on the last day.
func idealAt(start float64, index, days int) float64 {
	if days <= 1 {
		return 0
	}
	return round2(start * (1 - float64(index)/float64(days-1)))
}

// buildFlowStats measures cycle time, lead time and throughput over the sprint
// window. A finished reference whose history does not reach back to the
// transition being measured is excluded and counted, never guessed at.
func buildFlowStats(s *Sprint, byRef map[string]ItemHistory, now time.Time) FlowStats {
	out := FlowStats{}
	if s.Start.IsZero() || s.End.IsZero() {
		return out
	}
	from := s.Start.Time
	to := s.End.AddDate(0, 0, 1)
	if !now.IsZero() && now.Before(to) {
		to = now
	}
	var cycle, lead []float64
	for _, ref := range s.Items {
		history, ok := byRef[ref]
		if !ok || len(history.Observations) == 0 {
			continue
		}
		finished, ok := history.firstEntering(CategoryDone, CategoryCancelled)
		if !ok || finished.Before(from) || !finished.Before(to) {
			continue
		}
		out.Throughput++
		measured := false
		if started, ok := history.firstEntering(CategoryInProgress); ok && !started.After(finished) {
			cycle = append(cycle, days(started, finished))
			measured = true
		}
		if history.Complete {
			created := history.Observations[0].At.Time
			if !created.After(finished) {
				lead = append(lead, days(created, finished))
				measured = true
			}
		}
		if !measured {
			out.Excluded++
		}
	}
	out.CycleTime = summarize(cycle)
	out.LeadTime = summarize(lead)
	length := float64(s.TotalDays())
	if length > 0 {
		out.ThroughputPerWeek = round2(float64(out.Throughput) * 7 / length)
	}
	return out
}

// days is the distance between two instants, in days with two decimals.
func days(from, to time.Time) float64 {
	return round2(to.Sub(from).Hours() / 24)
}

// summarize reduces a sample of durations to the numbers the UI shows.
func summarize(sample []float64) Stat {
	if len(sample) == 0 {
		return Stat{}
	}
	sorted := append([]float64(nil), sample...)
	sort.Float64s(sorted)
	total := 0.0
	for _, v := range sorted {
		total += v
	}
	return Stat{
		Count:  len(sorted),
		Mean:   round2(total / float64(len(sorted))),
		Median: percentile(sorted, 0.5),
		P85:    percentile(sorted, 0.85),
		Min:    sorted[0],
		Max:    sorted[len(sorted)-1],
	}
}

// percentile is the nearest-rank percentile of an ascending sample.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(p * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

// round2 keeps two decimals, so that a sum of estimates never renders as
// 12.000000000000002.
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// describeProvenance fills in the sentence the UI shows next to a chart. It is
// the one place that decides how an approximation is worded, so that every
// surface says the same thing.
func describeProvenance(p MetricsProvenance) MetricsProvenance {
	if p.Source == "" {
		p.Source = MetricsSourceNone
	}
	p.Approximate = p.Source != MetricsSourceGit || p.Truncated
	if p.Note != "" {
		return p
	}
	switch p.Source {
	case MetricsSourceGit:
		p.Note = "Reconstructed from the git history of the item files"
		if !p.From.IsZero() {
			p.Note += ", back to " + p.From.String()
		}
		p.Note += "."
		if p.Truncated {
			p.Note += " The history was cut off by the commit limit, so earlier days are reported as unknown."
		}
	case MetricsSourceUpdated:
		p.Note = "Approximated from each item's `updated` stamp: every item is " +
			"assumed to have held its current status since it was last written, " +
			"and its state before that is reported as unknown. Open this " +
			"repository with the companion (`gintrack serve`) for the real " +
			"history read from git."
	default:
		p.Note = "No history is available here, so only the current state of the " +
			"scope is shown. Open this repository with the companion " +
			"(`gintrack serve`) to reconstruct the series from git."
	}
	return p
}

// ItemRevision is one version of one item file as a host read it out of the
// history of its repository: the bytes as they stood, and when they were
// committed. Reading them is native work (internal/gitops); turning them into
// observations is domain work and therefore happens here, once, for both hosts.
type ItemRevision struct {
	// Ref is the `<projectKey>/<itemId>` the file belongs to.
	Ref string
	// At is the commit instant.
	At Timestamp
	// Data is the file as it stood, empty when Deleted.
	Data []byte
	// Deleted reports a revision that removed the file.
	Deleted bool
}

// HistoriesFromRevisions turns the revisions of a set of item files into one
// history per reference. categoryOf maps a status onto its coarse bucket in the
// project the item belongs to; a nil function leaves every category empty,
// which reports every day as unknown rather than as finished work.
//
// complete says whether the walk reached the beginning of the history. Only a
// complete walk may claim that "before the first observation" means "did not
// exist"; a truncated one reports those days as unknown.
func HistoriesFromRevisions(
	revs []ItemRevision, complete bool, categoryOf func(ProjectKey, Status) StatusCategory,
) []ItemHistory {
	grouped := map[string][]ItemObservation{}
	for _, rev := range revs {
		if rev.Ref == "" || rev.At.IsZero() {
			continue
		}
		obs := ItemObservation{At: rev.At, Deleted: rev.Deleted}
		if !rev.Deleted {
			item, err := ParseItem(rev.Ref, rev.Data)
			if err != nil {
				// A revision that does not parse is a revision the history
				// cannot read. Skipping it leaves the previous observation in
				// force, which is the honest reading of "we cannot tell".
				continue
			}
			obs.Status = item.Status
			obs.Estimate = clonePtr(item.Estimate)
			if categoryOf != nil {
				if ref, err := ParseRef(rev.Ref); err == nil {
					obs.Category = categoryOf(ref.Project, item.Status)
				}
			}
		}
		grouped[rev.Ref] = append(grouped[rev.Ref], obs)
	}
	refs := make([]string, 0, len(grouped))
	for ref := range grouped {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	out := make([]ItemHistory, 0, len(refs))
	for _, ref := range refs {
		observations := grouped[ref]
		sort.SliceStable(observations, func(i, j int) bool {
			return observations[i].At.Before(observations[j].At.Time)
		})
		out = append(out, ItemHistory{Ref: ref, Observations: observations, Complete: complete})
	}
	return out
}
