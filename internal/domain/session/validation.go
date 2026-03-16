package session

import "strings"

var allowedCoreVersions = map[string]struct{}{
	"1.2.0": {},
	"1.2.5": {},
}

func NormalizeCoreVersion(v string) string {
	return strings.TrimSpace(v)
}

func IsValidCoreVersion(v string) bool {
	v = NormalizeCoreVersion(v)
	if v == "" {
		return false
	}
	_, ok := allowedCoreVersions[v]
	return ok
}

