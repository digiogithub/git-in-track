package trace

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// The report parsers. Each one is pure — bytes in, RawResults out — and
// streams its input, holding at most one event, one test case or one test
// file of the report in memory at a time plus one entry per test.

// ErrReportFormat reports input that is not a report of the requested (or of
// any detectable) format.
var ErrReportFormat = errors.New("not a test report")

// maxGoTestLine bounds one line of a go test -json stream. An output event
// carries one line of test output, so a longer line is not a sane report.
const maxGoTestLine = 64 << 20

// ParseReport parses a test report. An empty format detects it from the
// first bytes: "<" is JUnit XML, a line holding an "Action" key is a go test
// -json stream, any other "{" is a Vitest JSON report. It returns the format
// it parsed and the results, repeated tests collapsed (failing beats passing
// beats skipped).
func ParseReport(r io.Reader, format ReportFormat) (ReportFormat, []RawResult, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	if format == "" {
		f, err := detectFormat(br)
		if err != nil {
			return "", nil, err
		}
		format = f
	}
	var (
		out []RawResult
		err error
	)
	switch format {
	case FormatGoTest:
		out, err = ParseGoTest(br)
	case FormatJUnit:
		out, err = ParseJUnit(br)
	case FormatVitest:
		out, err = ParseVitest(br)
	default:
		return "", nil, fmt.Errorf("unknown report format %q: use go, junit or vitest", format)
	}
	return format, out, err
}

func detectFormat(br *bufio.Reader) (ReportFormat, error) {
	head, err := br.Peek(64 << 10)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return "", fmt.Errorf("read the report: %w", err)
	}
	head = bytes.TrimPrefix(head, []byte("\xef\xbb\xbf"))
	trimmed := bytes.TrimLeft(head, " \t\r\n")
	switch {
	case len(trimmed) == 0:
		return "", fmt.Errorf("%w: the input is empty", ErrReportFormat)
	case trimmed[0] == '<':
		return FormatJUnit, nil
	case bytes.Contains(head, []byte(`"Action":`)) || bytes.Contains(head, []byte(`"Action" :`)):
		return FormatGoTest, nil
	case trimmed[0] == '{':
		return FormatVitest, nil
	}
	return "", fmt.Errorf("%w: cannot tell the format from the first bytes; pass --format go, junit or vitest", ErrReportFormat)
}

// ---- go test -json ----

// goTestEvent is one event of `go test -json` (cmd/test2json).
type goTestEvent struct {
	Action  string   `json:"Action"`
	Package string   `json:"Package"`
	Test    string   `json:"Test"`
	Elapsed *float64 `json:"Elapsed"`
}

// ParseGoTest parses a `go test -json` event stream. Only the terminal
// events of a test (pass, fail, skip) are kept; package-level events, output
// and the run/pause/cont events of parallel tests are ignored, so events of
// tests running in parallel may interleave freely. Lines that do not start
// with "{" (build output mixed into the stream by 2>&1) are skipped; a line
// that starts with "{" but is not a JSON event is an error.
func ParseGoTest(r io.Reader) ([]RawResult, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxGoTestLine)
	var out []RawResult
	sawEvent := false
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev goTestEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return nil, fmt.Errorf("go test -json line %d: %w", n, err)
		}
		if ev.Action == "" {
			return nil, fmt.Errorf("go test -json line %d: %w: an event without an Action", n, ErrReportFormat)
		}
		sawEvent = true
		if ev.Test == "" {
			continue
		}
		var o Outcome
		switch ev.Action {
		case "pass":
			o = OutcomePass
		case "fail":
			o = OutcomeFail
		case "skip":
			o = OutcomeSkip
		default:
			continue
		}
		out = append(out, RawResult{
			Format: FormatGoTest, Scope: ev.Package, Name: ev.Test, Outcome: o,
			Duration: seconds(ev.Elapsed),
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read go test -json: %w", err)
	}
	if !sawEvent {
		return nil, fmt.Errorf("%w: no go test -json event found", ErrReportFormat)
	}
	return collapse(out), nil
}

func seconds(s *float64) time.Duration {
	if s == nil || math.IsNaN(*s) || *s < 0 || *s > math.MaxInt64/float64(time.Second) {
		return 0
	}
	return time.Duration(*s * float64(time.Second))
}

// ---- JUnit XML ----

// junitCase is one <testcase>. The body of <failure>, <error> and <skipped>
// (a stack trace, possibly large) is not kept.
type junitCase struct {
	Name      string     `xml:"name,attr"`
	Classname string     `xml:"classname,attr"`
	File      string     `xml:"file,attr"`
	Time      string     `xml:"time,attr"`
	Failures  []struct{} `xml:"failure"`
	Errors    []struct{} `xml:"error"`
	Skipped   []struct{} `xml:"skipped"`
}

