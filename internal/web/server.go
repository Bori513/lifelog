package web

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/Bori513/lifelog/internal/journal"
	"github.com/Bori513/lifelog/internal/profiles"
	"github.com/Bori513/lifelog/internal/questions"
	webassets "github.com/Bori513/lifelog/web"
)

const (
	sessionCookie = "lifelog_session"
	csrfCookie    = "lifelog_csrf"
	sessionTTL    = 30 * 24 * time.Hour
	maxFormBytes  = 2 << 20
)

type Server struct {
	profiles      *profiles.Store
	questions     *questions.Store
	journal       *journal.Store
	templates     *template.Template
	secureCookies bool
	now           func() time.Time
	logger        *log.Logger
	handler       http.Handler
}

type OptionView struct {
	ID               int64
	Label            string
	Active, Selected bool
}
type QuestionView struct {
	ID                 int64
	Label, Type, Value string
	Active             bool
	BoolValue          *bool
	Options            []OptionView
	Scale              []int
}
type PageData struct {
	Title, Error, CSRF, ProfileName                string
	Profiles                                       []profiles.Profile
	SelectedProfile                                profiles.Profile
	ShowCreate                                     bool
	Date, DateLabel, PreviousDate, NextDate, Today string
	Saved                                          bool
	GeneralNote, SpecialMoment, Location           string
	Questions                                      []QuestionView
}

func New(db *sql.DB, secureCookies bool, logger *log.Logger) (*Server, error) {
	if logger == nil {
		logger = log.Default()
	}
	funcs := template.FuncMap{"initial": func(value string) string {
		for _, r := range value {
			return string(r)
		}
		return "?"
	}}
	t, err := template.New("base.html").Funcs(funcs).ParseFS(webassets.Files, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse web templates: %w", err)
	}
	s := &Server{profiles: profiles.NewStore(db), questions: questions.NewStore(db), journal: journal.NewStore(db), templates: t, secureCookies: secureCookies, now: time.Now, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.root)
	mux.HandleFunc("POST /profiles", s.createProfile)
	mux.HandleFunc("GET /profiles", s.profileList)
	mux.HandleFunc("POST /profiles/select", s.selectProfile)
	mux.HandleFunc("GET /login", s.loginForm)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /today", s.today)
	mux.HandleFunc("GET /day/{date}", s.getDay)
	mux.HandleFunc("POST /day/{date}", s.saveDay)
	mux.Handle("GET /static/", http.FileServerFS(webassets.Files))
	s.handler = s.securityHeaders(mux)
	return s, nil
}

