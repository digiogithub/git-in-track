package core

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// ProjectKey is the uppercase prefix of every id of a project, e.g. "ACME".
type ProjectKey string

// String returns the key as a plain string.
func (k ProjectKey) String() string { return string(k) }

// TypeCode is the two-letter (or one-letter) code embedded in an item id.
type TypeCode string

// The four type codes of the id grammar (R-ID-1).
const (
	CodeEpic      TypeCode = "EP"
	CodeStory     TypeCode = "US"
	CodeTask      TypeCode = "T"
	CodeMilestone TypeCode = "M"
)

// minIDDigits is the minimum zero padding of the numeric part. Padding is a
// minimum, not a maximum: after 9999 items an id simply grows a digit (R-ID-3).
const minIDDigits = 4

// projectKeyRE is the grammar of a project key: [A-Z][A-Z0-9]{1,9}.
var projectKeyRE = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)

// itemIDRE is the grammar of an item id: <KEY>-<TYPECODE>-<NUMBER>.
var itemIDRE = regexp.MustCompile(`^([A-Z][A-Z0-9]{1,9})-(EP|US|T|M)-(\d{4,})$`)

// ValidProjectKey reports whether k matches the project-key grammar.
func ValidProjectKey(k ProjectKey) bool { return projectKeyRE.MatchString(string(k)) }

// TypeCodeFor returns the id type code of an item type.
func TypeCodeFor(t ItemType) (TypeCode, bool) {
	switch t {
	case TypeEpic:
		return CodeEpic, true
	case TypeStory:
		return CodeStory, true
	case TypeTask:
		return CodeTask, true
	case TypeMilestone:
		return CodeMilestone, true
	case TypeComment:
		return "", false
	default:
		return "", false
	}
}

// ItemTypeFor returns the item type a type code stands for.
func ItemTypeFor(c TypeCode) (ItemType, bool) {
	switch c {
	case CodeEpic:
		return TypeEpic, true
	case CodeStory:
		return TypeStory, true
	case CodeTask:
		return TypeTask, true
	case CodeMilestone:
		return TypeMilestone, true
	default:
		return "", false
	}
}

// ParseItemID splits an id into its project key, type code and number.
// It returns an error when s does not match the grammar of docs/03 section 3.3.
func ParseItemID(s string) (ProjectKey, TypeCode, int, error) {
	m := itemIDRE.FindStringSubmatch(s)
	if m == nil {
		return "", "", 0, fmt.Errorf("parse item id %q: want <KEY>-<EP|US|T|M>-<NNNN>", s)
	}
	n, err := strconv.Atoi(m[3])
	if err != nil { // unreachable: the regexp only matches digits
		return "", "", 0, fmt.Errorf("parse item id %q: %w", s, err)
	}
	return ProjectKey(m[1]), TypeCode(m[2]), n, nil
}

// FormatItemID builds an id from its parts, zero-padding the number to at least
// four digits.
func FormatItemID(key ProjectKey, code TypeCode, number int) ItemID {
	return ItemID(fmt.Sprintf("%s-%s-%0*d", key, code, minIDDigits, number))
}

// Valid reports whether the id matches the id grammar.
func (id ItemID) Valid() bool { return itemIDRE.MatchString(string(id)) }

// FileName returns the canonical file name of an item: "<ID>-<slug>.md".
func FileName(id ItemID, title string) string {
	return fmt.Sprintf("%s-%s.md", id, Slugify(title))
}

// IDFromFileName returns the id prefix of a file name, or the empty string when
// the name does not start with a well-formed id. Lookup is by the id front-matter
// field first and by this prefix second (R-SLUG-1).
func IDFromFileName(name string) ItemID {
	base := name
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	base = strings.TrimSuffix(base, ".md")
	parts := strings.Split(base, "-")
	if len(parts) < 3 {
		return ""
	}
	candidate := strings.Join(parts[:3], "-")
	if itemIDRE.MatchString(candidate) {
		return ItemID(candidate)
	}
	return ""
}

