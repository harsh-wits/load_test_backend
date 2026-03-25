package session

import "testing"

func TestIsValidCoreVersion(t *testing.T) {
	if IsValidCoreVersion("") {
		t.Fatalf("empty core_version should be invalid")
	}
	if !IsValidCoreVersion("1.2.0") || !IsValidCoreVersion("1.2.5") {
		t.Fatalf("allowed core versions should be valid")
	}
	if IsValidCoreVersion("1.1.0") {
		t.Fatalf("unexpected core_version should be invalid")
	}
}

func TestIsValidDomain(t *testing.T) {
	valid := []string{
		"ONDC:RET10",
		"ONDC:RET11",
		"ONDC:RET12",
		"ONDC:RET13",
		"ONDC:RET14",
		"ONDC:RET15",
		"ONDC:RET16",
		"ONDC:RET18",
	}
	for _, d := range valid {
		if !IsValidDomain(d) {
			t.Fatalf("expected domain %s to be valid", d)
		}
	}
	if IsValidDomain("") {
		t.Fatalf("empty domain should be invalid")
	}
	if IsValidDomain("ONDC:RET09") {
		t.Fatalf("unexpected domain should be invalid")
	}
}

