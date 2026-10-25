package engine

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVisibilityRejectsEmptyAndCorruptRuntimeFont(t *testing.T) {
	for _, damage := range []string{"empty", "same-size-corruption"} {
		t.Run(damage, func(t *testing.T) {
			s, g := xiaomiSelectorFixture(t)
			exposeFixtureGeneration(t, s, g)
			if err := os.MkdirAll(s.rom("/product/fonts"), 0700); err != nil {
				t.Fatal(err)
			}
			var victim string
			for name := range g.Fonts {
				if err := os.Link(s.rom("/system/fonts/"+name), s.rom("/product/fonts/"+name)); err != nil {
					t.Fatal(err)
				}
				victim = name
			}
			if err := s.verifyVisible(g); err != nil {
				t.Fatal("valid hardlinks", err)
			}
			path := s.rom("/product/fonts/" + victim)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if damage == "empty" {
				data = nil
			} else {
				data[0] ^= 0xff
			}
			// Replace one path, leaving the system mirror intact. A per-name
			// hash cache would miss this independent corrupt product copy.
			if err := writeAtomic(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := s.verifyVisible(g); err == nil {
				t.Fatal("corrupt font accepted")
			}
		})
	}
}

func beginTestUpload(t *testing.T, s *Store) string {
	t.Helper()
	v, err := s.Handle(Request{Op: "upload_begin", Label: "test.ttf", Size: 100})
	if err != nil {
		t.Fatal(err)
	}
	return v.(map[string]string)["token"]
}

func TestUploadCleanupRecoversFullQueueAndPreservesLibrary(t *testing.T) {
	s, a, _ := fixtureStore(t)
	before, err := os.ReadFile(s.data("library", a.ID+".font"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		beginTestUpload(t, s)
	}
	if _, err := s.Handle(Request{Op: "upload_begin", Size: 100}); err == nil {
		t.Fatal("queue should be full")
	}
	// A fresh process has no previous WebUI tokens but can still recover.
	reopened := *s
	v, err := reopened.Handle(Request{Op: "cleanup_uploads"})
	if err != nil || v.(map[string]int)["removed"] != 8 {
		t.Fatal(v, err)
	}
	beginTestUpload(t, &reopened)
	after, err := os.ReadFile(s.data("library", a.ID+".font"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("library changed", err)
	}
}

func TestUploadExpiryUsesLatestChunkAndIgnoresUnownedFiles(t *testing.T) {
	s, _, _ := fixtureStore(t)
	old := time.Now().Add(-48 * time.Hour)
	stale, active := beginTestUpload(t, s), beginTestUpload(t, s)
	for _, id := range []string{stale, active} {
		for _, suffix := range []string{"", ".json"} {
			if err := os.Chtimes(s.data("uploads", id+suffix), old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := s.Handle(Request{Op: "upload_chunk", Token: active, Chunk: base64.StdEncoding.EncodeToString([]byte{1, 2, 3})}); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "keep")
	if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	link := s.data("uploads", token())
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	unknown := s.data("uploads", "unowned.txt")
	if err := os.WriteFile(unknown, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	beginTestUpload(t, s) // Automatic cleanup.
	if _, err := os.Stat(s.data("uploads", stale)); !os.IsNotExist(err) {
		t.Fatal("stale upload retained", err)
	}
	if _, err := os.Stat(s.data("uploads", active)); err != nil {
		t.Fatal("active upload removed", err)
	}
	if _, err := s.cleanupUploads(time.Now(), true); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{outside, link, unknown} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatal("unowned path removed", path, err)
		}
	}
}

func TestUnconfirmedBootStaysBlockedUntilNewGeneration(t *testing.T) {
	s, _, _ := fixtureStore(t)
	generation := token()
	if blocked, err := s.bootAttemptBlocked(generation, "boot-a"); err != nil || blocked {
		t.Fatal(blocked, err)
	}
	if err := s.startBootAttempt(generation, "boot-a"); err != nil {
		t.Fatal(err)
	}
	for _, bid := range []string{"boot-b", "boot-c"} {
		if blocked, err := s.bootAttemptBlocked(generation, bid); err != nil || !blocked {
			t.Fatal(blocked, err)
		}
		if err := writeJSON(s.data("boot.json"), BootRecord{BootID: bid, Generation: generation, State: "guarded"}); err != nil {
			t.Fatal(err)
		}
		if confirmed, err := s.confirmBootAttempt(bid); err != nil || confirmed {
			t.Fatal("system fallback confirmed a failed generation", confirmed, err)
		}
	}
	if blocked, err := s.bootAttemptBlocked(token(), "boot-d"); err != nil || blocked {
		t.Fatal("new submission blocked", blocked, err)
	}
}

func TestBootConfirmationRequiresMatchingMountedBoot(t *testing.T) {
	s, _, _ := fixtureStore(t)
	generation := token()
	if err := s.startBootAttempt(generation, "boot-a"); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(s.data("boot.json"), BootRecord{BootID: "boot-a", Generation: generation, State: "mounted"}); err != nil {
		t.Fatal(err)
	}
	if confirmed, err := s.confirmBootAttempt("boot-b"); err != nil || confirmed {
		t.Fatal(confirmed, err)
	}
	for i := 0; i < 2; i++ {
		if confirmed, err := s.confirmBootAttempt("boot-a"); err != nil || !confirmed {
			t.Fatal(confirmed, err)
		}
	}
	if blocked, err := s.bootAttemptBlocked(generation, "boot-b"); err != nil || blocked {
		t.Fatal(blocked, err)
	}
	if err := s.startBootAttempt(generation, "boot-b"); err != nil {
		t.Fatal(err)
	}
	if blocked, err := s.bootAttemptBlocked(generation, "boot-c"); err != nil || !blocked {
		t.Fatal("every boot must be confirmed", blocked, err)
	}
}

func TestBootGuardRejectsMalformedRecord(t *testing.T) {
	s, _, _ := fixtureStore(t)
	if err := writeJSON(s.data("boot-attempt.json"), BootAttempt{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.bootAttemptBlocked(token(), "boot-a"); err == nil {
		t.Fatal("invalid record accepted")
	}
	if _, err := s.bootAttemptBlocked(token(), ""); err == nil {
		t.Fatal("missing boot ID accepted")
	}
}