// ParseJUnit parses JUnit XML: a <testsuites> or a <testsuite> root, suites
// nested at any depth. A <testcase> with a <failure> or an <error> fails,
// one with <skipped> is skipped, any other passes. The test's file is the
// testcase's file attribute, else the nearest enclosing testsuite's.
func ParseJUnit(r io.Reader) ([]RawResult, error) {
	dec := xml.NewDecoder(r)
	dec.Strict = true
	var (
		out      []RawResult
		files    []string // file attribute of each open testsuite
		sawRoot  bool
		elemPath int
	)
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("junit xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if elemPath == 0 {
				if t.Name.Local != "testsuites" && t.Name.Local != "testsuite" {
					return nil, fmt.Errorf("%w: the JUnit root element is <%s>, want <testsuites> or <testsuite>", ErrReportFormat, t.Name.Local)
				}
				sawRoot = true
			}
			switch t.Name.Local {
			case "testsuite":
				elemPath++
				files = append(files, attr(t, "file"))
			case "testcase":
				var c junitCase
				if err := dec.DecodeElement(&c, &t); err != nil {
					return nil, fmt.Errorf("junit xml: testcase: %w", err)
				}
				out = append(out, junitResult(c, files))
			default:
				elemPath++
			}
		case xml.EndElement:
			if t.Name.Local == "testsuite" && len(files) > 0 {
				files = files[:len(files)-1]
			}
			elemPath--
		}
	}
	if !sawRoot {
		return nil, fmt.Errorf("%w: no JUnit <testsuites> or <testsuite> element", ErrReportFormat)
	}
	return collapse(out), nil
}

func junitResult(c junitCase, files []string) RawResult {
	o := OutcomePass
	switch {
	case len(c.Failures) > 0 || len(c.Errors) > 0:
		o = OutcomeFail
	case len(c.Skipped) > 0:
		o = OutcomeSkip
	}
	file := strings.TrimSpace(c.File)
	for i := len(files) - 1; file == "" && i >= 0; i-- {
		file = strings.TrimSpace(files[i])
	}
	var d time.Duration
	if s, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(c.Time), ",", ""), 64); err == nil {
		d = seconds(&s)
	}
	return RawResult{
		Format: FormatJUnit, Scope: strings.TrimSpace(c.Classname), File: file,
		Name: strings.TrimSpace(c.Name), Outcome: o, Duration: d,
	}
}

func attr(t xml.StartElement, name string) string {
	for _, a := range t.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// ---- Vitest JSON ----

// vitestFile is one entry of testResults. Jest's --json output has the same
// shape.
type vitestFile struct {
	Name             string            `json:"name"`
	AssertionResults []vitestAssertion `json:"assertionResults"`
}

type vitestAssertion struct {
	AncestorTitles []string `json:"ancestorTitles"`
	Title          string   `json:"title"`
	Status         string   `json:"status"`
	Duration       *float64 `json:"duration"`
}

// ParseVitest parses the Vitest JSON reporter (`vitest run --reporter=json`).
// Each assertion result becomes one test named "describe > it" from its
// ancestor titles and title, in the test file the entry names. passed passes,
// failed fails, skipped, pending, todo and disabled skip; any other status is
// ignored. The report is streamed one test file at a time.
func ParseVitest(r io.Reader) ([]RawResult, error) {
	dec := json.NewDecoder(r)
	if err := expectDelim(dec, '{'); err != nil {
		return nil, fmt.Errorf("vitest json: %w", err)
	}
	var out []RawResult
	sawResults := false
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("vitest json: %w", err)
		}
		key, _ := tok.(string)
		if key != "testResults" {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, fmt.Errorf("vitest json: %s: %w", key, err)
			}
			continue
		}
		sawResults = true
		if err := expectDelim(dec, '['); err != nil {
			return nil, fmt.Errorf("vitest json: testResults: %w", err)
		}
		for dec.More() {
			var f vitestFile
			if err := dec.Decode(&f); err != nil {
				return nil, fmt.Errorf("vitest json: testResults: %w", err)
			}
			out = append(out, vitestResults(f)...)
		}
		if err := expectDelim(dec, ']'); err != nil {
			return nil, fmt.Errorf("vitest json: testResults: %w", err)
		}
	}
	if err := expectDelim(dec, '}'); err != nil {
		return nil, fmt.Errorf("vitest json: %w", err)
	}
	if !sawResults {
		return nil, fmt.Errorf("%w: a JSON object without testResults", ErrReportFormat)
	}
	return collapse(out), nil
}

func vitestResults(f vitestFile) []RawResult {
	out := make([]RawResult, 0, len(f.AssertionResults))
	for _, a := range f.AssertionResults {
		var o Outcome
		switch a.Status {
		case "passed":
			o = OutcomePass
		case "failed":
			o = OutcomeFail
		case "skipped", "pending", "todo", "disabled":
			o = OutcomeSkip
		default:
			continue
		}
		titles := make([]string, 0, len(a.AncestorTitles)+1)
		for _, t := range append(append([]string{}, a.AncestorTitles...), a.Title) {
			if t = strings.TrimSpace(t); t != "" {
				titles = append(titles, t)
			}
		}
		var d time.Duration
		if a.Duration != nil {
			ms := *a.Duration / 1000
			d = seconds(&ms)
		}
		out = append(out, RawResult{
			Format: FormatVitest, Scope: f.Name, File: f.Name,
			Name: strings.Join(titles, " > "), Outcome: o, Duration: d,
		})
	}
	return out
}

func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("read a JSON token: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != want {
		return fmt.Errorf("%w: expected %q, found %v", ErrReportFormat, want, tok)
	}
	return nil
}
