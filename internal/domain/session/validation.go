package session

import "strings"

var allowedCoreVersions = map[string]struct{}{
	"1.2.0": {},
	"1.2.5": {},
}

func IsValidCoreVersion(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	_, ok := allowedCoreVersions[v]
	return ok
}

var allowedDomains = map[string]struct{}{
	"ONDC:RET10": {},
	"ONDC:RET11": {},
	"ONDC:RET12": {},
	"ONDC:RET13": {},
	"ONDC:RET14": {},
	"ONDC:RET15": {},
	"ONDC:RET16": {},
	"ONDC:RET18": {},
}

func IsValidDomain(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	_, ok := allowedDomains[v]
	return ok
}

