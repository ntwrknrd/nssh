package inv

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestPruneLocalBackupsKeepsTieredProviderLocalHistory(t *testing.T) {
	dir := t.TempDir()
	base := "provider_local.conf"
	loc := time.FixedZone("test", -5*60*60)
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, loc)

	var want []string
	for i := 0; i < 12; i++ {
		name := writeBackupAt(t, dir, base, now.Add(-time.Duration(i)*5*time.Minute))
		if i < 10 {
			want = append(want, name)
		}
	}
	for i := 0; i < 7; i++ {
		name := writeBackupAt(t, dir, base, now.Add(-2*time.Hour-time.Duration(i)*2*time.Hour))
		if i < 5 {
			want = append(want, name)
		}
	}
	for day := 1; day <= 8; day++ {
		newer := time.Date(2026, 6, 10-day, 8, 0, 0, 0, loc)
		older := time.Date(2026, 6, 10-day, 7, 0, 0, 0, loc)
		name := writeBackupAt(t, dir, base, newer)
		writeBackupAt(t, dir, base, older)
		if day <= 7 {
			want = append(want, name)
		}
	}

	otherBase := writeBackupAt(t, dir, "provider_netbox.conf", now.Add(-30*24*time.Hour))
	malformed := base + ".not-a-time.bak"
	if err := os.WriteFile(filepath.Join(dir, malformed), []byte("malformed"), 0600); err != nil {
		t.Fatal(err)
	}
	want = append(want, otherBase, malformed)

	if err := pruneLocalBackups(dir, base, now); err != nil {
		t.Fatalf("pruneLocalBackups: %v", err)
	}

	got := backupDirEntries(t, dir)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("remaining backups:\nwant %v\n got %v", want, got)
	}
}

func TestPruneLocalBackupsKeepsNewestWhenAllBackupsAreFutureSkewed(t *testing.T) {
	dir := t.TempDir()
	base := "provider_local.conf"
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.Local)
	newest := writeBackupAt(t, dir, base, now.Add(3*time.Hour))
	writeBackupAt(t, dir, base, now.Add(time.Hour))

	if err := pruneLocalBackups(dir, base, now); err != nil {
		t.Fatalf("pruneLocalBackups: %v", err)
	}

	got := backupDirEntries(t, dir)
	if len(got) != 1 || got[0] != newest {
		t.Fatalf("remaining backups = %v, want only %s", got, newest)
	}
}

func TestBackupFileCreatesBackupAndPrunesMatchingBackups(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "provider_local.conf")
	backupDir := filepath.Join(dir, "backups")
	if err := os.WriteFile(src, []byte("current"), 0600); err != nil {
		t.Fatal(err)
	}

	for day := 1; day <= 10; day++ {
		writeBackupAt(t, backupDir, filepath.Base(src), time.Now().AddDate(0, 0, -day))
	}

	if err := backupFile(src, backupDir); err != nil {
		t.Fatalf("backupFile: %v", err)
	}

	var valid []string
	for _, name := range backupDirEntries(t, backupDir) {
		if strings.HasPrefix(name, filepath.Base(src)+".") && strings.HasSuffix(name, ".bak") {
			valid = append(valid, name)
		}
	}
	if len(valid) != 8 {
		t.Fatalf("valid provider_local backups = %d (%v), want 8", len(valid), valid)
	}
}

func writeBackupAt(t *testing.T, dir, base string, ts time.Time) string {
	t.Helper()

	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	name := base + "." + ts.Format(localBackupTimestampLayout) + ".bak"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(name), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, ts, ts); err != nil {
		t.Fatal(err)
	}
	return name
}

func backupDirEntries(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
