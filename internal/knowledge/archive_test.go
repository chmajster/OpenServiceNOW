package knowledge

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func makeArchive(t *testing.T, manifest archiveManifest, extras map[string][]byte) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeArchiveFile(writer, "manifest.json", encoded, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	for name, content := range extras {
		if err := writeArchiveFile(writer, name, content, time.Unix(1, 0)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestReadArchiveValidatesAndLoadsHierarchy(t *testing.T) {
	manifest := archiveManifest{
		Format: archiveFormat,
		Space:  archiveSpace{Key: "OPS", Name: "Operacje", ReviewIntervalDays: 90},
		Pages: []archivePage{
			{SourceID: "root", Title: "Start", Status: PagePublished, File: "pages/root.md"},
			{SourceID: "child", ParentSourceID: "root", Title: "Procedura", Status: PageDraft, File: "pages/child.md"},
		},
	}
	loaded, err := readArchive(makeArchive(t, manifest, map[string][]byte{
		"pages/root.md":  []byte("# Start"),
		"pages/child.md": []byte("Treść"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Pages) != 2 || loaded.Pages[1].Body != "# Start" && loaded.Pages[0].Body != "# Start" {
		t.Fatalf("unexpected archive contents: %#v", loaded.Pages)
	}
}

func TestReadArchiveRejectsCyclesUnknownFilesAndTraversal(t *testing.T) {
	cyclic := archiveManifest{
		Format: archiveFormat,
		Space:  archiveSpace{Key: "OPS", Name: "Operacje"},
		Pages: []archivePage{
			{SourceID: "a", ParentSourceID: "b", Title: "A", Status: PageDraft, File: "pages/a.md"},
			{SourceID: "b", ParentSourceID: "a", Title: "B", Status: PageDraft, File: "pages/b.md"},
		},
	}
	if _, err := readArchive(makeArchive(t, cyclic, map[string][]byte{"pages/a.md": {}, "pages/b.md": {}})); !errors.Is(err, ErrInvalid) {
		t.Fatalf("cycle error = %v", err)
	}

	valid := archiveManifest{
		Format: archiveFormat,
		Space:  archiveSpace{Key: "OPS", Name: "Operacje"},
		Pages:  []archivePage{{SourceID: "a", Title: "A", Status: PageDraft, File: "pages/a.md"}},
	}
	if _, err := readArchive(makeArchive(t, valid, map[string][]byte{"pages/a.md": {}, "unexpected.txt": {}})); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown file error = %v", err)
	}
	if _, err := readArchive(makeArchive(t, valid, map[string][]byte{"../pages/a.md": {}})); !errors.Is(err, ErrInvalid) {
		t.Fatalf("traversal error = %v", err)
	}
}