// slugMaxBytes is the maximum length of a slug, cut on a "-" boundary.
const slugMaxBytes = 60

// Slugify derives the cosmetic part of a file name from a title, following
// docs/03 section 3.4: fold accents, lowercase, collapse every run of characters
// outside [a-z0-9] into a single "-", trim, truncate to 60 bytes on a "-"
// boundary, and fall back to "item" when nothing is left.
func Slugify(title string) string {
	var b strings.Builder
	b.Grow(len(title))
	dash := false
	for _, r := range title {
		if unicode.Is(unicode.Mn, r) {
			// Combining mark of an already decomposed letter: drop it.
			continue
		}
		folded, ok := foldRune(r)
		if !ok {
			if !dash && b.Len() > 0 {
				dash = true
			}
			continue
		}
		if dash {
			b.WriteByte('-')
			dash = false
		}
		b.WriteString(folded)
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > slugMaxBytes {
		slug = slug[:slugMaxBytes]
		if i := strings.LastIndexByte(slug, '-'); i > 0 {
			slug = slug[:i]
		}
		slug = strings.Trim(slug, "-")
	}
	if slug == "" {
		return "item"
	}
	return slug
}

// foldRune maps one rune to its slug representation. It reports false for every
// rune that is not part of [a-z0-9] after folding, which the caller turns into a
// separator.
func foldRune(r rune) (string, bool) {
	switch {
	case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		return string(r), true
	case r >= 'A' && r <= 'Z':
		return string(r + ('a' - 'A')), true
	}
	if s, ok := latinFolding[unicode.ToLower(r)]; ok {
		return s, true
	}
	return "", false
}

// latinFolding is a compatibility folding of the Latin letters that appear in the
// languages this tool is used in. A full Unicode NFKD normalisation would need
// golang.org/x/text; this table covers Latin-1 Supplement and Latin Extended-A,
// and any rune outside it degrades to a separator, which is harmless because the
// slug is cosmetic (R-SLUG-1).
var latinFolding = map[rune]string{
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'ä': "a", 'å': "a", 'ā': "a", 'ă': "a", 'ą': "a",
	'æ': "ae",
	'ç': "c", 'ć': "c", 'ĉ': "c", 'ċ': "c", 'č': "c",
	'ď': "d", 'đ': "d", 'ð': "d",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ē': "e", 'ĕ': "e", 'ė': "e", 'ę': "e", 'ě': "e",
	'ĝ': "g", 'ğ': "g", 'ġ': "g", 'ģ': "g",
	'ĥ': "h", 'ħ': "h",
	'ì': "i", 'í': "i", 'î': "i", 'ï': "i", 'ĩ': "i", 'ī': "i", 'ĭ': "i", 'į': "i", 'ı': "i",
	'ĵ': "j",
	'ķ': "k",
	'ĺ': "l", 'ļ': "l", 'ľ': "l", 'ŀ': "l", 'ł': "l",
	'ñ': "n", 'ń': "n", 'ņ': "n", 'ň': "n", 'ŋ': "n",
	'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ö': "o", 'ø': "o", 'ō': "o", 'ŏ': "o", 'ő': "o",
	'œ': "oe",
	'ŕ': "r", 'ŗ': "r", 'ř': "r",
	'ś': "s", 'ŝ': "s", 'ş': "s", 'š': "s", 'ș': "s", 'ß': "ss",
	'ţ': "t", 'ť': "t", 'ŧ': "t", 'ț': "t",
	'ù': "u", 'ú': "u", 'û': "u", 'ü': "u", 'ũ': "u", 'ū': "u", 'ŭ': "u", 'ů': "u", 'ű': "u", 'ų': "u",
	'ŵ': "w",
	'ý': "y", 'ÿ': "y", 'ŷ': "y",
	'ź': "z", 'ż': "z", 'ž': "z",
	'þ': "th",
}
