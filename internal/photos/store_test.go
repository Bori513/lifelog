package photos

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bori513/lifelog/internal/database"
	"github.com/Bori513/lifelog/internal/journal"
	"github.com/Bori513/lifelog/internal/profiles"
)

func uploadHeaders(t *testing.T, files map[string][]byte) []*multipart.FileHeader {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for name, data := range files {
		part, err := w.CreateFormFile("photos", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	if err := r.ParseMultipartForm(1024); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.MultipartForm.RemoveAll() })
	return r.MultipartForm.File["photos"]
}

func imageBytes(kind string) []byte {
	switch kind {
	case "jpeg":
		return []byte{0xff, 0xd8, 0xff, 0xe0, 0, 16, 'J', 'F', 'I', 'F', 0}
	case "png":
		return []byte("\x89PNG\r\n\x1a\nmore")
	case "webp":
		return []byte("RIFF\x04\x00\x00\x00WEBPVP8 ")
	default:
		return []byte(kind)
	}
}

func TestStageSupportedImagesAndRejectsUnsafeUploads(t *testing.T) {
	db, err := database.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db, t.TempDir())
	for _, test := range []struct{ name, kind, mime, ext string }{
		{"phone.weird", "jpeg", "image/jpeg", ".jpg"}, {"photo.jpg", "png", "image/png", ".png"}, {"photo.png", "webp", "image/webp", ".webp"},
	} {
		t.Run(test.kind, func(t *testing.T) {
			staged, err := store.Stage(uploadHeaders(t, map[string][]byte{test.name: imageBytes(test.kind)}), 3, "2026-08-29")
			if err != nil {
				t.Fatal(err)
			}
			defer store.CleanupStaged(staged)
			if len(staged) != 1 || staged[0].MIMEType != test.mime || filepath.Ext(staged[0].RelativePath) != test.ext {
				t.Fatalf("staged=%+v", staged)
			}
			if filepath.Base(staged[0].RelativePath) == test.name || !strings.HasPrefix(staged[0].RelativePath, filepath.Join("3", "2026", "08", "29")+string(filepath.Separator)) {
				t.Fatalf("unsafe stored path %q", staged[0].RelativePath)
			}
		})
	}
	for name, data := range map[string][]byte{"empty.jpg": {}, "fake.jpg": []byte("<html>not an image</html>"), "vector.svg": []byte("<svg></svg>")} {
		t.Run(name, func(t *testing.T) {
			if _, err := store.Stage(uploadHeaders(t, map[string][]byte{name: data}), 1, "2026-08-29"); err == nil {
				t.Fatal("invalid upload accepted")
			}
		})
	}
}

func TestStageLimits(t *testing.T) {
	db, _ := database.Open(t.TempDir())
	defer db.Close()
	store := NewStore(db, t.TempDir())
	large := append(imageBytes("jpeg"), make([]byte, MaxPhotoBytes)...)
	if _, err := store.Stage(uploadHeaders(t, map[string][]byte{"large.jpg": large}), 1, "2026-08-29"); err != ErrTooLarge {
		t.Fatalf("large error=%v", err)
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for i := 0; i < MaxNewPhotos+1; i++ {
		part, _ := w.CreateFormFile("photos", string(rune('a'+i))+".jpg")
		part.Write(imageBytes("jpeg"))
	}
	w.Close()
	r := httptest.NewRequest("POST", "/", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	r.ParseMultipartForm(1024)
	defer r.MultipartForm.RemoveAll()
	if _, err := store.Stage(r.MultipartForm.File["photos"], 1, "2026-08-29"); err != ErrTooMany {
		t.Fatalf("many error=%v", err)
	}
}

func TestPersistMetadataAndOwnership(t *testing.T) {
	dataDir := t.TempDir()
	db, err := database.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	profileStore := profiles.NewStore(db)
	p, err := profileStore.CreateProfile(context.Background(), profiles.CreateProfileInput{Name: "A", Timezone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	js, _ := profileStore.ListJournals(context.Background(), p.ID)
	day, err := journal.NewStore(db).SaveDay(context.Background(), js[0].ID, "2026-08-29", journal.SaveDayInput{})
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(db, dataDir)
	staged, err := store.Stage(uploadHeaders(t, map[string][]byte{"My photo ü.jpg": imageBytes("jpeg")}), p.ID, day.EntryDate)
	if err != nil {
		t.Fatal(err)
	}
	defer store.CleanupStaged(staged)
	if err := store.Persist(staged); err != nil {
		t.Fatal(err)
	}
	created, err := store.Add(context.Background(), p.ID, day.ID, staged)
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 || created[0].OriginalFilename != "My photo ü.jpg" || created[0].FileSize != int64(len(imageBytes("jpeg"))) {
		t.Fatalf("created=%+v", created)
	}
	path, _ := store.ResolvePath(created[0].RelativePath)
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	listed, err := store.ListForDay(context.Background(), p.ID, day.EntryDate)
	if err != nil || len(listed) != 1 {
		t.Fatalf("listed=%v err=%v", listed, err)
	}
	if _, err := store.Get(context.Background(), p.ID+999, created[0].ID); err != ErrNotFound {
		t.Fatalf("cross-user get=%v", err)
	}
}
