package web

import (
	"bytes"
	"errors"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Bori513/lifelog/internal/database"
	"github.com/Bori513/lifelog/internal/journal"
	"github.com/Bori513/lifelog/internal/photos"
	"github.com/Bori513/lifelog/internal/profiles"
	"github.com/Bori513/lifelog/internal/questions"
)

type testApp struct {
	t         *testing.T
	s         *Server
	profiles  *profiles.Store
	questions *questions.Store
	journal   *journal.Store
	photos    *photos.Store
	dataDir   string
	cookies   map[string]*http.Cookie
}

func newTestApp(t *testing.T) *testApp {
	t.Helper()
	dataDir := t.TempDir()
	db, err := database.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := New(db, dataDir, false, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	return &testApp{t: t, s: s, profiles: profiles.NewStore(db), questions: questions.NewStore(db), journal: journal.NewStore(db), photos: photos.NewStore(db, dataDir), dataDir: dataDir, cookies: map[string]*http.Cookie{}}
}

func (a *testApp) request(method, path string, form url.Values) *httptest.ResponseRecorder {
	a.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, path, body)
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, c := range a.cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.s.Handler().ServeHTTP(w, r)
	for _, c := range w.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(a.cookies, c.Name)
		} else {
			a.cookies[c.Name] = c
		}
	}
	return w
}

func (a *testApp) multipartRequest(path string, values url.Values, files map[string][]byte) *httptest.ResponseRecorder {
	a.t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for key, entries := range values {
		for _, value := range entries {
			if err := w.WriteField(key, value); err != nil {
				a.t.Fatal(err)
			}
		}
	}
	for name, data := range files {
		part, err := w.CreateFormFile("photos", name)
		if err != nil {
			a.t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			a.t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		a.t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, path, &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	for _, c := range a.cookies {
		r.AddCookie(c)
	}
	rr := httptest.NewRecorder()
	a.s.Handler().ServeHTTP(rr, r)
	for _, c := range rr.Result().Cookies() {
		a.cookies[c.Name] = c
	}
	return rr
}

func testJPEG() []byte { return []byte{0xff, 0xd8, 0xff, 0xe0, 0, 16, 'J', 'F', 'I', 'F', 0} }

func (a *testApp) getToken(path string) (string, *httptest.ResponseRecorder) {
	w := a.request(http.MethodGet, path, nil)
	re := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)
	match := re.FindStringSubmatch(w.Body.String())
	if len(match) != 2 {
		a.t.Fatalf("no CSRF token in %s: %s", path, w.Body.String())
	}
	return match[1], w
}

func (a *testApp) create(name, pin, tz string) profiles.Profile {
	a.t.Helper()
	p, err := a.profiles.CreateProfile(a.t.Context(), profiles.CreateProfileInput{Name: name, PIN: pin, Timezone: tz})
	if err != nil {
		a.t.Fatal(err)
	}
	return p
}

func (a *testApp) loginProfile(id int64) {
	a.t.Helper()
	session, err := a.profiles.CreateSession(a.t.Context(), id, sessionTTL)
	if err != nil {
		a.t.Fatal(err)
	}
	a.cookies[sessionCookie] = &http.Cookie{Name: sessionCookie, Value: session.Token}
}

func TestPWAAssetsAndMetadata(t *testing.T) {
	a := newTestApp(t)

	tests := []struct {
		path        string
		contentType string
		body        string
	}{
		{"/manifest.webmanifest", "application/manifest+json", `"display": "standalone"`},
		{"/sw.js", "text/javascript", `const CACHE_NAME = "lifelog-static-v1"`},
		{"/offline.html", "text/html", "Connect to your LifeLog server"},
	}
	for _, tt := range tests {
		w := a.request(http.MethodGet, tt.path, nil)
		if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), tt.contentType) || !strings.Contains(w.Body.String(), tt.body) {
			t.Errorf("GET %s: code=%d type=%q body=%s", tt.path, w.Code, w.Header().Get("Content-Type"), w.Body.String())
		}
	}

	for _, path := range []string{"/static/icon-192.png", "/static/icon-512.png", "/static/apple-touch-icon.png"} {
		w := a.request(http.MethodGet, path, nil)
		if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" || len(w.Body.Bytes()) == 0 {
			t.Errorf("GET %s: code=%d type=%q bytes=%d", path, w.Code, w.Header().Get("Content-Type"), len(w.Body.Bytes()))
		}
	}

	w := a.request(http.MethodGet, "/", nil)
	body := w.Body.String()
	for _, metadata := range []string{`rel="manifest" href="/manifest.webmanifest"`, `name="theme-color" content="#27684e"`, `rel="apple-touch-icon"`, `viewport-fit=cover`} {
		if !strings.Contains(body, metadata) {
			t.Errorf("base HTML missing %q", metadata)
		}
	}

	w = a.request(http.MethodGet, "/day/2026-08-29", nil)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("PWA assets changed authentication: code=%d location=%q", w.Code, w.Header().Get("Location"))
	}
}

