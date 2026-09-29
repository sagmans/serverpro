package doctor

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestReportSave(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	report := Report{Results: []Result{{Status: Fail, Evidence: strings.Repeat("x", 6000)}}}
	var want bytes.Buffer
	if err := report.Write(&want); err != nil {
		t.Fatal(err)
	}
	path, err := report.Save("team", "host")
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(temp)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(canonical, fmt.Sprintf("serverpro-%d", os.Getuid()), "doctor", "team", "host")
	if filepath.Dir(path) != dir {
		t.Fatalf("unexpected path %s", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, want.Bytes()) {
		t.Fatal("saved report differs from Report.Write")
	}
	for current := path; current != canonical; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0077 != 0 {
			t.Fatalf("nonprivate %s", current)
		}
	}
	for i := 0; i < 7; i++ {
		path, err = report.Save("team", "host")
		if err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Fatalf("got %d reports, want 5", len(entries))
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("deleted just-written report")
	}
}

func TestReportSaveRetentionPreservesForeignFiles(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	path, err := (Report{}).Save("team", "host")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(path)
	var preserved []string
	for i, kind := range []string{"public", "symlink", "directory", "hardlink", "unrelated"} {
		name := fmt.Sprintf("doctor-%032x.json", i)
		if kind == "unrelated" {
			name = "notes.json"
		}
		target := filepath.Join(dir, name)
		switch kind {
		case "symlink":
			err = os.Symlink(path, target)
		case "directory":
			err = os.Mkdir(target, 0700)
		case "hardlink":
			err = os.Link(path, target)
		default:
			err = os.WriteFile(target, []byte("preserve"), 0600)
		}
		if err != nil {
			t.Fatal(err)
		}
		if kind == "public" {
			if err := os.Chmod(target, 0644); err != nil {
				t.Fatal(err)
			}
		}
		preserved = append(preserved, target)
	}
	var recent []string
	for i := 0; i < 6; i++ {
		target := filepath.Join(dir, fmt.Sprintf("doctor-%032x.json", i+20))
		if err := os.WriteFile(target, nil, 0600); err != nil {
			t.Fatal(err)
		}
		stamp := time.Unix(int64(i+1), 0)
		if err := os.Chtimes(target, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		recent = append(recent, target)
	}
	latest, err := (Report{}).Save("team", "host")
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range append(preserved, latest) {
		if _, err := os.Lstat(target); err != nil {
			t.Fatalf("removed protected file %s: %v", target, err)
		}
	}
	for i, target := range recent {
		_, err := os.Stat(target)
		if i < 2 && !os.IsNotExist(err) {
			t.Fatalf("old report retained: %s", target)
		}
		if i >= 2 && err != nil {
			t.Fatalf("new report removed: %s", target)
		}
	}
}

func TestReportSaveTempAlias(t *testing.T) {
	temp := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(temp, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", alias)
	path, err := (Report{}).Save("team", "host")
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(temp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, canonical+string(filepath.Separator)) {
		t.Fatalf("noncanonical report path %s", path)
	}
}

func TestPrivateReportFileRejectsForeignOwner(t *testing.T) {
	var info unix.Stat_t
	if err := unix.Stat(t.TempDir(), &info); err != nil {
		t.Fatal(err)
	}
	info.Mode = unix.S_IFREG | reportFileMode
	info.Nlink = 1
	if !privateReportFile(info) {
		t.Fatal("rejected private owned report")
	}
	info.Uid++
	if privateReportFile(info) {
		t.Fatal("accepted foreign-owned report")
	}
}

func TestReportSaveRejectsUnsafeTempAncestor(t *testing.T) {
	for _, mode := range []os.FileMode{0777, 0770, 0777 | os.ModeSticky} {
		t.Run(mode.String(), func(t *testing.T) {
			ancestor := t.TempDir()
			base := filepath.Join(ancestor, "temp")
			if err := os.Mkdir(base, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(ancestor, mode); err != nil {
				t.Fatal(err)
			}
			t.Setenv("TMPDIR", base)
			if os.Getuid() == 0 && mode&os.ModeSticky != 0 {
				t.Skip("root-owned sticky ancestor is permitted")
			}
			if path, err := (Report{}).Save("team", "host"); err == nil || path != "" {
				t.Fatal("accepted writable temporary ancestor")
			}
		})
	}
}

func TestReportSaveConcurrentRetention(t *testing.T) {
	const writers = 24
	t.Setenv("TMPDIR", t.TempDir())
	start := make(chan struct{})
	paths := make(chan string, writers)
	failures := make(chan error, writers)
	var workers sync.WaitGroup
	for range writers {
		workers.Go(func() {
			<-start
			path, err := (Report{}).Save("team", "host")
			if err != nil {
				failures <- err
				return
			}
			paths <- path
		})
	}
	close(start)
	workers.Wait()
	close(paths)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	names := make(map[string]bool)
	var dir string
	for path := range paths {
		if names[path] {
			t.Errorf("duplicate path %s", path)
		}
		names[path] = true
		dir = filepath.Dir(path)
		if !strings.HasPrefix(filepath.Base(path), "doctor-") {
			t.Errorf("unexpected report filename %s", path)
		}
	}
	if len(names) != writers {
		t.Fatalf("saved %d reports, want %d", len(names), writers)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Fatalf("concurrent retention kept %d reports, want 5", len(entries))
	}
	for _, entry := range entries {
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var want bytes.Buffer
		if err := (Report{}).Write(&want); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(body, want.Bytes()) {
			t.Fatal("incomplete concurrent report")
		}
	}
}

func TestReportSaveRejectsTraversal(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	for _, name := range []string{"", ".", "..", "../escape", "a/b", "a\\b", "/absolute", "nul\x00name"} {
		for _, pair := range [][2]string{{name, "host"}, {"team", name}} {
			if path, err := (Report{}).Save(pair[0], pair[1]); err == nil || path != "" {
				t.Fatalf("accepted %q", pair)
			}
		}
	}
}

func TestReportSaveRejectsUnsafeDirectories(t *testing.T) {
	for _, level := range []int{0, 1, 2, 3} {
		for _, kind := range []string{"symlink", "public", "file", "unowned"} {
			t.Run(fmt.Sprintf("%d/%s", level, kind), func(t *testing.T) {
				temp := t.TempDir()
				t.Setenv("TMPDIR", temp)
				parts := []string{fmt.Sprintf("serverpro-%d", os.Getuid()), "doctor", "team", "host"}
				target := filepath.Join(append([]string{temp}, parts[:level+1]...)...)
				if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "symlink":
					if err := os.Symlink(t.TempDir(), target); err != nil {
						t.Fatal(err)
					}
				case "public":
					if err := os.Mkdir(target, reportDirectoryMode); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(target, 0755); err != nil {
						t.Fatal(err)
					}
				case "unowned":
					if err := os.Mkdir(target, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.Chown(target, os.Getuid()+1, -1); err != nil {
						if os.IsPermission(err) {
							t.Skip("foreign ownership requires privilege")
						}
						t.Fatal(err)
					}
				case "file":
					if err := os.WriteFile(target, nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
				before, _ := os.Lstat(target)
				if _, err := (Report{}).Save("team", "host"); err == nil {
					t.Fatal("accepted unsafe directory")
				}
				after, _ := os.Lstat(target)
				if before.Mode() != after.Mode() {
					t.Fatal("changed untrusted permissions")
				}
			})
		}
	}
}
