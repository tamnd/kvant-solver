package corpus

import "testing"

func TestIssueKeyRoundTrip(t *testing.T) {
	cases := []struct {
		in  string
		dir string
	}{
		{"kvant_1970_1", "1970/01"},
		{"kvant_1975_12", "1975/12"},
		{"kvant_1976_5-6", "1976/05-06"},
		{"kvant_2026_11-12", "2026/11-12"},
	}
	for _, c := range cases {
		key, err := ParseIssueKey(c.in)
		if err != nil {
			t.Fatalf("ParseIssueKey(%q): %v", c.in, err)
		}
		if got := key.String(); got != c.in {
			t.Errorf("round trip of %q gave %q", c.in, got)
		}
		if got := key.Dir(); got != c.dir {
			t.Errorf("Dir of %q gave %q, want %q", c.in, got, c.dir)
		}
	}
}

func TestIssueKeyRejects(t *testing.T) {
	for _, in := range []string{
		"",
		"kvant_1975",
		"kvant_75_1",
		"kvant_1975_0013",
		"1975_1",
		"kvant_1975_1_p0001",
	} {
		if _, err := ParseIssueKey(in); err == nil {
			t.Errorf("ParseIssueKey(%q) should have failed", in)
		}
	}
	if _, err := NewIssueKey(1969, "1"); err == nil {
		t.Error("a year before the first issue should have failed")
	}
}

func TestPageID(t *testing.T) {
	id, err := ParsePageID("kvant_1975_1_p0007")
	if err != nil {
		t.Fatal(err)
	}
	if id.Index != 7 || id.Issue.Year != 1975 {
		t.Fatalf("parsed to %+v", id)
	}
	if got := id.String(); got != "kvant_1975_1_p0007" {
		t.Errorf("String gave %q", got)
	}
	if got := id.Filename(); got != "0007.md" {
		t.Errorf("Filename gave %q", got)
	}
	for _, in := range []string{"kvant_1975_1_p7", "kvant_1975_1_p0000", "kvant_1975_1"} {
		if _, err := ParsePageID(in); err == nil {
			t.Errorf("ParsePageID(%q) should have failed", in)
		}
	}
}

func TestProblemID(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		subject Subject
		path    string
	}{
		{"M1234", "M1234", Math, "problems/m/1234.md"},
		{"m1", "M1", Math, "problems/m/0001.md"},
		{"F567", "F567", Physics, "problems/f/0567.md"},
	}
	for _, c := range cases {
		id, err := ParseProblemID(c.in)
		if err != nil {
			t.Fatalf("ParseProblemID(%q): %v", c.in, err)
		}
		if id.String() != c.want || id.Subject != c.subject {
			t.Errorf("ParseProblemID(%q) gave %s %s", c.in, id, id.Subject)
		}
		if got := id.Path(); got != c.path {
			t.Errorf("Path of %q gave %q, want %q", c.in, got, c.path)
		}
	}
	for _, in := range []string{"", "M", "X12", "M0", "1234"} {
		if _, err := ParseProblemID(in); err == nil {
			t.Errorf("ParseProblemID(%q) should have failed", in)
		}
	}
}

func TestTag(t *testing.T) {
	if _, err := ParseTag("0A3F"); err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{"0a3f", "0A3", "0A3FF", "0A-F", ""} {
		if _, err := ParseTag(in); err == nil {
			t.Errorf("ParseTag(%q) should have failed", in)
		}
	}
}

func TestArticleID(t *testing.T) {
	key, err := ParseIssueKey("kvant_1975_1")
	if err != nil {
		t.Fatal(err)
	}
	id, err := NewArticleID(key, "bronshteyn-ellips")
	if err != nil {
		t.Fatal(err)
	}
	if got := id.String(); got != "1975-1-bronshteyn-ellips" {
		t.Errorf("String gave %q", got)
	}
	if _, err := NewArticleID(key, "Bronshteyn Ellips"); err == nil {
		t.Error("a slug with spaces and capitals should have failed")
	}
}