func TestHealthChecksDatabaseWithoutAuthentication(t *testing.T) {
	a := newTestApp(t)
	w := a.request(http.MethodGet, "/healthz", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", w.Code, http.StatusOK)
	}
	if got := w.Body.String(); got != "ok\n" {
		t.Fatalf("health body = %q, want %q", got, "ok\n")
	}
	if got := w.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("health content type = %q", got)
	}
}

func TestHealthReportsUnavailableDatabase(t *testing.T) {
	a := newTestApp(t)
	if err := a.s.db.Close(); err != nil {
		t.Fatal(err)
	}

	w := a.request(http.MethodGet, "/healthz", nil)
	if w.Code != http.StatusServiceUnavailable || w.Body.String() != "unavailable\n" {
		t.Fatalf("health response: status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestFirstRunCreatesProfileJournalAndSession(t *testing.T) {
	a := newTestApp(t)
	token, w := a.getToken("/")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Create your profile") {
		t.Fatalf("first run: code=%d body=%s", w.Code, w.Body.String())
	}
	w = a.request(http.MethodPost, "/profiles", url.Values{"csrf_token": {token}, "name": {"Boris"}, "timezone": {"Europe/Bratislava"}})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/today" {
		t.Fatalf("create redirect: %d %s", w.Code, w.Header().Get("Location"))
	}
	ps, err := a.profiles.ListProfiles(t.Context())
	if err != nil || len(ps) != 1 {
		t.Fatalf("profiles=%v err=%v", ps, err)
	}
	js, err := a.profiles.ListJournals(t.Context(), ps[0].ID)
	if err != nil || len(js) != 1 || js[0].Name != "Personal" {
		t.Fatalf("journals=%v err=%v", js, err)
	}
	if _, ok := a.cookies[sessionCookie]; !ok {
		t.Fatal("session cookie missing")
	}
}

func TestAuthenticatedSearchPageAndEscaping(t *testing.T) {
	a := newTestApp(t)
	p := a.create("Searcher", "", "UTC")
	a.loginProfile(p.ID)
	js, err := a.profiles.ListJournals(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.journal.SaveDay(t.Context(), js[0].ID, "2026-08-28", journal.SaveDayInput{GeneralNote: `<script>alert("x")</script> McDonald's`})
	if err != nil {
		t.Fatal(err)
	}

	w := a.request(http.MethodGet, "/search?q=McDonald%27s", nil)
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, "Search journal") || !strings.Contains(body, `href="/day/2026-08-28"`) {
		t.Fatalf("search page code=%d body=%s", w.Code, body)
	}
	if strings.Contains(body, "<script>alert") || !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("search result was not escaped: %s", body)
	}
	if !strings.Contains(body, `value="McDonald&#39;s"`) {
		t.Fatalf("query was not preserved safely: %s", body)
	}
	w = a.request(http.MethodGet, "/search?q=absent", nil)
	if !strings.Contains(w.Body.String(), "No matching journal entries.") {
		t.Fatal("no-match state missing")
	}
	w = a.request(http.MethodGet, "/search", nil)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "No matching journal entries.") {
		t.Fatal("empty search state invalid")
	}

	b := newTestApp(t)
	w = b.request(http.MethodGet, "/search", nil)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatal("unauthenticated search was not protected")
	}
}

func TestProfileSelectionAndPINLogin(t *testing.T) {
	a := newTestApp(t)
	open := a.create("Open", "", "UTC")
	locked := a.create("Locked", "1234", "UTC")
	token, w := a.getToken("/profiles")
	if !strings.Contains(w.Body.String(), "Open") || !strings.Contains(w.Body.String(), "Locked") {
		t.Fatal("profiles not rendered")
	}
	w = a.request(http.MethodPost, "/profiles/select", url.Values{"csrf_token": {token}, "profile_id": {"2"}})
	if w.Header().Get("Location") != "/login?profile=2" {
		t.Fatalf("locked redirect=%s", w.Header().Get("Location"))
	}
	token, _ = a.getToken("/login?profile=2")
	w = a.request(http.MethodPost, "/login", url.Values{"csrf_token": {token}, "profile_id": {"2"}, "pin": {"bad"}})
	if !strings.Contains(w.Body.String(), "not accepted") {
		t.Fatal("missing generic login error")
	}
	w = a.request(http.MethodPost, "/login", url.Values{"csrf_token": {token}, "profile_id": {"2"}, "pin": {"1234"}})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/today" {
		t.Fatal("correct PIN did not login")
	}
	delete(a.cookies, sessionCookie)
	token, _ = a.getToken("/profiles")
	w = a.request(http.MethodPost, "/profiles/select", url.Values{"csrf_token": {token}, "profile_id": {url.QueryEscape("1")}})
	if w.Header().Get("Location") != "/today" {
		t.Fatal("no-PIN selection failed")
	}
	_ = open
	_ = locked
}

