package web

import (
	"io"
	"log"
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
	"github.com/Bori513/lifelog/internal/profiles"
	"github.com/Bori513/lifelog/internal/questions"
)

type testApp struct {
	t         *testing.T
	s         *Server
	profiles  *profiles.Store
	questions *questions.Store
	journal   *journal.Store
	cookies   map[string]*http.Cookie
}

func newTestApp(t *testing.T) *testApp {
	t.Helper()
	db, err := database.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := New(db, false, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	return &testApp{t: t, s: s, profiles: profiles.NewStore(db), questions: questions.NewStore(db), journal: journal.NewStore(db), cookies: map[string]*http.Cookie{}}
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
