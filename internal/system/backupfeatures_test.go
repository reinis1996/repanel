package system

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteRcloneConfig(t *testing.T) {
	// Field-based remote.
	p, err := writeRcloneConfig(map[string]string{"type": "s3", "access_key_id": "AK", "secret_access_key": "SK", "empty": ""})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(p)
	body, _ := os.ReadFile(p)
	s := string(body)
	if !strings.Contains(s, "[repanel]") || !strings.Contains(s, "type = s3") || !strings.Contains(s, "access_key_id = AK") {
		t.Errorf("config missing expected lines:\n%s", s)
	}
	if strings.Contains(s, "empty =") {
		t.Error("empty values should be omitted")
	}

	// Raw paste mode.
	p2, err := writeRcloneConfig(map[string]string{"raw": "type = dropbox\ntoken = {}"})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(p2)
	body2, _ := os.ReadFile(p2)
	if !strings.Contains(string(body2), "type = dropbox") {
		t.Errorf("raw config not written:\n%s", body2)
	}
}

func TestBackupContentsAndSingleFileRestore(t *testing.T) {
	// Build a web tree and archive it (no databases — mysqldump may be absent).
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(src, "index.php"), []byte("<?php echo 1;"), 0o644)
	os.WriteFile(filepath.Join(src, "sub", "app.js"), []byte("console.log(1)"), 0o644)

	archive := filepath.Join(t.TempDir(), "b.tar.gz")
	if err := CreateBackupArchive(archive, []byte(`{"version":"t"}`), []BackupSource{{Prefix: "web", Dir: src}}, nil); err != nil {
		t.Fatal(err)
	}

	c, err := ListBackupContents(archive)
	if err != nil {
		t.Fatal(err)
	}
	if !c.HasWeb {
		t.Error("expected HasWeb")
	}
	if len(c.Files) != 2 {
		t.Errorf("expected 2 web files, got %d: %v", len(c.Files), c.Files)
	}

	// Restore just one file into a fresh target.
	target := t.TempDir()
	if err := RestoreSingleFile(archive, "sub/app.js", target, ""); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(target, "sub", "app.js"))
	if err != nil || string(got) != "console.log(1)" {
		t.Fatalf("single-file restore failed: %v (%q)", err, got)
	}
	if _, err := os.Stat(filepath.Join(target, "index.php")); !os.IsNotExist(err) {
		t.Error("only the selected file should have been restored")
	}

	// A missing file is an error.
	if err := RestoreSingleFile(archive, "nope.txt", target, ""); err == nil {
		t.Error("expected error for a file not in the archive")
	}
}

func TestServerBackupRoundTrip(t *testing.T) {
	// Fake panel state: a db file, a config dir, a certs dir.
	root := t.TempDir()
	dbSnap := filepath.Join(root, "snap.db")
	os.WriteFile(dbSnap, []byte("SQLITE-DATA"), 0o600)
	confDir := filepath.Join(root, "etc")
	os.MkdirAll(confDir, 0o755)
	os.WriteFile(filepath.Join(confDir, "repanel.conf"), []byte("LISTEN=:8443"), 0o644)
	certsDir := filepath.Join(root, "certs")
	os.MkdirAll(filepath.Join(certsDir, "example.com"), 0o755)
	os.WriteFile(filepath.Join(certsDir, "example.com", "cert.pem"), []byte("CERT"), 0o644)

	archive := filepath.Join(root, "server.tar.gz")
	if err := CreateServerBackup(archive, dbSnap, confDir, certsDir); err != nil {
		t.Fatal(err)
	}

	// Restore into fresh locations.
	out := t.TempDir()
	newDB := filepath.Join(out, "repanel.db")
	newConf := filepath.Join(out, "etc")
	newCerts := filepath.Join(out, "certs")
	if err := RestoreServerBackup(archive, newDB, newConf, newCerts); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(newDB); string(b) != "SQLITE-DATA" {
		t.Error("database not restored")
	}
	if b, _ := os.ReadFile(filepath.Join(newConf, "repanel.conf")); string(b) != "LISTEN=:8443" {
		t.Error("config not restored")
	}
	if b, _ := os.ReadFile(filepath.Join(newCerts, "example.com", "cert.pem")); string(b) != "CERT" {
		t.Error("certificate not restored")
	}
}