func TestRootDoesNotAutoLoginProtectedOrMultipleProfiles(t *testing.T) {
	protected := newTestApp(t)
	protected.create("Locked", "1234", "UTC")
	w := protected.request(http.MethodGet, "/", nil)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/profiles" {
		t.Fatalf("protected profile auto-login: %d %s", w.Code, w.Header().Get("Location"))
	}

	multiple := newTestApp(t)
	multiple.create("One", "", "UTC")
	multiple.create("Two", "", "UTC")
	w = multiple.request(http.MethodGet, "/", nil)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/profiles" {
		t.Fatalf("multiple profile auto-login: %d %s", w.Code, w.Header().Get("Location"))
	}
}

func TestAutoSelectionAuthenticationTimezoneAndLogout(t *testing.T) {
	a := newTestApp(t)
	p := a.create("Solo", "", "Pacific/Kiritimati")
	w := a.request(http.MethodGet, "/", nil)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/today" {
		t.Fatal("single profile was not auto-selected")
	}
	a.s.SetClock(func() time.Time { return time.Date(2026, 8, 29, 12, 30, 0, 0, time.FixedZone("UTC", 0)) })
	w = a.request(http.MethodGet, "/today", nil)
	if w.Header().Get("Location") != "/day/2026-08-30" {
		t.Fatalf("timezone date=%s", w.Header().Get("Location"))
	}
	token, _ := a.getToken("/day/2026-08-30")
	w = a.request(http.MethodPost, "/logout", url.Values{"csrf_token": {token}})
	if w.Header().Get("Location") != "/profiles" {
		t.Fatal("logout redirect")
	}
	w = a.request(http.MethodGet, "/day/2026-08-30", nil)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatal("unauthenticated day allowed")
	}
	_ = p
}

func TestDayRenderSaveClearAndHistoricalQuestion(t *testing.T) {
	a := newTestApp(t)
	p := a.create("Writer", "", "UTC")
	js, _ := a.profiles.ListJournals(t.Context(), p.ID)
	jid := js[0].ID
	textQ, _ := a.questions.CreateQuestion(t.Context(), jid, questions.CreateQuestionInput{Label: "<b>Mood</b>", Type: questions.QuestionTypeShortText})
	boolQ, _ := a.questions.CreateQuestion(t.Context(), jid, questions.CreateQuestionInput{Label: "Good day?", Type: questions.QuestionTypeBoolean})
	multiQ, _ := a.questions.CreateQuestion(t.Context(), jid, questions.CreateQuestionInput{Label: "People", Type: questions.QuestionTypeMultiSelect})
	opt1, _ := a.questions.CreateOption(t.Context(), jid, multiQ.ID, questions.CreateOptionInput{Label: "Family"})
	opt2, _ := a.questions.CreateOption(t.Context(), jid, multiQ.ID, questions.CreateOptionInput{Label: "Friends"})
	inactive, _ := a.questions.CreateQuestion(t.Context(), jid, questions.CreateQuestionInput{Label: "Old label", Type: questions.QuestionTypeShortText})
	old := "kept"
	a.journal.SaveDay(t.Context(), jid, "2026-08-29", journal.SaveDayInput{Answers: []journal.AnswerInput{{QuestionID: inactive.ID, TextValue: &old}}})
	a.questions.RenameQuestion(t.Context(), jid, inactive.ID, questions.RenameQuestionInput{Label: "New label"})
	a.questions.DeactivateQuestion(t.Context(), jid, inactive.ID)
	a.loginProfile(p.ID)
	token, w := a.getToken("/day/2026-08-29")
	body := w.Body.String()
	if strings.Contains(body, "<b>Mood</b>") || !strings.Contains(body, "&lt;b&gt;Mood&lt;/b&gt;") || !strings.Contains(body, "Old label") || !strings.Contains(body, "Inactive") {
		t.Fatalf("rendering/historical failure: %s", body)
	}
	form := url.Values{"csrf_token": {token}, "general_note": {"hello"}, "q_1_present": {"1"}, "q_1": {"fine"}, "q_2_present": {"1"}, "q_2": {"false"}, "q_3_present": {"1"}, "q_3": {"1", "2"}, "q_4_present": {"1"}, "q_4": {"kept"}}
	w = a.request(http.MethodPost, "/day/2026-08-29", form)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	day, _ := a.journal.GetDay(t.Context(), jid, "2026-08-29")
	if day.GeneralNote != "hello" || len(day.Answers) != 4 {
		t.Fatalf("saved day=%+v", day)
	}
	var foundFalse bool
	for _, answer := range day.Answers {
		if answer.QuestionID == boolQ.ID && answer.BoolValue != nil && !*answer.BoolValue {
			foundFalse = true
		}
		if answer.QuestionID == multiQ.ID && len(answer.SelectedOptions) != 2 {
			t.Fatal("multi-select not saved")
		}
	}
	if !foundFalse {
		t.Fatal("boolean false not preserved")
	}
	token, _ = a.getToken("/day/2026-08-29")
	form = url.Values{"csrf_token": {token}, "q_1_present": {"1"}, "q_1": {""}, "q_2_present": {"1"}, "q_2": {""}, "q_3_present": {"1"}, "q_4_present": {"1"}, "q_4": {"kept"}}
	a.request(http.MethodPost, "/day/2026-08-29", form)
	day, _ = a.journal.GetDay(t.Context(), jid, "2026-08-29")
	if len(day.Answers) != 1 {
		t.Fatalf("clears left %d answers", len(day.Answers))
	}
	_ = textQ
	_ = opt1
	_ = opt2
}

