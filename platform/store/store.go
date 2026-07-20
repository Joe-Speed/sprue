// Package store owns the SQLite database: schema, and every query the
// platform runs. Nothing outside this package writes SQL.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

const (
	MaxBuildsPerUser   = 200
	MaxPhotosPerBuild  = 12
	MaxEntriesPerComp  = 64
	MaxCompetitions    = 256
	MaxSlugAttempts    = 100
	sessionLifetime    = 90 * 24 * time.Hour
	magicTokenLifetime = 15 * time.Minute
	maxTextField       = 2000
	maxShortField      = 200
)

var ErrNotFound = errors.New("not found")
var ErrLimit = errors.New("limit reached")

type Store struct {
	db *sql.DB
}

const schema = `
create table if not exists users (
	id integer primary key,
	email text not null unique,
	display_name text not null,
	slug text not null unique,
	is_admin integer not null default 0,
	created_at text not null
);
create table if not exists magic_tokens (
	token_hash text primary key,
	email text not null,
	expires_at text not null,
	used integer not null default 0
);
create table if not exists sessions (
	token_hash text primary key,
	user_id integer not null references users(id),
	expires_at text not null
);
create table if not exists builds (
	id integer primary key,
	user_id integer not null references users(id),
	title text not null,
	kit text not null,
	brand text not null default '',
	scale text not null default '',
	description text not null default '',
	built_on text not null default '',
	featured integer not null default 0,
	created_at text not null
);
create table if not exists photos (
	id integer primary key,
	build_id integer not null references builds(id),
	position integer not null,
	file_name text not null
);
create table if not exists competitions (
	id integer primary key,
	slug text not null unique,
	title text not null,
	deadline text not null,
	status text not null default 'open',
	created_at text not null
);
create table if not exists entries (
	id integer primary key,
	competition_id integer not null references competitions(id),
	build_id integer not null references builds(id),
	created_at text not null,
	unique(competition_id, build_id)
);
create table if not exists votes (
	competition_id integer not null references competitions(id),
	user_id integer not null references users(id),
	entry_id integer not null references entries(id),
	created_at text not null,
	primary key (competition_id, user_id)
);
create table if not exists trophies (
	id integer primary key,
	competition_id integer not null references competitions(id),
	place integer not null,
	user_id integer not null references users(id),
	build_id integer not null references builds(id),
	downloads_left integer not null default 1,
	unique(competition_id, place)
);
`

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("store: empty database path")
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("store: schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func clip(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

type User struct {
	ID          int64
	Email       string
	DisplayName string
	Slug        string
	IsAdmin     bool
	CreatedAt   string
}

func (s *Store) CreateMagicToken(tokenHash, email string) error {
	if tokenHash == "" || email == "" {
		return errors.New("store: empty magic token or email")
	}
	expires := time.Now().UTC().Add(magicTokenLifetime).Format(time.RFC3339)
	_, err := s.db.Exec(`insert into magic_tokens (token_hash, email, expires_at) values (?, ?, ?)`,
		tokenHash, strings.ToLower(clip(email, maxShortField)), expires)
	return err
}

// ConsumeMagicToken burns the token and returns the email it was issued for.
func (s *Store) ConsumeMagicToken(tokenHash string) (string, error) {
	if tokenHash == "" {
		return "", ErrNotFound
	}
	var email, expires string
	var used int
	err := s.db.QueryRow(`select email, expires_at, used from magic_tokens where token_hash = ?`, tokenHash).
		Scan(&email, &expires, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	when, err := time.Parse(time.RFC3339, expires)
	if err != nil || used != 0 || time.Now().UTC().After(when) {
		return "", ErrNotFound
	}
	if _, err := s.db.Exec(`update magic_tokens set used = 1 where token_hash = ?`, tokenHash); err != nil {
		return "", err
	}
	return email, nil
}

// FindOrCreateUser returns the user for an email, creating one on first login.
// makeAdmin marks the account as admin (used for the configured admin email).
func (s *Store) FindOrCreateUser(email string, makeAdmin bool) (User, error) {
	email = strings.ToLower(clip(email, maxShortField))
	if email == "" || !strings.Contains(email, "@") {
		return User{}, errors.New("store: bad email")
	}
	user, err := s.userBy("email = ?", email)
	if err == nil {
		if makeAdmin && !user.IsAdmin {
			if _, err := s.db.Exec(`update users set is_admin = 1 where id = ?`, user.ID); err != nil {
				return User{}, err
			}
			user.IsAdmin = true
		}
		return user, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return User{}, err
	}
	name := email[:strings.Index(email, "@")]
	if name == "" {
		name = "builder"
	}
	slug, err := s.uniqueSlug("users", Slugify(name))
	if err != nil {
		return User{}, err
	}
	admin := 0
	if makeAdmin {
		admin = 1
	}
	result, err := s.db.Exec(`insert into users (email, display_name, slug, is_admin, created_at) values (?, ?, ?, ?, ?)`,
		email, clip(name, maxShortField), slug, admin, now())
	if err != nil {
		return User{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return User{}, err
	}
	return s.userBy("id = ?", id)
}

func (s *Store) userBy(where string, arg any) (User, error) {
	var u User
	var admin int
	err := s.db.QueryRow(`select id, email, display_name, slug, is_admin, created_at from users where `+where, arg).
		Scan(&u.ID, &u.Email, &u.DisplayName, &u.Slug, &admin, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	u.IsAdmin = admin != 0
	return u, nil
}

func (s *Store) UserBySlug(slug string) (User, error) { return s.userBy("slug = ?", slug) }
func (s *Store) UserByID(id int64) (User, error)      { return s.userBy("id = ?", id) }

func (s *Store) RenameUser(id int64, displayName string) error {
	displayName = clip(displayName, maxShortField)
	if displayName == "" {
		return errors.New("store: empty display name")
	}
	_, err := s.db.Exec(`update users set display_name = ? where id = ?`, displayName, id)
	return err
}

func (s *Store) CreateSession(tokenHash string, userID int64) error {
	if tokenHash == "" || userID <= 0 {
		return errors.New("store: bad session")
	}
	expires := time.Now().UTC().Add(sessionLifetime).Format(time.RFC3339)
	_, err := s.db.Exec(`insert into sessions (token_hash, user_id, expires_at) values (?, ?, ?)`,
		tokenHash, userID, expires)
	return err
}

func (s *Store) SessionUser(tokenHash string) (User, error) {
	if tokenHash == "" {
		return User{}, ErrNotFound
	}
	var userID int64
	var expires string
	err := s.db.QueryRow(`select user_id, expires_at from sessions where token_hash = ?`, tokenHash).
		Scan(&userID, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	when, err := time.Parse(time.RFC3339, expires)
	if err != nil || time.Now().UTC().After(when) {
		return User{}, ErrNotFound
	}
	return s.userBy("id = ?", userID)
}

func (s *Store) DeleteSession(tokenHash string) error {
	_, err := s.db.Exec(`delete from sessions where token_hash = ?`, tokenHash)
	return err
}

type Build struct {
	ID          int64
	UserID      int64
	Title       string
	Kit         string
	Brand       string
	Scale       string
	Description string
	BuiltOn     string
	Featured    bool
	CreatedAt   string
	// Filled by list queries for display.
	OwnerName   string
	OwnerSlug   string
	CoverPhoto  string
	TrophyPlace int // 0 = none, else 1..3 best placing this build has won
}

func (s *Store) CreateBuild(b Build) (int64, error) {
	if b.UserID <= 0 || clip(b.Title, maxShortField) == "" || clip(b.Kit, maxShortField) == "" {
		return 0, errors.New("store: build needs an owner, a title, and a kit")
	}
	var count int
	if err := s.db.QueryRow(`select count(*) from builds where user_id = ?`, b.UserID).Scan(&count); err != nil {
		return 0, err
	}
	if count >= MaxBuildsPerUser {
		return 0, ErrLimit
	}
	result, err := s.db.Exec(
		`insert into builds (user_id, title, kit, brand, scale, description, built_on, featured, created_at)
		 values (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.UserID, clip(b.Title, maxShortField), clip(b.Kit, maxShortField), clip(b.Brand, maxShortField),
		clip(b.Scale, maxShortField), clip(b.Description, maxTextField), clip(b.BuiltOn, maxShortField),
		boolInt(b.Featured), now())
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *Store) UpdateBuild(b Build) error {
	if b.ID <= 0 || b.UserID <= 0 {
		return errors.New("store: bad build update")
	}
	_, err := s.db.Exec(
		`update builds set title = ?, kit = ?, brand = ?, scale = ?, description = ?, built_on = ?, featured = ?
		 where id = ? and user_id = ?`,
		clip(b.Title, maxShortField), clip(b.Kit, maxShortField), clip(b.Brand, maxShortField),
		clip(b.Scale, maxShortField), clip(b.Description, maxTextField), clip(b.BuiltOn, maxShortField),
		boolInt(b.Featured), b.ID, b.UserID)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

const buildColumns = `
	b.id, b.user_id, b.title, b.kit, b.brand, b.scale, b.description, b.built_on, b.featured, b.created_at,
	u.display_name, u.slug,
	coalesce((select p.file_name from photos p where p.build_id = b.id order by p.position limit 1), ''),
	coalesce((select min(t.place) from trophies t where t.build_id = b.id), 0)
`

func (s *Store) scanBuilds(rows *sql.Rows) ([]Build, error) {
	defer rows.Close()
	var builds []Build
	for rows.Next() {
		var b Build
		var featured int
		err := rows.Scan(&b.ID, &b.UserID, &b.Title, &b.Kit, &b.Brand, &b.Scale, &b.Description,
			&b.BuiltOn, &featured, &b.CreatedAt, &b.OwnerName, &b.OwnerSlug, &b.CoverPhoto, &b.TrophyPlace)
		if err != nil {
			return nil, err
		}
		b.Featured = featured != 0
		builds = append(builds, b)
	}
	return builds, rows.Err()
}

func (s *Store) BuildByID(id int64) (Build, error) {
	rows, err := s.db.Query(`select `+buildColumns+` from builds b join users u on u.id = b.user_id where b.id = ?`, id)
	if err != nil {
		return Build{}, err
	}
	builds, err := s.scanBuilds(rows)
	if err != nil {
		return Build{}, err
	}
	if len(builds) == 0 {
		return Build{}, ErrNotFound
	}
	return builds[0], nil
}

// BuildsForUser returns featured builds first, then the rest, newest built first.
func (s *Store) BuildsForUser(userID int64) ([]Build, error) {
	rows, err := s.db.Query(`select `+buildColumns+` from builds b join users u on u.id = b.user_id
		where b.user_id = ?
		order by b.featured desc, case when b.built_on = '' then b.created_at else b.built_on end desc, b.id desc
		limit ?`, userID, MaxBuildsPerUser)
	if err != nil {
		return nil, err
	}
	return s.scanBuilds(rows)
}

func (s *Store) RecentBuilds(limit int) ([]Build, error) {
	if limit <= 0 || limit > 100 {
		limit = 24
	}
	rows, err := s.db.Query(`select `+buildColumns+` from builds b join users u on u.id = b.user_id
		order by b.id desc limit ?`, limit)
	if err != nil {
		return nil, err
	}
	return s.scanBuilds(rows)
}

func (s *Store) AddPhoto(buildID int64, fileName string) error {
	if buildID <= 0 || fileName == "" {
		return errors.New("store: bad photo")
	}
	var count int
	if err := s.db.QueryRow(`select count(*) from photos where build_id = ?`, buildID).Scan(&count); err != nil {
		return err
	}
	if count >= MaxPhotosPerBuild {
		return ErrLimit
	}
	_, err := s.db.Exec(`insert into photos (build_id, position, file_name) values (?, ?, ?)`,
		buildID, count, fileName)
	return err
}

func (s *Store) Photos(buildID int64) ([]string, error) {
	rows, err := s.db.Query(`select file_name from photos where build_id = ? order by position limit ?`,
		buildID, MaxPhotosPerBuild)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

type Competition struct {
	ID        int64
	Slug      string
	Title     string
	Deadline  string
	Status    string
	CreatedAt string
}

func (s *Store) CreateCompetition(title, deadline string) (Competition, error) {
	title = clip(title, maxShortField)
	deadline = clip(deadline, maxShortField)
	if title == "" || deadline == "" {
		return Competition{}, errors.New("store: competition needs a title and a deadline")
	}
	var count int
	if err := s.db.QueryRow(`select count(*) from competitions`).Scan(&count); err != nil {
		return Competition{}, err
	}
	if count >= MaxCompetitions {
		return Competition{}, ErrLimit
	}
	slug, err := s.uniqueSlug("competitions", Slugify(title))
	if err != nil {
		return Competition{}, err
	}
	_, err = s.db.Exec(`insert into competitions (slug, title, deadline, status, created_at) values (?, ?, ?, 'open', ?)`,
		slug, title, deadline, now())
	if err != nil {
		return Competition{}, err
	}
	return s.CompetitionBySlug(slug)
}

func (s *Store) CompetitionBySlug(slug string) (Competition, error) {
	var c Competition
	err := s.db.QueryRow(`select id, slug, title, deadline, status, created_at from competitions where slug = ?`, slug).
		Scan(&c.ID, &c.Slug, &c.Title, &c.Deadline, &c.Status, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Competition{}, ErrNotFound
	}
	return c, err
}

func (s *Store) Competitions() ([]Competition, error) {
	rows, err := s.db.Query(`select id, slug, title, deadline, status, created_at from competitions
		order by id desc limit ?`, MaxCompetitions)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Competition
	for rows.Next() {
		var c Competition
		if err := rows.Scan(&c.ID, &c.Slug, &c.Title, &c.Deadline, &c.Status, &c.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

func (s *Store) SetCompetitionStatus(id int64, status string) error {
	if status != "open" && status != "voting" && status != "decided" {
		return errors.New("store: bad status")
	}
	_, err := s.db.Exec(`update competitions set status = ? where id = ?`, status, id)
	return err
}

type Entry struct {
	ID    int64
	Build Build
	Votes int
}

func (s *Store) EnterCompetition(competitionID, buildID, userID int64) error {
	if competitionID <= 0 || buildID <= 0 || userID <= 0 {
		return errors.New("store: bad entry")
	}
	build, err := s.BuildByID(buildID)
	if err != nil {
		return err
	}
	if build.UserID != userID {
		return errors.New("store: not your build")
	}
	var count int
	if err := s.db.QueryRow(`select count(*) from entries where competition_id = ?`, competitionID).Scan(&count); err != nil {
		return err
	}
	if count >= MaxEntriesPerComp {
		return ErrLimit
	}
	var existing int
	err = s.db.QueryRow(`select count(*) from entries e join builds b on b.id = e.build_id
		where e.competition_id = ? and b.user_id = ?`, competitionID, userID).Scan(&existing)
	if err != nil {
		return err
	}
	if existing > 0 {
		return errors.New("store: you already entered this competition")
	}
	_, err = s.db.Exec(`insert into entries (competition_id, build_id, created_at) values (?, ?, ?)`,
		competitionID, buildID, now())
	return err
}

// EntriesWithVotes lists a competition's entries in entry order with counts.
func (s *Store) EntriesWithVotes(competitionID int64) ([]Entry, error) {
	rows, err := s.db.Query(`select e.id, `+buildColumns+`,
		(select count(*) from votes v where v.entry_id = e.id)
		from entries e join builds b on b.id = e.build_id join users u on u.id = b.user_id
		where e.competition_id = ? order by e.id limit ?`, competitionID, MaxEntriesPerComp)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Entry
	for rows.Next() {
		var entry Entry
		var featured int
		b := &entry.Build
		err := rows.Scan(&entry.ID, &b.ID, &b.UserID, &b.Title, &b.Kit, &b.Brand, &b.Scale, &b.Description,
			&b.BuiltOn, &featured, &b.CreatedAt, &b.OwnerName, &b.OwnerSlug, &b.CoverPhoto, &b.TrophyPlace, &entry.Votes)
		if err != nil {
			return nil, err
		}
		b.Featured = featured != 0
		list = append(list, entry)
	}
	return list, rows.Err()
}

func (s *Store) Vote(competitionID, userID, entryID int64) error {
	var entryOwner int64
	err := s.db.QueryRow(`select b.user_id from entries e join builds b on b.id = e.build_id
		where e.id = ? and e.competition_id = ?`, entryID, competitionID).Scan(&entryOwner)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if entryOwner == userID {
		return errors.New("store: no voting for your own entry")
	}
	voted, err := s.HasVoted(competitionID, userID)
	if err != nil {
		return err
	}
	if voted {
		return errors.New("store: you already voted in this competition")
	}
	_, err = s.db.Exec(`insert into votes (competition_id, user_id, entry_id, created_at) values (?, ?, ?, ?)`,
		competitionID, userID, entryID, now())
	return err
}

func (s *Store) HasVoted(competitionID, userID int64) (bool, error) {
	var count int
	err := s.db.QueryRow(`select count(*) from votes where competition_id = ? and user_id = ?`,
		competitionID, userID).Scan(&count)
	return count > 0, err
}

// Decide tallies votes deterministically (ties break to the earlier entry),
// writes trophies for up to three placings, and marks the competition decided.
// Entries with zero votes never place.
func (s *Store) Decide(competitionID int64) error {
	entries, err := s.EntriesWithVotes(competitionID)
	if err != nil {
		return err
	}
	var winners []Entry
	taken := map[int64]bool{}
	for place := 0; place < 3; place++ {
		best := -1
		for i, entry := range entries {
			if taken[entry.ID] || entry.Votes == 0 {
				continue
			}
			if best == -1 || entry.Votes > entries[best].Votes {
				best = i
			}
		}
		if best == -1 {
			break
		}
		taken[entries[best].ID] = true
		winners = append(winners, entries[best])
	}
	if len(winners) == 0 {
		return errors.New("store: no votes, nothing to decide")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, winner := range winners {
		_, err := tx.Exec(`insert into trophies (competition_id, place, user_id, build_id) values (?, ?, ?, ?)`,
			competitionID, i+1, winner.Build.UserID, winner.Build.ID)
		if err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`update competitions set status = 'decided' where id = ?`, competitionID); err != nil {
		return err
	}
	return tx.Commit()
}

type Trophy struct {
	ID               int64
	CompetitionID    int64
	CompetitionSlug  string
	CompetitionTitle string
	Place            int
	UserID           int64
	BuildID          int64
	BuildTitle       string
	DownloadsLeft    int
}

const trophyColumns = `t.id, t.competition_id, c.slug, c.title, t.place, t.user_id, t.build_id, b.title, t.downloads_left
	from trophies t join competitions c on c.id = t.competition_id join builds b on b.id = t.build_id`

func (s *Store) scanTrophies(rows *sql.Rows) ([]Trophy, error) {
	defer rows.Close()
	var list []Trophy
	for rows.Next() {
		var t Trophy
		err := rows.Scan(&t.ID, &t.CompetitionID, &t.CompetitionSlug, &t.CompetitionTitle,
			&t.Place, &t.UserID, &t.BuildID, &t.BuildTitle, &t.DownloadsLeft)
		if err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

func (s *Store) TrophiesForUser(userID int64) ([]Trophy, error) {
	rows, err := s.db.Query(`select `+trophyColumns+` where t.user_id = ? order by t.id desc limit 100`, userID)
	if err != nil {
		return nil, err
	}
	return s.scanTrophies(rows)
}

func (s *Store) TrophiesForCompetition(competitionID int64) ([]Trophy, error) {
	rows, err := s.db.Query(`select `+trophyColumns+` where t.competition_id = ? order by t.place limit 3`, competitionID)
	if err != nil {
		return nil, err
	}
	return s.scanTrophies(rows)
}

func (s *Store) TrophyByID(id int64) (Trophy, error) {
	rows, err := s.db.Query(`select `+trophyColumns+` where t.id = ?`, id)
	if err != nil {
		return Trophy{}, err
	}
	list, err := s.scanTrophies(rows)
	if err != nil {
		return Trophy{}, err
	}
	if len(list) == 0 {
		return Trophy{}, ErrNotFound
	}
	return list[0], nil
}

// UseTrophyDownload decrements the download allowance, refusing at zero.
func (s *Store) UseTrophyDownload(id, userID int64) error {
	result, err := s.db.Exec(`update trophies set downloads_left = downloads_left - 1
		where id = ? and user_id = ? and downloads_left > 0`, id, userID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RearmTrophy(id int64) error {
	_, err := s.db.Exec(`update trophies set downloads_left = 1 where id = ?`, id)
	return err
}

// Slugify turns free text into a lowercase hyphenated slug, ascii only.
func Slugify(text string) string {
	var out strings.Builder
	lastHyphen := true
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				out.WriteByte('-')
				lastHyphen = true
			}
		}
		if out.Len() >= 60 {
			break
		}
	}
	slug := strings.Trim(out.String(), "-")
	if slug == "" {
		return "item"
	}
	return slug
}

func (s *Store) uniqueSlug(table, base string) (string, error) {
	if table != "users" && table != "competitions" {
		return "", errors.New("store: bad slug table")
	}
	for attempt := 0; attempt < MaxSlugAttempts; attempt++ {
		candidate := base
		if attempt > 0 {
			candidate = fmt.Sprintf("%s-%d", base, attempt+1)
		}
		var count int
		if err := s.db.QueryRow(`select count(*) from `+table+` where slug = ?`, candidate).Scan(&count); err != nil {
			return "", err
		}
		if count == 0 {
			return candidate, nil
		}
	}
	return "", ErrLimit
}
