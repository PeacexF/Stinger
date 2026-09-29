package output

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/PeacexF/Stinger/internal/verify"
)

func result(email string, status verify.Status, sub verify.SubStatus) verify.Result {
	return verify.Result{Email: email, Status: status, SubStatus: &sub}
}

type marks []string

func (m *marks) Mark(e string) { *m = append(*m, e) }

func lines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(b))
}

func TestWriter(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "out")
	var m marks
	w, err := Open(dir, "valid.txt", "results.jsonl", false, &m)
	if err != nil {
		t.Fatal(err)
	}
	w.Write(result("valid@b.com", verify.Valid, verify.Confirmed))
	w.Write(result("bad@b.com", verify.Invalid, verify.MailboxNotFound))
	w.Write(result("catchall@b.com", verify.CatchAll, verify.SubCatchAll))
	w.Write(result("grey@b.com", verify.Unknown, verify.Greylisted))
	w.Close()

	if got := lines(t, w.ValidPath); !slices.Equal(got, []string{"valid@b.com", "catchall@b.com"}) {
		t.Errorf("valid = %v", got)
	}
	if got := lines(t, w.JSONLPath); len(got) != 4 {
		t.Errorf("jsonl has %d lines", len(got))
	}
	c := w.Counters()
	if c[verify.Valid] != 1 || c[verify.Invalid] != 1 || c[verify.CatchAll] != 1 || c[verify.Unknown] != 1 {
		t.Errorf("counters = %v", c)
	}
	if w.SubCounters()[verify.Greylisted] != 1 {
		t.Errorf("sub counters = %v", w.SubCounters())
	}
	if len(m) != 4 {
		t.Errorf("marked = %v", m)
	}
}

func TestWriterAppendAndTruncate(t *testing.T) {
	dir := t.TempDir()
	w, _ := Open(dir, "v.txt", "r.jsonl", false, nil)
	w.Write(result("first@b.com", verify.Valid, verify.Confirmed))
	w.Close()

	w, _ = Open(dir, "v.txt", "r.jsonl", true, nil)
	w.Write(result("second@b.com", verify.Valid, verify.Confirmed))
	w.Close()
	if got := lines(t, w.ValidPath); !slices.Equal(got, []string{"first@b.com", "second@b.com"}) {
		t.Errorf("append: %v", got)
	}

	w, _ = Open(dir, "v.txt", "r.jsonl", false, nil)
	w.Write(result("third@b.com", verify.Valid, verify.Confirmed))
	w.Close()
	if got := lines(t, w.ValidPath); !slices.Equal(got, []string{"third@b.com"}) {
		t.Errorf("truncate: %v", got)
	}
}

func TestSummarise(t *testing.T) {
	p := filepath.Join(t.TempDir(), "results.jsonl")
	var b strings.Builder
	b.WriteString(`{"email":"a@gmail.com","status":"catch_all","sub_status":"catch_all","duration_ms":100,"is_catch_all_domain":true}` + "\n")
	b.WriteString(`{"email":"b@x.com","status":"invalid","sub_status":"no_mx","duration_ms":200}` + "\n")
	b.WriteString("\n{corrupt\n")
	for range 12 {
		b.WriteString(`{"email":"c@x.com","status":"unknown","sub_status":"greylisted","duration_ms":150,"reason":"later"}` + "\n")
	}
	os.WriteFile(p, []byte(b.String()), 0o644)

	s, err := Summarise(p)
	if err != nil {
		t.Fatal(err)
	}
	if s.Total != 14 || s.Counts["unknown"] != 12 || s.Counts["invalid"] != 1 {
		t.Errorf("counts: total=%d %v", s.Total, s.Counts)
	}
	if s.SubCounts["greylisted"] != 12 {
		t.Errorf("sub counts: %v", s.SubCounts)
	}
	if s.AvgDurationMS != 150 {
		t.Errorf("avg = %v", s.AvgDurationMS)
	}
	if !slices.Equal(s.CatchAllDomains, []string{"gmail.com"}) {
		t.Errorf("catch-all = %v", s.CatchAllDomains)
	}
	if len(s.SampleErrors) != 10 || !strings.Contains(s.SampleErrors[0], "[greylisted] later") {
		t.Errorf("samples = %v", s.SampleErrors)
	}
}

func TestSummariseEmpty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "empty.jsonl")
	os.WriteFile(p, nil, 0o644)
	s, err := Summarise(p)
	if err != nil || s.Total != 0 || s.AvgDurationMS != 0 {
		t.Errorf("got %+v, %v", s, err)
	}
}