func TestCSRFAndBodyLimit(t *testing.T) {
	a := newTestApp(t)
	a.create("Writer", "", "UTC")
	a.loginProfile(1)
	w := a.request(http.MethodPost, "/day/2026-08-29", url.Values{})
	if w.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF=%d", w.Code)
	}
	token, _ := a.getToken("/day/2026-08-29")
	w = a.request(http.MethodPost, "/day/2026-08-29", url.Values{"csrf_token": {token + "x"}})
	if w.Code != http.StatusForbidden {
		t.Fatalf("bad CSRF=%d", w.Code)
	}
	r := httptest.NewRequest(http.MethodPost, "/day/2026-08-29", strings.NewReader("csrf_token="+token+"&general_note="+strings.Repeat("x", maxFormBytes)))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range a.cookies {
		r.AddCookie(c)
	}
	rr := httptest.NewRecorder()
	a.s.Handler().ServeHTTP(rr, r)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large body=%d", rr.Code)
	}
}

func TestExpiredSessionAndUserIsolation(t *testing.T) {
	a := newTestApp(t)
	userA := a.create("A", "", "UTC")
	userB := a.create("B", "", "UTC")
	journalsA, _ := a.profiles.ListJournals(t.Context(), userA.ID)
	journalsB, _ := a.profiles.ListJournals(t.Context(), userB.ID)
	qA, _ := a.questions.CreateQuestion(t.Context(), journalsA[0].ID, questions.CreateQuestionInput{Label: "A only", Type: questions.QuestionTypeShortText})
	qB, _ := a.questions.CreateQuestion(t.Context(), journalsB[0].ID, questions.CreateQuestionInput{Label: "B only", Type: questions.QuestionTypeShortText})
	a.loginProfile(userA.ID)
	token, w := a.getToken("/day/2026-08-29")
	if strings.Contains(w.Body.String(), "B only") || !strings.Contains(w.Body.String(), "A only") {
		t.Fatal("cross-user question visibility")
	}
	form := url.Values{"csrf_token": {token}, "journal_id": {strconv.FormatInt(journalsB[0].ID, 10)}, "q_1_present": {"1"}, "q_1": {"mine"}, "q_2_present": {"1"}, "q_2": {"intrusion"}}
	w = a.request(http.MethodPost, "/day/2026-08-29", form)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("save code=%d", w.Code)
	}
	dayA, _ := a.journal.GetDay(t.Context(), journalsA[0].ID, "2026-08-29")
	dayB, _ := a.journal.GetDay(t.Context(), journalsB[0].ID, "2026-08-29")
	if len(dayA.Answers) != 1 || dayA.Answers[0].QuestionID != qA.ID || dayB.Exists {
		t.Fatal("browser-controlled IDs crossed ownership")
	}
	_ = qB

	expired, err := a.profiles.CreateSession(t.Context(), userA.ID, time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	a.cookies[sessionCookie] = &http.Cookie{Name: sessionCookie, Value: expired.Token}
	w = a.request(http.MethodGet, "/day/2026-08-29", nil)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatal("expired session accepted")
	}
}