func (s *Server) Handler() http.Handler         { return s.handler }
func (s *Server) SetClock(now func() time.Time) { s.now = now }

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) root(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authProfile(r); ok {
		http.Redirect(w, r, "/today", http.StatusSeeOther)
		return
	}
	ps, err := s.profiles.ListProfiles(r.Context())
	if err != nil {
		s.internal(w, "list profiles", err)
		return
	}
	if len(ps) == 0 {
		s.render(w, "profiles.html", PageData{Title: "Create profile", ShowCreate: true, CSRF: s.csrfToken(w, r)})
		return
	}
	if p, ok, err := s.profiles.AutoSelectableProfile(r.Context()); err != nil {
		s.internal(w, "auto-select profile", err)
		return
	} else if ok {
		if !s.startSession(w, r, p.ID) {
			return
		}
		http.Redirect(w, r, "/today", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/profiles", http.StatusSeeOther)
}

func (s *Server) profileList(w http.ResponseWriter, r *http.Request) {
	ps, err := s.profiles.ListProfiles(r.Context())
	if err != nil {
		s.internal(w, "list profiles", err)
		return
	}
	s.render(w, "profiles.html", PageData{Title: "Profiles", Profiles: ps, ShowCreate: len(ps) == 0 || r.URL.Query().Get("create") == "1", CSRF: s.csrfToken(w, r)})
}

func (s *Server) createProfile(w http.ResponseWriter, r *http.Request) {
	if !s.parseForm(w, r) || !s.validCSRF(r) {
		http.Error(w, "Invalid form token.", http.StatusForbidden)
		return
	}
	p, err := s.profiles.CreateProfile(r.Context(), profiles.CreateProfileInput{Name: r.FormValue("name"), Timezone: r.FormValue("timezone"), PIN: r.FormValue("pin")})
	if err != nil {
		s.render(w, "profiles.html", PageData{Title: "Create profile", ShowCreate: true, CSRF: s.csrfToken(w, r), Error: profileError(err)})
		return
	}
	if !s.startSession(w, r, p.ID) {
		return
	}
	http.Redirect(w, r, "/today", http.StatusSeeOther)
}

func (s *Server) selectProfile(w http.ResponseWriter, r *http.Request) {
	if !s.parseForm(w, r) || !s.validCSRF(r) {
		http.Error(w, "Invalid form token.", http.StatusForbidden)
		return
	}
	id, err := strconv.ParseInt(r.FormValue("profile_id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid profile.", http.StatusBadRequest)
		return
	}
	p, err := s.profiles.GetProfile(r.Context(), id)
	if err != nil {
		http.Error(w, "Profile not found.", http.StatusNotFound)
		return
	}
	if p.HasPIN {
		http.Redirect(w, r, "/login?profile="+strconv.FormatInt(id, 10), http.StatusSeeOther)
		return
	}
	if !s.startSession(w, r, id) {
		return
	}
	http.Redirect(w, r, "/today", http.StatusSeeOther)
}

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requestedProfile(w, r)
	if !ok {
		return
	}
	s.render(w, "login.html", PageData{Title: "Sign in", SelectedProfile: p, CSRF: s.csrfToken(w, r)})
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.parseForm(w, r) || !s.validCSRF(r) {
		http.Error(w, "Invalid form token.", http.StatusForbidden)
		return
	}
	id, err := strconv.ParseInt(r.FormValue("profile_id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid profile.", http.StatusBadRequest)
		return
	}
	p, err := s.profiles.GetProfile(r.Context(), id)
	if err != nil {
		http.Error(w, "Profile not found.", http.StatusNotFound)
		return
	}
	if err := s.profiles.VerifyPIN(r.Context(), id, r.FormValue("pin")); err != nil {
		s.render(w, "login.html", PageData{Title: "Sign in", SelectedProfile: p, CSRF: s.csrfToken(w, r), Error: "PIN or password was not accepted."})
		return
	}
	if !s.startSession(w, r, id) {
		return
	}
	http.Redirect(w, r, "/today", http.StatusSeeOther)
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !s.parseForm(w, r) || !s.validCSRF(r) {
		http.Error(w, "Invalid form token.", http.StatusForbidden)
		return
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := s.profiles.DeleteSession(r.Context(), c.Value); err != nil {
			s.logger.Printf("delete session: %v", err)
		}
	}
	s.setCookie(w, sessionCookie, "", time.Unix(1, 0), -1, true)
	http.Redirect(w, r, "/profiles", http.StatusSeeOther)
}

func (s *Server) today(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireProfile(w, r)
	if !ok {
		return
	}
	loc, err := time.LoadLocation(p.Timezone)
	if err != nil {
		s.internal(w, "load profile timezone", err)
		return
	}
	http.Redirect(w, r, "/day/"+s.now().In(loc).Format("2006-01-02"), http.StatusSeeOther)
}
func (s *Server) getDay(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireProfile(w, r)
	if !ok {
		return
	}
	s.renderDay(w, r, p, r.PathValue("date"), "")
}
func (s *Server) saveDay(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireProfile(w, r)
	if !ok {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	if !s.validCSRF(r) {
		http.Error(w, "Invalid form token.", http.StatusForbidden)
		return
	}
	date := r.PathValue("date")
	j, ok := s.defaultJournal(w, r, p)
	if !ok {
		return
	}
	input, err := s.dayInput(r, j.ID)
	if err != nil {
		s.renderDay(w, r, p, date, err.Error())
		return
	}
	if _, err := s.journal.SaveDay(r.Context(), j.ID, date, input); err != nil {
		s.logger.Printf("save day: %v", err)
		s.renderDay(w, r, p, date, "Could not save this day. Check the highlighted values and try again.")
		return
	}
	http.Redirect(w, r, "/day/"+date+"?saved=1", http.StatusSeeOther)
}

func (s *Server) renderDay(w http.ResponseWriter, r *http.Request, p profiles.Profile, date, message string) {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil || parsed.Format("2006-01-02") != date {
		http.Error(w, "Invalid date.", http.StatusBadRequest)
		return
	}
	j, ok := s.defaultJournal(w, r, p)
	if !ok {
		return
	}
	day, err := s.journal.GetDay(r.Context(), j.ID, date)
	if err != nil {
		s.internal(w, "load day", err)
		return
	}
	all, err := s.questions.ListQuestions(r.Context(), j.ID, true)
	if err != nil {
		s.internal(w, "load questions", err)
		return
	}
	answers := map[int64]journal.Answer{}
	for _, a := range day.Answers {
		answers[a.QuestionID] = a
	}
	views := make([]QuestionView, 0, len(all))
	for _, q := range all {
		a, has := answers[q.ID]
		if !q.IsActive && !has {
			continue
		}
		label := q.Label
		if !q.IsActive && has {
			label = a.QuestionLabelSnapshot
		}
		v := QuestionView{ID: q.ID, Label: label, Type: string(q.Type), Active: q.IsActive}
		if has {
			if a.TextValue != nil {
				v.Value = *a.TextValue
			}
			if a.NumberValue != nil {
				v.Value = strconv.FormatFloat(*a.NumberValue, 'f', -1, 64)
			}
			if a.TimeValue != nil {
				v.Value = *a.TimeValue
			}
			if a.BoolValue != nil {
				v.Value = strconv.FormatBool(*a.BoolValue)
				v.BoolValue = a.BoolValue
			}
		}
		if q.Type == questions.QuestionTypeScale5 {
			v.Scale = []int{1, 2, 3, 4, 5}
		}
		if q.Type == questions.QuestionTypeScale10 {
			v.Scale = []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
		}
		if q.Type == questions.QuestionTypeSelect || q.Type == questions.QuestionTypeMultiSelect {
			opts, e := s.questions.ListOptions(r.Context(), j.ID, q.ID, true)
			if e != nil {
				s.internal(w, "load options", e)
				return
			}
			selected := map[int64]string{}
			for _, o := range a.SelectedOptions {
				selected[o.OptionID] = o.OptionLabelSnapshot
			}
			for _, o := range opts {
				snap, sel := selected[o.ID]
				if !o.IsActive && !sel {
					continue
				}
				label := o.Label
				if !o.IsActive && sel {
					label = snap
				}
				v.Options = append(v.Options, OptionView{ID: o.ID, Label: label, Active: o.IsActive, Selected: sel})
			}
		}
		views = append(views, v)
	}
	loc, _ := time.LoadLocation(p.Timezone)
	today := s.now().In(loc).Format("2006-01-02")
	d := PageData{Title: "Daily journal", ProfileName: p.Name, CSRF: s.csrfToken(w, r), Date: date, DateLabel: parsed.Format("2 January 2006"), PreviousDate: parsed.AddDate(0, 0, -1).Format("2006-01-02"), NextDate: parsed.AddDate(0, 0, 1).Format("2006-01-02"), Today: today, Saved: r.URL.Query().Get("saved") == "1", Error: message, GeneralNote: day.GeneralNote, SpecialMoment: day.SpecialMoment, Location: day.Location, Questions: views}
	s.render(w, "day.html", d)
}

func (s *Server) dayInput(r *http.Request, journalID int64) (journal.SaveDayInput, error) {
	qs, err := s.questions.ListQuestions(r.Context(), journalID, true)
	if err != nil {
		return journal.SaveDayInput{}, err
	}
	input := journal.SaveDayInput{GeneralNote: r.FormValue("general_note"), SpecialMoment: r.FormValue("special_moment"), Location: r.FormValue("location")}
	for _, q := range qs {
		prefix := "q_" + strconv.FormatInt(q.ID, 10)
		if _, present := r.PostForm[prefix+"_present"]; !present {
			continue
		}
		a := journal.AnswerInput{QuestionID: q.ID}
		raw := r.FormValue(prefix)
		switch q.Type {
		case questions.QuestionTypeShortText, questions.QuestionTypeLongText:
			a.TextValue = &raw
		case questions.QuestionTypeBoolean:
			if raw == "" {
				a.Clear = true
			} else {
				b, e := strconv.ParseBool(raw)
				if e != nil {
					return input, fmt.Errorf("Invalid value for %s.", q.Label)
				}
				a.BoolValue = &b
			}
		case questions.QuestionTypeNumber, questions.QuestionTypeScale5, questions.QuestionTypeScale10:
			if raw == "" {
				a.Clear = true
			} else {
				n, e := strconv.ParseFloat(raw, 64)
				if e != nil {
					return input, fmt.Errorf("Invalid value for %s.", q.Label)
				}
				a.NumberValue = &n
			}
		case questions.QuestionTypeTime:
			if raw == "" {
				a.Clear = true
			} else {
				a.TimeValue = &raw
			}
		case questions.QuestionTypeSelect, questions.QuestionTypeMultiSelect:
			for _, v := range r.PostForm[prefix] {
				id, e := strconv.ParseInt(v, 10, 64)
				if e != nil {
					return input, fmt.Errorf("Please choose a valid option for %s.", q.Label)
				}
				a.OptionIDs = append(a.OptionIDs, id)
			}
		}
		input.Answers = append(input.Answers, a)
	}
	return input, nil
}

func (s *Server) defaultJournal(w http.ResponseWriter, r *http.Request, p profiles.Profile) (profiles.Journal, bool) {
	js, err := s.profiles.ListJournals(r.Context(), p.ID)
	if err != nil || len(js) == 0 {
		s.internal(w, "load profile journal", err)
		return profiles.Journal{}, false
	}
	return js[0], true
}
func (s *Server) requestedProfile(w http.ResponseWriter, r *http.Request) (profiles.Profile, bool) {
	id, e := strconv.ParseInt(r.URL.Query().Get("profile"), 10, 64)
	if e != nil {
		http.Error(w, "Invalid profile.", http.StatusBadRequest)
		return profiles.Profile{}, false
	}
	p, e := s.profiles.GetProfile(r.Context(), id)
	if e != nil {
		http.Error(w, "Profile not found.", http.StatusNotFound)
		return profiles.Profile{}, false
	}
	return p, true
}
func (s *Server) authProfile(r *http.Request) (profiles.Profile, bool) {
	c, e := r.Cookie(sessionCookie)
	if e != nil {
		return profiles.Profile{}, false
	}
	p, e := s.profiles.GetProfileBySession(r.Context(), c.Value)
	return p, e == nil
}
func (s *Server) requireProfile(w http.ResponseWriter, r *http.Request) (profiles.Profile, bool) {
	p, ok := s.authProfile(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
	return p, ok
}
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, id int64) bool {
	session, e := s.profiles.CreateSession(r.Context(), id, sessionTTL)
	if e != nil {
		s.internal(w, "create session", e)
		return false
	}
	s.setCookie(w, sessionCookie, session.Token, session.ExpiresAt, 0, true)
	return true
}
func (s *Server) setCookie(w http.ResponseWriter, name, value string, expires time.Time, maxAge int, httpOnly bool) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Expires: expires, MaxAge: maxAge, HttpOnly: httpOnly, Secure: s.secureCookies, SameSite: http.SameSiteLaxMode})
}
func (s *Server) csrfToken(w http.ResponseWriter, r *http.Request) string {
	if c, e := r.Cookie(csrfCookie); e == nil && validToken(c.Value) {
		return c.Value
	}
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		s.logger.Printf("generate CSRF token: %v", e)
		return ""
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	s.setCookie(w, csrfCookie, token, time.Now().Add(sessionTTL), 0, true)
	return token
}
func validToken(v string) bool {
	b, e := base64.RawURLEncoding.DecodeString(v)
	return e == nil && len(b) == 32
}
func (s *Server) validCSRF(r *http.Request) bool {
	c, e := r.Cookie(csrfCookie)
	if e != nil {
		return false
	}
	form := r.PostFormValue("csrf_token")
	return validToken(c.Value) && subtle.ConstantTimeCompare([]byte(c.Value), []byte(form)) == 1
}
func (s *Server) parseForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if e := r.ParseForm(); e != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(e, &tooLarge) {
			http.Error(w, "Form is too large.", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "Invalid form.", http.StatusBadRequest)
		}
		return false
	}
	return true
}
func (s *Server) render(w http.ResponseWriter, name string, data PageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if e := s.templates.ExecuteTemplate(w, name, data); e != nil {
		s.logger.Printf("render %s: %v", name, e)
	}
}
func (s *Server) internal(w http.ResponseWriter, operation string, err error) {
	if err == nil {
		err = errors.New("missing record")
	}
	s.logger.Printf("%s: %v", operation, err)
	http.Error(w, "Something went wrong.", http.StatusInternalServerError)
}
func profileError(err error) string {
	switch {
	case errors.Is(err, profiles.ErrInvalidName):
		return "Please enter a profile name."
	case errors.Is(err, profiles.ErrInvalidTimezone):
		return "Please enter a valid IANA timezone."
	case errors.Is(err, profiles.ErrPINTooShort):
		return "PIN or password must be at least 4 characters."
	default:
		return "Could not create the profile."
	}
}
