package system

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeTarGz builds an in-memory .tar.gz from name->body entries.
func makeTarGz(t *testing.T, files map[string]string) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if strings.HasSuffix(name, "/") {
			hdr.Typeflag, hdr.Size = tar.TypeDir, 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			tw.Write([]byte(body))
		}
	}
	tw.Close()
	gz.Close()
	return &buf
}

func TestExtractWordPressStripsPrefix(t *testing.T) {
	tgz := makeTarGz(t, map[string]string{
		"wordpress/":                        "",
		"wordpress/index.php":               "<?php // wp",
		"wordpress/wp-includes/version.php": "x",
		"outside.txt":                       "ignored", // not under wordpress/
	})
	dest := t.TempDir()
	if err := extractWordPress(tgz, dest); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dest, "index.php")); err != nil || string(b) != "<?php // wp" {
		t.Fatalf("index.php not extracted to root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "wp-includes", "version.php")); err != nil {
		t.Fatalf("nested file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "outside.txt")); !os.IsNotExist(err) {
		t.Fatal("entry outside wordpress/ should have been ignored")
	}
}

func TestExtractWordPressRejectsTraversal(t *testing.T) {
	tgz := makeTarGz(t, map[string]string{"wordpress/../evil.php": "x"})
	dest := t.TempDir()
	if err := extractWordPress(tgz, dest); err == nil {
		t.Fatal("expected path-traversal entry to be rejected")
	}
}

func TestWordpressSalts(t *testing.T) {
	s, err := wordpressSalts()
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(s, "define( '"); n != 8 {
		t.Fatalf("expected 8 salt defines, got %d", n)
	}
	if strings.Contains(s, "''") {
		t.Fatal("a salt was empty")
	}
}

func TestPhpEscape(t *testing.T) {
	if got := phpEscape(`pa'ss\word`); got != `pa\'ss\\word` {
		t.Fatalf("phpEscape = %q", got)
	}
}