func TestInvalidWholeDaySaveIsAtomic(t *testing.T) {
	a := newTestApp(t)
	p := a.create("Writer", "", "UTC")
	js, _ := a.profiles.ListJournals(t.Context(), p.ID)
	q, _ := a.questions.CreateQuestion(t.Context(), js[0].ID, questions.CreateQuestionInput{Label: "Energy", Type: questions.QuestionTypeScale5})
	valid := float64(3)
	if _, err := a.journal.SaveDay(t.Context(), js[0].ID, "2026-08-29", journal.SaveDayInput{GeneralNote: "original", Answers: []journal.AnswerInput{{QuestionID: q.ID, NumberValue: &valid}}}); err != nil {
		t.Fatal(err)
	}
	a.loginProfile(p.ID)
	token, _ := a.getToken("/day/2026-08-29")
	w := a.request(http.MethodPost, "/day/2026-08-29", url.Values{"csrf_token": {token}, "general_note": {"changed"}, "q_1_present": {"1"}, "q_1": {"9"}})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Could not save this day") {
		t.Fatalf("invalid save response=%d", w.Code)
	}
	day, _ := a.journal.GetDay(t.Context(), js[0].ID, "2026-08-29")
	if day.GeneralNote != "original" || len(day.Answers) != 1 || day.Answers[0].NumberValue == nil || *day.Answers[0].NumberValue != 3 {
		t.Fatalf("failed save partially persisted: %+v", day)
	}
}

func TestPhotoUploadServingReopenRemovalAndIsolation(t *testing.T) {
	a := newTestApp(t)
	userA := a.create("Photo owner", "", "UTC")
	userB := a.create("Other user", "", "UTC")
	a.loginProfile(userA.ID)
	token, w := a.getToken("/day/2026-08-29")
	if w.Code != http.StatusOK {
		t.Fatalf("empty day=%d", w.Code)
	}
	journals, _ := a.profiles.ListJournals(t.Context(), userA.ID)
	before, _ := a.journal.GetDay(t.Context(), journals[0].ID, "2026-08-29")
	if before.Exists {
		t.Fatal("viewing an empty day created it")
	}
	w = a.multipartRequest("/day/2026-08-29", url.Values{"csrf_token": {token}, "general_note": {"with photo"}}, map[string][]byte{"Trip photo ü.jpg": testJPEG()})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/day/2026-08-29?saved=1" {
		t.Fatalf("upload=%d %s", w.Code, w.Body.String())
	}
	day, _ := a.journal.GetDay(t.Context(), journals[0].ID, "2026-08-29")
	items, err := a.photos.ListForDay(t.Context(), userA.ID, "2026-08-29")
	if err != nil || !day.Exists || day.GeneralNote != "with photo" || len(items) != 1 {
		t.Fatalf("day=%+v photos=%+v err=%v", day, items, err)
	}
	if items[0].OriginalFilename != "Trip photo ü.jpg" || items[0].MIMEType != "image/jpeg" || items[0].FileSize != int64(len(testJPEG())) {
		t.Fatalf("metadata=%+v", items[0])
	}
	w = a.request(http.MethodGet, "/day/2026-08-29", nil)
	photoURL := "/photos/" + strconv.FormatInt(items[0].ID, 10)
	if !strings.Contains(w.Body.String(), photoURL) {
		t.Fatal("reopened day did not list photo")
	}
	w = a.request(http.MethodGet, photoURL, nil)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/jpeg" || !bytes.Equal(w.Body.Bytes(), testJPEG()) {
		t.Fatalf("serve=%d type=%q body=%v", w.Code, w.Header().Get("Content-Type"), w.Body.Bytes())
	}

	a.loginProfile(userB.ID)
	w = a.request(http.MethodGet, photoURL, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-user photo view=%d", w.Code)
	}
	token, _ = a.getToken("/day/2026-08-29")
	w = a.multipartRequest("/day/2026-08-29", url.Values{"csrf_token": {token}, "remove_photo": {strconv.FormatInt(items[0].ID, 10)}}, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-user removal=%d", w.Code)
	}
	if _, err := a.photos.Get(t.Context(), userA.ID, items[0].ID); err != nil {
		t.Fatalf("owner photo removed by other user: %v", err)
	}

	a.loginProfile(userA.ID)
	token, _ = a.getToken("/day/2026-08-29")
	w = a.multipartRequest("/day/2026-08-29", url.Values{"csrf_token": {token}, "general_note": {"with photo"}, "remove_photo": {strconv.FormatInt(items[0].ID, 10)}}, nil)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("owner removal=%d %s", w.Code, w.Body.String())
	}
	if _, err := a.photos.Get(t.Context(), userA.ID, items[0].ID); !errors.Is(err, photos.ErrNotFound) {
		t.Fatalf("removed metadata lookup=%v", err)
	}
}

