package checkpoint

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cp := New(dir, "emails.txt", 4)
	cp.Mark("c@d.com")
	cp.Mark("a@b.com")
	cp.Mark("a@b.com")
	if err := cp.Save(); err != nil {
		t.Fatal(err)
	}
	if filepath.Base(cp.Path) != Filename {
		t.Errorf("path = %s", cp.Path)
	}
	if _, err := os.Stat(filepath.Join(dir, "checkpoint.tmp")); !os.IsNotExist(err) {
		t.Error("tmp file left behind")
	}

	f, _ := os.Open(cp.Path)
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Scan()
	var header map[string]any
	json.Unmarshal(sc.Bytes(), &header)
	if header["type"] != "header" || header["total"] != 4.0 || header["completed"] != 2.0 ||
		header["emails_file"] != "emails.txt" {
		t.Errorf("header = %v", header)
	}

	got, err := Load(cp.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("loaded %v", got)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(filepath.Join(dir, "missing.jsonl")); err == nil {
		t.Error("expected error for missing file")
	}

	p := filepath.Join(dir, "cp.jsonl")
	os.WriteFile(p, []byte(`{"type":"header","total":3}`+"\n\n"+
		`{"type":"email","email":"a@b.com"}`+"\n"+
		`{"type":"email","email":"  "}`+"\n"), 0o644)
	got, err := Load(p)
	if err != nil || len(got) != 1 {
		t.Errorf("got %v, %v", got, err)
	}

	os.WriteFile(p, []byte("{not json\n"), 0o644)
	if _, err := Load(p); err == nil {
		t.Error("expected corrupt error")
	}
}

func TestDelete(t *testing.T) {
	cp := New(t.TempDir(), "e.txt", 1)
	if err := cp.Delete(); err != nil {
		t.Errorf("delete of missing file: %v", err)
	}
	cp.Save()
	cp.Delete()
	if _, err := os.Stat(cp.Path); !os.IsNotExist(err) {
		t.Error("file still exists")
	}
}
