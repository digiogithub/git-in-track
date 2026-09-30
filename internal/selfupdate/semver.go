package selfupdate

import (
	"fmt"
	"strconv"
	"strings"
)

// Semver is a parsed semantic version. Build metadata is kept but ignored by
// Compare.
type Semver struct {
	Major, Minor, Patch uint64
	Pre                 []string
	Build               string
}

// ParseSemver parses major.minor.patch[-pre][+build] with an optional
// leading v. Anything else, including "dev", "unknown" and dirty builds such
// as "1.2.3-dirty" is not guaranteed to parse: callers treat an error as
// "not comparable".
func ParseSemver(s string) (Semver, error) {
	orig := s
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V")
	var v Semver
	if i := strings.IndexByte(s, '+'); i >= 0 {
		v.Build = s[i+1:]
		s = s[:i]
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		pre := s[i+1:]
		s = s[:i]
		if pre == "" {
			return Semver{}, fmt.Errorf("selfupdate: invalid version %q", orig)
		}
		v.Pre = strings.Split(pre, ".")
		for _, id := range v.Pre {
			if id == "" {
				return Semver{}, fmt.Errorf("selfupdate: invalid version %q", orig)
			}
			if strings.HasSuffix(id, "dirty") {
				return Semver{}, fmt.Errorf("selfupdate: dirty version %q is not comparable", orig)
			}
		}
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Semver{}, fmt.Errorf("selfupdate: invalid version %q", orig)
	}
	nums := [3]uint64{}
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil || (len(p) > 1 && p[0] == '0') {
			return Semver{}, fmt.Errorf("selfupdate: invalid version %q", orig)
		}
		nums[i] = n
	}
	v.Major, v.Minor, v.Patch = nums[0], nums[1], nums[2]
	return v, nil
}

// IsPrerelease reports whether the version has a pre-release suffix.
func (v Semver) IsPrerelease() bool { return len(v.Pre) > 0 }

// String renders the version without a v prefix.
func (v Semver) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if len(v.Pre) > 0 {
		s += "-" + strings.Join(v.Pre, ".")
	}
	if v.Build != "" {
		s += "+" + v.Build
	}
	return s
}

// Compare returns -1, 0 or 1 following semver precedence (build ignored).
func (v Semver) Compare(o Semver) int {
	for _, p := range [][2]uint64{{v.Major, o.Major}, {v.Minor, o.Minor}, {v.Patch, o.Patch}} {
		if p[0] != p[1] {
			if p[0] < p[1] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(v.Pre) == 0 && len(o.Pre) == 0:
		return 0
	case len(v.Pre) == 0:
		return 1
	case len(o.Pre) == 0:
		return -1
	}
	for i := 0; i < len(v.Pre) && i < len(o.Pre); i++ {
		a, b := v.Pre[i], o.Pre[i]
		an, aErr := strconv.ParseUint(a, 10, 64)
		bn, bErr := strconv.ParseUint(b, 10, 64)
		switch {
		case aErr == nil && bErr == nil:
			if an != bn {
				if an < bn {
					return -1
				}
				return 1
			}
		case aErr == nil:
			return -1
		case bErr == nil:
			return 1
		case a != b:
			if a < b {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(v.Pre) < len(o.Pre):
		return -1
	case len(v.Pre) > len(o.Pre):
		return 1
	}
	return 0
}