func TestInvalidPhotoDoesNotSaveDay(t *testing.T) {
	a := newTestApp(t)
	p := a.create("Writer", "", "UTC")
	a.loginProfile(p.ID)
	token, _ := a.getToken("/day/2026-08-30")
	w := a.multipartRequest("/day/2026-08-30", url.Values{"csrf_token": {token}, "general_note": {"must not save"}}, map[string][]byte{"fake.jpg": []byte("<html>not a photo</html>")})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Only JPEG, PNG, and WebP") {
		t.Fatalf("invalid photo=%d %s", w.Code, w.Body.String())
	}
	journals, _ := a.profiles.ListJournals(t.Context(), p.ID)
	day, _ := a.journal.GetDay(t.Context(), journals[0].ID, "2026-08-30")
	if day.Exists {
		t.Fatal("invalid photo partially saved day")
	}
}

func TestInvalidAnswerWithValidPhotoSavesNeither(t *testing.T) {
	a := newTestApp(t)
	p := a.create("Writer", "", "UTC")
	js, _ := a.profiles.ListJournals(t.Context(), p.ID)
	q, _ := a.questions.CreateQuestion(t.Context(), js[0].ID, questions.CreateQuestionInput{Label: "Energy", Type: questions.QuestionTypeScale5})
	valid := float64(3)
	if _, err := a.journal.SaveDay(t.Context(), js[0].ID, "2026-08-29", journal.SaveDayInput{GeneralNote: "original", Answers: []journal.AnswerInput{{QuestionID: q.ID, NumberValue: &valid}}}); err != nil {
		t.Fatal(err)
	}
	a.loginProfile(p.ID)
	token, _ := a.getToken("/day/2026-08-29")
	prefix := "q_" + strconv.FormatInt(q.ID, 10)
	w := a.multipartRequest("/day/2026-08-29", url.Values{"csrf_token": {token}, "general_note": {"changed"}, prefix + "_present": {"1"}, prefix: {"9"}}, map[string][]byte{"valid.jpg": testJPEG()})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Could not save this day") {
		t.Fatalf("invalid save=%d %s", w.Code, w.Body.String())
	}
	day, _ := a.journal.GetDay(t.Context(), js[0].ID, "2026-08-29")
	items, _ := a.photos.ListForDay(t.Context(), p.ID, "2026-08-29")
	if day.GeneralNote != "original" || len(items) != 0 {
		t.Fatalf("partial save day=%+v photos=%+v", day, items)
	}
}

func TestOneInvalidPhotoRejectsAllPhotos(t *testing.T) {
	a := newTestApp(t)
	p := a.create("Writer", "", "UTC")
	a.loginProfile(p.ID)
	token, _ := a.getToken("/day/2026-08-31")
	w := a.multipartRequest("/day/2026-08-31", url.Values{"csrf_token": {token}}, map[string][]byte{"valid.jpg": testJPEG(), "bad.jpg": []byte("not an image")})
	if w.Code != http.StatusOK {
		t.Fatalf("mixed upload=%d", w.Code)
	}
	items, _ := a.photos.ListForDay(t.Context(), p.ID, "2026-08-31")
	if len(items) != 0 {
		t.Fatalf("mixed upload persisted photos=%+v", items)
	}
}

func TestQuestionManagementAuthenticationEmptyStateAndValidation(t *testing.T) {
	a := newTestApp(t)
	p := a.create("Writer", "", "UTC")
	w := a.request(http.MethodGet, "/questions", nil)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("unauthenticated questions: %d %s", w.Code, w.Header().Get("Location"))
	}
	a.loginProfile(p.ID)
	token, w := a.getToken("/questions")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "No active questions yet") || !strings.Contains(w.Body.String(), "Multiple choice") {
		t.Fatalf("question empty state: code=%d body=%s", w.Code, w.Body.String())
	}
	_, day := a.getToken("/day/2026-08-29")
	if !strings.Contains(day.Body.String(), "Your journal has no questions yet") || !strings.Contains(day.Body.String(), `href="/questions"`) {
		t.Fatal("daily empty state does not link to question management")
	}
	for _, tc := range []struct {
		name string
		form url.Values
		want string
	}{
		{"missing csrf", url.Values{"label": {"Mood"}, "type": {"short_text"}}, "Invalid form token"},
		{"blank label", url.Values{"csrf_token": {token}, "label": {"  "}, "type": {"short_text"}}, "cannot be empty"},
		{"bad type", url.Values{"csrf_token": {token}, "label": {"Mood"}, "type": {"script"}}, "valid question type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := a.request(http.MethodPost, "/questions", tc.form)
			if w.Code != http.StatusForbidden && w.Code != http.StatusBadRequest {
				t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.want) {
				t.Fatalf("missing %q in %s", tc.want, w.Body.String())
			}
		})
	}
	w = a.request(http.MethodPost, "/questions", url.Values{"csrf_token": {token + "x"}, "label": {"Mood"}, "type": {"short_text"}})
	if w.Code != http.StatusForbidden {
		t.Fatalf("invalid csrf=%d", w.Code)
	}
}

