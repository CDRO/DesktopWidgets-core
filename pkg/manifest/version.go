package manifest

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a widget version. Widgets start at 1.0.0 and normally bump the
// major number for every feature release; minor and patch are reserved for
// small fixes that should not nag users with an update prompt.
type Version struct {
	Major, Minor, Patch int
}

// ParseVersion parses "MAJOR", "MAJOR.MINOR" or "MAJOR.MINOR.PATCH",
// with an optional leading "v".
func ParseVersion(s string) (Version, error) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".")
	if len(parts) == 0 || len(parts) > 3 || parts[0] == "" {
		return Version{}, fmt.Errorf("invalid version %q", s)
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return Version{}, fmt.Errorf("invalid version %q", s)
		}
		n[i] = v
	}
	return Version{n[0], n[1], n[2]}, nil
}

func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Compare returns -1, 0 or 1.
func (v Version) Compare(o Version) int {
	for _, d := range [3]int{v.Major - o.Major, v.Minor - o.Minor, v.Patch - o.Patch} {
		if d < 0 {
			return -1
		}
		if d > 0 {
			return 1
		}
	}
	return 0
}

// UpdateKind classifies an update from v to newer.
type UpdateKind string

const (
	UpdateNone  UpdateKind = ""
	UpdateMajor UpdateKind = "major"
	UpdateMinor UpdateKind = "minor"
)

func (v Version) UpdateTo(newer Version) UpdateKind {
	switch {
	case newer.Compare(v) <= 0:
		return UpdateNone
	case newer.Major > v.Major:
		return UpdateMajor
	default:
		return UpdateMinor
	}
}
