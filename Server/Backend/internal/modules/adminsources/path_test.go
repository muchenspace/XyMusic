package adminsources

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLibrarySourcePathsResolveRelativeToExecutableRoot(t *testing.T) {
	root := t.TempDir()
	music := filepath.Join(root, "music")
	child := filepath.Join(music, "Album")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	mutation, err := validateRootInput(root, RootMutation{
		Name: " Music ", Path: "music", Mode: RootModeReadOnly, Enabled: true,
		IncludePatterns: []string{}, ExcludePatterns: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if mutation.Name != "Music" || mutation.Path != music || mutation.NormalizedPath != normalizeRootPath(music) {
		t.Fatalf("validated mutation=%+v", mutation)
	}
	browse, err := browseDirectory(root, "music", 1, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if browse.Path != music || len(browse.Directories) != 1 || browse.Directories[0].Path != child ||
		browse.Page != 1 || browse.PageSize != 100 || browse.Total != 1 || browse.TotalPages != 1 {
		t.Fatalf("browse=%+v", browse)
	}
}

func TestBrowseDirectorySortsAndPaginatesWithoutTruncation(t *testing.T) {
	root := t.TempDir()
	for index := 0; index < 505; index++ {
		name := fmt.Sprintf("dir-%03d", index)
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	browse, err := browseDirectory(root, "", 6, 100, 500)
	if err != nil {
		t.Fatal(err)
	}
	if browse.Total != 505 || browse.TotalPages != 6 || len(browse.Directories) != 5 ||
		browse.Directories[0].Name != "dir-500" || browse.Directories[4].Name != "dir-504" {
		t.Fatalf("browse=%+v", browse)
	}
}

func TestLibraryGlobMatchesLegacyDoubleStarSemantics(t *testing.T) {
	tests := []struct {
		pattern string
		match   []string
		reject  []string
	}{
		{"**/*.flac", []string{"song.flac", "album/song.flac"}, []string{"song.mp3"}},
		{"Album/?rack*.mp3", []string{"Album/Track 1.mp3"}, []string{"Album/sub/Track.mp3"}},
		{"music/**", []string{"music/a.mp3", "music/sub/a.flac"}, []string{"other/a.mp3"}},
	}
	for _, item := range tests {
		pattern, err := compileLibraryGlob(item.pattern)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range item.match {
			if !pattern.MatchString(value) {
				t.Errorf("%q should match %q", item.pattern, value)
			}
		}
		for _, value := range item.reject {
			if pattern.MatchString(value) {
				t.Errorf("%q should reject %q", item.pattern, value)
			}
		}
	}
}

func TestResolveLibraryFileRejectsTraversalAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "inside.flac")
	outside := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-outside.flac")
	if err := os.WriteFile(inside, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	if _, err := resolveFileWithinRoot(root, inside); err != nil {
		t.Fatalf("inside path rejected: %v", err)
	}
	if _, err := resolveFileWithinRoot(root, outside); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("outside path err=%v", err)
	}
	link := filepath.Join(root, "link.flac")
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("Windows symlink creation is unavailable")
		}
		t.Fatal(err)
	}
	if _, err := resolveFileWithinRoot(root, link); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("symlink escape err=%v", err)
	}
}