func TestQuestionManagementLifecycleOrderingAndEscaping(t *testing.T) {
	a := newTestApp(t)
	p := a.create("Writer", "", "UTC")
	js, _ := a.profiles.ListJournals(t.Context(), p.ID)
	a.loginProfile(p.ID)
	token, _ := a.getToken("/questions")
	post := func(path string, values url.Values) *httptest.ResponseRecorder {
		values.Set("csrf_token", token)
		w := a.request(http.MethodPost, path, values)
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/questions" {
			t.Fatalf("post %s: code=%d location=%s body=%s", path, w.Code, w.Header().Get("Location"), w.Body.String())
		}
		return w
	}
	post("/questions", url.Values{"label": {"<script>alert(1)</script>"}, "type": {"short_text"}})
	post("/questions", url.Values{"label": {"Meal"}, "type": {"select"}})
	qs, _ := a.questions.ListQuestions(t.Context(), js[0].ID, false)
	if len(qs) != 2 {
		t.Fatalf("created questions=%v", qs)
	}
	page := a.request(http.MethodGet, "/questions", nil).Body.String()
	if strings.Contains(page, "<script>alert(1)</script>") || !strings.Contains(page, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatal("question label was not escaped")
	}
	day := a.request(http.MethodGet, "/day/2026-08-29", nil).Body.String()
	if !strings.Contains(day, "Meal") || !strings.Contains(day, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatal("created questions missing from daily journal")
	}
	post("/questions/reorder", url.Values{"id": {"2", "1"}})
	qs, _ = a.questions.ListQuestions(t.Context(), js[0].ID, false)
	if qs[0].Label != "Meal" {
		t.Fatalf("question order=%v", qs)
	}
	day = a.request(http.MethodGet, "/day/2026-08-29", nil).Body.String()
	if strings.Index(day, "Meal") > strings.Index(day, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatal("daily journal did not follow question order")
	}
	before := []int64{qs[0].ID, qs[1].ID}
	w := a.request(http.MethodPost, "/questions/reorder", url.Values{"csrf_token": {token}, "id": {"1"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid reorder=%d", w.Code)
	}
	qs, _ = a.questions.ListQuestions(t.Context(), js[0].ID, false)
	if qs[0].ID != before[0] || qs[1].ID != before[1] {
		t.Fatal("invalid reorder partially changed order")
	}
	post("/questions/1/rename", url.Values{"label": {"Current mood"}})
	post("/questions/1/deactivate", url.Values{})
	page = a.request(http.MethodGet, "/questions", nil).Body.String()
	if !strings.Contains(page, "Inactive questions") || !strings.Contains(page, "Current mood") {
		t.Fatal("inactive question section missing")
	}
	day = a.request(http.MethodGet, "/day/2026-08-30", nil).Body.String()
	if strings.Contains(day, "Current mood") {
		t.Fatal("unused inactive question appears on future day")
	}
	post("/questions/1/reactivate", url.Values{})
	qs, _ = a.questions.ListQuestions(t.Context(), js[0].ID, false)
	if qs[len(qs)-1].ID != 1 {
		t.Fatalf("reactivated question not appended: %v", qs)
	}
}

func TestQuestionOptionManagementAndIsolation(t *testing.T) {
	a := newTestApp(t)
	userA := a.create("A", "", "UTC")
	userB := a.create("B", "", "UTC")
	ja, _ := a.profiles.ListJournals(t.Context(), userA.ID)
	jb, _ := a.profiles.ListJournals(t.Context(), userB.ID)
	selectA, _ := a.questions.CreateQuestion(t.Context(), ja[0].ID, questions.CreateQuestionInput{Label: "Meal", Type: questions.QuestionTypeSelect})
	textA, _ := a.questions.CreateQuestion(t.Context(), ja[0].ID, questions.CreateQuestionInput{Label: "Note", Type: questions.QuestionTypeShortText})
	questionB, _ := a.questions.CreateQuestion(t.Context(), jb[0].ID, questions.CreateQuestionInput{Label: "Private", Type: questions.QuestionTypeSelect})
	optionB, _ := a.questions.CreateOption(t.Context(), jb[0].ID, questionB.ID, questions.CreateOptionInput{Label: "Secret"})
	a.loginProfile(userA.ID)
	token, _ := a.getToken("/questions")
	post := func(path string, values url.Values) *httptest.ResponseRecorder {
		values.Set("csrf_token", token)
		return a.request(http.MethodPost, path, values)
	}
	for _, label := range []string{"<b>Breakfast</b>", "Dinner"} {
		w := post("/questions/"+strconv.FormatInt(selectA.ID, 10)+"/options", url.Values{"label": {label}})
		if w.Code != http.StatusSeeOther {
			t.Fatalf("add option=%d %s", w.Code, w.Body.String())
		}
	}
	page := a.request(http.MethodGet, "/questions", nil).Body.String()
	if strings.Contains(page, "<b>Breakfast</b>") || !strings.Contains(page, "&lt;b&gt;Breakfast&lt;/b&gt;") {
		t.Fatal("option label was not escaped")
	}
	if w := post("/questions/"+strconv.FormatInt(textA.ID, 10)+"/options", url.Values{"label": {"invalid"}}); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "only available") {
		t.Fatalf("non-select option: %d %s", w.Code, w.Body.String())
	}
	opts, _ := a.questions.ListOptions(t.Context(), ja[0].ID, selectA.ID, false)
	selectPath := "/questions/" + strconv.FormatInt(selectA.ID, 10) + "/options/"
	if w := post(selectPath+"reorder", url.Values{"id": {strconv.FormatInt(opts[1].ID, 10), strconv.FormatInt(opts[0].ID, 10)}}); w.Code != http.StatusSeeOther {
		t.Fatalf("option reorder=%d", w.Code)
	}
	managedID := opts[0].ID
	if w := post(selectPath+strconv.FormatInt(managedID, 10)+"/rename", url.Values{"label": {"Brunch"}}); w.Code != http.StatusSeeOther {
		t.Fatalf("rename option=%d", w.Code)
	}
	if w := post(selectPath+strconv.FormatInt(managedID, 10)+"/deactivate", url.Values{}); w.Code != http.StatusSeeOther {
		t.Fatalf("deactivate option=%d", w.Code)
	}
	if w := post(selectPath+strconv.FormatInt(managedID, 10)+"/reactivate", url.Values{}); w.Code != http.StatusSeeOther {
		t.Fatalf("reactivate option=%d", w.Code)
	}
	opts, _ = a.questions.ListOptions(t.Context(), ja[0].ID, selectA.ID, false)
	if len(opts) != 2 || opts[len(opts)-1].Label != "Brunch" {
		t.Fatalf("managed options=%v", opts)
	}
	day := a.request(http.MethodGet, "/day/2026-08-29", nil).Body.String()
	if strings.Index(day, "Dinner") > strings.Index(day, "Brunch") {
		t.Fatal("daily journal did not follow active option order")
	}
	for _, attempt := range []struct {
		path   string
		values url.Values
	}{
		{"/questions/" + strconv.FormatInt(questionB.ID, 10) + "/rename", url.Values{"label": {"Stolen"}}},
		{"/questions/" + strconv.FormatInt(questionB.ID, 10) + "/deactivate", url.Values{}},
		{"/questions/reorder", url.Values{"id": {strconv.FormatInt(questionB.ID, 10), strconv.FormatInt(selectA.ID, 10), strconv.FormatInt(textA.ID, 10)}}},
		{selectPath + strconv.FormatInt(optionB.ID, 10) + "/rename", url.Values{"label": {"Stolen"}}},
		{"/questions/" + strconv.FormatInt(questionB.ID, 10) + "/options/" + strconv.FormatInt(optionB.ID, 10) + "/rename", url.Values{"label": {"Stolen"}}},
	} {
		w := post(attempt.path, attempt.values)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("isolation %s=%d", attempt.path, w.Code)
		}
	}
	private, _ := a.questions.ListQuestions(t.Context(), jb[0].ID, true)
	privateOpts, _ := a.questions.ListOptions(t.Context(), jb[0].ID, questionB.ID, true)
	if private[0].Label != "Private" || !private[0].IsActive || privateOpts[0].ID != optionB.ID || privateOpts[0].Label != "Secret" {
		t.Fatal("cross-user operation changed private configuration")
	}
}
