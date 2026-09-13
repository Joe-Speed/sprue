// Package store owns the SQLite database: schema, and every query the
// platform runs. Nothing outside this package writes SQL.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"regexp"
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
	MaxOpenPerCreator  = 3   // competitions one member may have running at once
	MaxEntryDays       = 180 // how far ahead entries may stay open
	MaxVotingDays      = 60  // how long voting may run after entries close
	FeaturedWindowDays = 30  // votes this recent count toward the featured spot
	MaxFeatured        = 3
	MaxSlugAttempts    = 100
	MaxLikesPerMember  = 5000                // builds one member may have liked at once
	MaxLikesShown      = 200                 // liked builds listed on the likes page
	sessionLifetime    = 90 * 24 * time.Hour // remembered sessions, renewed while the member keeps visiting
	shortSessionLife   = 24 * time.Hour      // sessions the member asked not to remember
	magicTokenLifetime = 15 * time.Minute
	maxTextField       = 2000
	maxShortField      = 200
)

var ErrNotFound = errors.New("not found")
var ErrLimit = errors.New("limit reached")
var ErrInUse = errors.New("in use")

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
	created_at text not null,
	goal_count integer not null default 0,
	goal_by text not null default '',
	goal_set_at text not null default '',
	nudge text not null default 'off',
	nudged_at text not null default '',
	flair text not null default '',
	slug_chosen integer not null default 0,
	avatar text not null default '',
	bio text not null default ''
);
create table if not exists stash (
	id integer primary key autoincrement,
	user_id integer not null references users(id),
	title text not null,
	brand text not null default '',
	scale text not null default '',
	note text not null default '',
	cost_pence integer not null default 0,
	status text not null default 'unbuilt',
	next integer not null default 0,
	build_id integer references builds(id),
	added_at text not null,
	finished_at text not null default ''
);
create table if not exists journal (
	id integer primary key,
	stash_id integer not null references stash(id),
	text text not null,
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
	id integer primary key autoincrement,
	user_id integer not null references users(id),
	title text not null,
	kit text not null,
	brand text not null default '',
	scale text not null default '',
	description text not null default '',
	built_on text not null default '',
	pinned integer not null default 0,
	private integer not null default 0,
	hidden integer not null default 0,
	created_at text not null
);
create table if not exists reports (
	id integer primary key,
	build_id integer not null references builds(id),
	user_id integer not null references users(id),
	reason text not null default '',
	created_at text not null,
	unique(build_id, user_id)
);
create table if not exists build_votes (
	build_id integer not null references builds(id),
	user_id integer not null references users(id),
	created_at text not null,
	primary key (build_id, user_id)
);
create table if not exists build_likes (
	build_id integer not null references builds(id),
	user_id integer not null references users(id),
	created_at text not null,
	primary key (build_id, user_id)
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
	description text not null default '',
	creator_id integer not null references users(id),
	entries_close text not null,
	voting_closes text not null,
	status text not null default 'open',
	official integer not null default 0,
	theme text not null default '',
	category text not null default '',
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
create table if not exists friendships (
	requester_id integer not null references users(id),
	addressee_id integer not null references users(id),
	status text not null default 'pending',
	created_at text not null,
	primary key (requester_id, addressee_id),
	check (requester_id != addressee_id)
);
create unique index if not exists friendship_pair on friendships (min(requester_id, addressee_id), max(requester_id, addressee_id));
create table if not exists trophies (
	id integer primary key,
	competition_id integer not null references competitions(id),
	place integer not null,
	user_id integer not null references users(id),
	build_id integer not null references builds(id),
	downloads_left integer not null default 1,
	seen integer not null default 0,
	unique(competition_id, place)
);
create table if not exists counters (
	name text primary key,
	count integer not null default 0
);
create table if not exists donations (
	id integer primary key autoincrement,
	external_id text not null unique,
	name text not null,
	message text not null default '',
	amount_minor integer not null,
	currency text not null,
	public integer not null default 1,
	created_at text not null
);
`

// migrations add columns to tables that shipped without them. SQLite has no
// "add column if not exists", so a duplicate column error means already done.
var migrations = []string{
	`alter table magic_tokens add column remember integer not null default 1`,
	`alter table sessions add column remember integer not null default 1`,
	`alter table users add column slug_chosen integer not null default 0`,
	`alter table users add column bio text not null default ''`,
}

func migrate(db *sql.DB) error {
	for _, statement := range migrations {
		if _, err := db.Exec(statement); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("store: migrate %q: %w", statement, err)
		}
	}
	return nil
}

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
	if err := migrate(db); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// Ping reports whether the database answers, for health checks.
func (s *Store) Ping() error {
	var one int
	return s.db.QueryRow(`select 1`).Scan(&one)
}

// Sweep removes expired sessions, spent or expired sign-in tokens, and
// counters from earlier months. Run it at startup and on a timer so the
// tables do not grow without bound.
func (s *Store) Sweep() error {
	current := now()
	if _, err := s.db.Exec(`delete from sessions where expires_at < ?`, current); err != nil {
		return fmt.Errorf("store: sweep sessions: %w", err)
	}
	if _, err := s.db.Exec(`delete from magic_tokens where used = 1 or expires_at < ?`, current); err != nil {
		return fmt.Errorf("store: sweep tokens: %w", err)
	}
	if _, err := s.db.Exec(`delete from counters where name not like ?`, "%:"+thisMonth()); err != nil {
		return fmt.Errorf("store: sweep counters: %w", err)
	}
	return nil
}

func thisMonth() string {
	return time.Now().UTC().Format("2006-01")
}

// TakeMonthly counts count uses of a metered outside service this calendar
// month and reports whether they stayed within limit. The count survives
// restarts, which is the point: it guards a bill, not a burst.
func (s *Store) TakeMonthly(name string, limit, count int) (bool, error) {
	if name == "" || limit <= 0 || count <= 0 || count > limit {
		return false, errors.New("store: bad monthly counter")
	}
	key := name + ":" + thisMonth()
	result, err := s.db.Exec(`insert into counters (name, count) values (?, ?)
		on conflict(name) do update set count = count + ? where count + ? <= ?`,
		key, count, count, count, limit)
	if err != nil {
		return false, fmt.Errorf("store: count %s: %w", name, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return changed == 1, nil
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
	GoalCount   int    // kits to finish, zero for no goal
	GoalBy      string // YYYY-MM-DD
	GoalSetAt   string // finished kits from this stamp count toward the goal
	Nudge       string // off, weekly, monthly
	NudgedAt    string
	Flair       string // small picture beside the name, empty for the default
	Avatar      string // file name of the profile picture, empty for none
	SlugChosen  bool   // the member has picked their page address, so it is fixed
	Bio         string // a line the member writes about what they build
}

const userColumns = `id, email, display_name, slug, is_admin, created_at, goal_count, goal_by, goal_set_at, nudge, nudged_at, flair, avatar, slug_chosen, bio`

func scanUsers(rows *sql.Rows) ([]User, error) {
	defer rows.Close()
	var users []User
	for rows.Next() {
		var u User
		var admin, chosen int
		err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.Slug, &admin, &u.CreatedAt,
			&u.GoalCount, &u.GoalBy, &u.GoalSetAt, &u.Nudge, &u.NudgedAt, &u.Flair, &u.Avatar, &chosen, &u.Bio)
		if err != nil {
			return nil, err
		}
		u.IsAdmin = admin != 0
		u.SlugChosen = chosen != 0
		users = append(users, u)
	}
	return users, rows.Err()
}

// CreateMagicToken issues a sign-in token. remember is the member's choice
// from the sign-in form and carries through to the session the link creates.
func (s *Store) CreateMagicToken(tokenHash, email string, remember bool) error {
	if tokenHash == "" || email == "" {
		return errors.New("store: empty magic token or email")
	}
	expires := time.Now().UTC().Add(magicTokenLifetime).Format(time.RFC3339)
	_, err := s.db.Exec(`insert into magic_tokens (token_hash, email, expires_at, remember) values (?, ?, ?, ?)`,
		tokenHash, strings.ToLower(clip(email, maxShortField)), expires, boolInt(remember))
	return err
}

// ConsumeMagicToken burns the token and returns the email it was issued for
// and whether the member asked to stay signed in. One conditional update
// does the burning, so two requests with the same link cannot both win.
func (s *Store) ConsumeMagicToken(tokenHash string) (string, bool, error) {
	if tokenHash == "" {
		return "", false, ErrNotFound
	}
	result, err := s.db.Exec(`update magic_tokens set used = 1 where token_hash = ? and used = 0 and expires_at > ?`,
		tokenHash, now())
	if err != nil {
		return "", false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return "", false, err
	}
	if changed == 0 {
		return "", false, ErrNotFound
	}
	var email string
	var remember int
	if err := s.db.QueryRow(`select email, remember from magic_tokens where token_hash = ?`, tokenHash).Scan(&email, &remember); err != nil {
		return "", false, err
	}
	return email, remember == 1, nil
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
		// Admin follows the configuration in both directions, so changing
		// SPRUE_ADMIN_EMAIL takes the right away from the old address.
		if user.IsAdmin != makeAdmin {
			if _, err := s.db.Exec(`update users set is_admin = ? where id = ?`, boolInt(makeAdmin), user.ID); err != nil {
				return User{}, err
			}
			user.IsAdmin = makeAdmin
		}
		return user, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return User{}, err
	}
	// New members start with a neutral name and address so the email's
	// local part is never published. Settings lets them pick both.
	slug, err := s.uniqueSlug("users", defaultSlugBase)
	if err != nil {
		return User{}, err
	}
	admin := 0
	if makeAdmin {
		admin = 1
	}
	result, err := s.db.Exec(`insert into users (email, display_name, slug, is_admin, created_at) values (?, ?, ?, ?, ?)`,
		email, defaultDisplayName, slug, admin, now())
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
	rows, err := s.db.Query(`select `+userColumns+` from users where `+where, arg)
	if err != nil {
		return User{}, err
	}
	users, err := scanUsers(rows)
	if err != nil {
		return User{}, err
	}
	if len(users) == 0 {
		return User{}, ErrNotFound
	}
	return users[0], nil
}

func (s *Store) UserBySlug(slug string) (User, error) { return s.userBy("slug = ?", slug) }

// MaxBioLength is how long a member's line about themselves may be.
const MaxBioLength = 200

// SetBio saves the line a member writes about what they build.
func (s *Store) SetBio(id int64, bio string) error {
	return s.updateOwned(`update users set bio = ? where id = ?`, clip(bio, MaxBioLength), id)
}

// MovePhoto shifts one photo one place earlier or later in a build's order.
// The photo at the front is the cover, so moving to the front changes it.
func (s *Store) MovePhoto(buildID int64, fileName string, later bool) error {
	names, err := s.Photos(buildID)
	if err != nil {
		return err
	}
	at := -1
	for index, name := range names {
		if name == fileName {
			at = index
			break
		}
	}
	if at < 0 {
		return ErrNotFound
	}
	swap := at - 1
	if later {
		swap = at + 1
	}
	if swap < 0 || swap >= len(names) {
		return nil
	}
	names[at], names[swap] = names[swap], names[at]
	return s.writePhotoOrder(buildID, names)
}

// RenameUser saves the display name and flair. The caller validates the
// flair against the pictures it can show.
func (s *Store) RenameUser(id int64, displayName, flair string) error {
	displayName = clip(displayName, maxShortField)
	if displayName == "" {
		return errors.New("store: empty display name")
	}
	_, err := s.db.Exec(`update users set display_name = ?, flair = ? where id = ?`, displayName, clip(flair, maxShortField), id)
	return err
}

// SetAvatar records the profile picture's file name, or clears it with "".
func (s *Store) SetAvatar(id int64, fileName string) error {
	return s.updateOwned(`update users set avatar = ? where id = ?`, clip(fileName, maxShortField), id)
}

// CreateSession opens a session. A remembered session lasts sessionLifetime
// and renews itself on use; any other ends after shortSessionLife.
func (s *Store) CreateSession(tokenHash string, userID int64, remember bool) error {
	if tokenHash == "" || userID <= 0 {
		return errors.New("store: bad session")
	}
	life := shortSessionLife
	if remember {
		life = sessionLifetime
	}
	expires := time.Now().UTC().Add(life).Format(time.RFC3339)
	_, err := s.db.Exec(`insert into sessions (token_hash, user_id, expires_at, remember) values (?, ?, ?, ?)`,
		tokenHash, userID, expires, boolInt(remember))
	return err
}

// SessionUser returns the member behind a live session. A remembered
// session past the halfway point of its life is pushed out to a full
// lifetime again, so a member who keeps visiting never has to sign in.
func (s *Store) SessionUser(tokenHash string) (User, error) {
	if tokenHash == "" {
		return User{}, ErrNotFound
	}
	var userID int64
	var expires string
	var remember int
	err := s.db.QueryRow(`select user_id, expires_at, remember from sessions where token_hash = ?`, tokenHash).
		Scan(&userID, &expires, &remember)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	when, err := time.Parse(time.RFC3339, expires)
	current := time.Now().UTC()
	if err != nil || current.After(when) {
		return User{}, ErrNotFound
	}
	if remember == 1 && when.Sub(current) < sessionLifetime/2 {
		renewed := current.Add(sessionLifetime).Format(time.RFC3339)
		if _, err := s.db.Exec(`update sessions set expires_at = ? where token_hash = ?`, renewed, tokenHash); err != nil {
			return User{}, err
		}
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
	Pinned      bool // owner keeps it at the top of their bench
	Private     bool // owner keeps it off the workbench; only they see it
	Hidden      bool // taken out of public view by the admin after a report
	CreatedAt   string
	// Filled by list queries for display.
	OwnerName   string
	OwnerSlug   string
	OwnerFlair  string
	OwnerAvatar string
	CoverPhoto  string
	TrophyPlace int // 0 = none, else 1..3 best placing this build has won
	Votes       int // community votes toward the featured spot
	Likes       int // members who liked the build, appreciation only
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
		`insert into builds (user_id, title, kit, brand, scale, description, built_on, pinned, private, created_at)
		 values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.UserID, clip(b.Title, maxShortField), clip(b.Kit, maxShortField), clip(b.Brand, maxShortField),
		clip(b.Scale, maxShortField), clip(b.Description, maxTextField), clip(b.BuiltOn, maxShortField),
		boolInt(b.Pinned), boolInt(b.Private), now())
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// UpdateBuild saves the owner's edits. A build that has entered a
// competition cannot be made private, because the entry is public.
func (s *Store) UpdateBuild(b Build) error {
	if b.ID <= 0 || b.UserID <= 0 || clip(b.Title, maxShortField) == "" || clip(b.Kit, maxShortField) == "" {
		return errors.New("store: bad build update")
	}
	if b.Private {
		entered, err := s.BuildHasEntries(b.ID)
		if err != nil {
			return err
		}
		if entered {
			return ErrInUse
		}
	}
	_, err := s.db.Exec(
		`update builds set title = ?, kit = ?, brand = ?, scale = ?, description = ?, built_on = ?, pinned = ?, private = ?
		 where id = ? and user_id = ?`,
		clip(b.Title, maxShortField), clip(b.Kit, maxShortField), clip(b.Brand, maxShortField),
		clip(b.Scale, maxShortField), clip(b.Description, maxTextField), clip(b.BuiltOn, maxShortField),
		boolInt(b.Pinned), boolInt(b.Private), b.ID, b.UserID)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

const buildColumns = `
	b.id, b.user_id, b.title, b.kit, b.brand, b.scale, b.description, b.built_on, b.pinned, b.private, b.hidden, b.created_at,
	u.display_name, u.slug, u.flair, u.avatar,
	coalesce((select p.file_name from photos p where p.build_id = b.id order by p.position limit 1), ''),
	coalesce((select min(t.place) from trophies t where t.build_id = b.id), 0),
	(select count(*) from build_votes v where v.build_id = b.id),
	(select count(*) from build_likes l where l.build_id = b.id)
`

// buildTargets says where each column of buildColumns lands, in the same
// order. Every reader of those columns uses it, so the two lists cannot
// drift apart when a column is added.
func buildTargets(b *Build, pinned, private, hidden *int) []any {
	return []any{&b.ID, &b.UserID, &b.Title, &b.Kit, &b.Brand, &b.Scale, &b.Description,
		&b.BuiltOn, pinned, private, hidden, &b.CreatedAt,
		&b.OwnerName, &b.OwnerSlug, &b.OwnerFlair, &b.OwnerAvatar,
		&b.CoverPhoto, &b.TrophyPlace, &b.Votes, &b.Likes}
}

func (s *Store) scanBuilds(rows *sql.Rows) ([]Build, error) {
	defer rows.Close()
	var builds []Build
	for rows.Next() {
		var b Build
		var pinned, private, hidden int
		err := rows.Scan(buildTargets(&b, &pinned, &private, &hidden)...)
		if err != nil {
			return nil, err
		}
		b.Pinned = pinned != 0
		b.Private = private != 0
		b.Hidden = hidden != 0
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

// BuildsForUser returns pinned builds first, then the rest, newest built first.
func (s *Store) BuildsForUser(userID int64) ([]Build, error) {
	rows, err := s.db.Query(`select `+buildColumns+` from builds b join users u on u.id = b.user_id
		where b.user_id = ?
		order by b.pinned desc, case when b.built_on = '' then b.created_at else b.built_on end desc, b.id desc
		limit ?`, userID, MaxBuildsPerUser)
	if err != nil {
		return nil, err
	}
	return s.scanBuilds(rows)
}

// RecentBuilds pages newest first. beforeID of zero starts at the top; pass
// the last ID of one page to get the next.
func (s *Store) RecentBuilds(beforeID int64, limit int) ([]Build, error) {
	if limit <= 0 || limit > 100 {
		limit = 24
	}
	if beforeID <= 0 {
		beforeID = math.MaxInt64
	}
	rows, err := s.db.Query(`select `+buildColumns+` from builds b join users u on u.id = b.user_id
		where b.id < ? and b.private = 0 and b.hidden = 0 order by b.id desc limit ?`, beforeID, limit)
	if err != nil {
		return nil, err
	}
	return s.scanBuilds(rows)
}

// NewerBuilds pages back up the feed. It returns the builds immediately
// newer than the given ID, newest first, so a reader who has paged down can
// climb back one page at a time.
func (s *Store) NewerBuilds(afterID int64, limit int) ([]Build, error) {
	if limit <= 0 || limit > 100 {
		limit = 24
	}
	if afterID <= 0 {
		return nil, nil
	}
	rows, err := s.db.Query(`select `+buildColumns+` from builds b join users u on u.id = b.user_id
		where b.id > ? and b.private = 0 and b.hidden = 0 order by b.id asc limit ?`, afterID, limit)
	if err != nil {
		return nil, err
	}
	builds, err := s.scanBuilds(rows)
	if err != nil {
		return nil, err
	}
	// The query climbs towards the newest, and every list on the site reads
	// newest first, so the page is turned over before it goes out.
	for front, back := 0, len(builds)-1; front < back; front, back = front+1, back-1 {
		builds[front], builds[back] = builds[back], builds[front]
	}
	return builds, nil
}

// MaxSearchedBuilds is how many matches one build search shows.
const MaxSearchedBuilds = 48

// SearchBuilds finds public builds whose title, kit, brand or scale contains
// the words typed, newest first. An empty search matches nothing, so the
// caller shows its usual list instead.
func (s *Store) SearchBuilds(query string, limit int) ([]Build, error) {
	if limit <= 0 || limit > MaxSearchedBuilds {
		limit = MaxSearchedBuilds
	}
	query = strings.ToLower(strings.TrimSpace(clip(query, maxShortField)))
	if query == "" {
		return nil, nil
	}
	pattern := "%" + escapeLike(query) + "%"
	rows, err := s.db.Query(`select `+buildColumns+` from builds b join users u on u.id = b.user_id
		where b.private = 0 and b.hidden = 0 and (lower(b.title) like ? escape '\'
			or lower(b.kit) like ? escape '\' or lower(b.brand) like ? escape '\'
			or lower(b.scale) like ? escape '\')
		order by b.id desc limit ?`, pattern, pattern, pattern, pattern, limit)
	if err != nil {
		return nil, err
	}
	return s.scanBuilds(rows)
}

type SitemapBuild struct {
	ID        int64
	CreatedAt string
}

// BuildsForSitemap lists the newest builds' IDs and creation stamps.
func (s *Store) BuildsForSitemap(limit int) ([]SitemapBuild, error) {
	rows, err := s.db.Query(`select id, created_at from builds where private = 0 and hidden = 0 order by id desc limit ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []SitemapBuild
	for rows.Next() {
		var b SitemapBuild
		if err := rows.Scan(&b.ID, &b.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, b)
	}
	return list, rows.Err()
}

// UserSlugs lists the newest members' page slugs.
func (s *Store) UserSlugs(limit int) ([]string, error) {
	rows, err := s.db.Query(`select slug from users order by id desc limit ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var slugs []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		slugs = append(slugs, slug)
	}
	return slugs, rows.Err()
}

// BuildHasEntries reports whether a build has ever entered a competition.
// BuildsWithEntries lists which of a member's builds are already in a
// competition, so the entry picker can say so before they pick one.
func (s *Store) BuildsWithEntries(userID int64) (map[int64]bool, error) {
	rows, err := s.db.Query(`select distinct e.build_id from entries e
		join builds b on b.id = e.build_id where b.user_id = ? limit ?`, userID, MaxBuildsPerUser)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entered := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		entered[id] = true
	}
	return entered, rows.Err()
}

func (s *Store) BuildHasEntries(id int64) (bool, error) {
	var entries int
	if err := s.db.QueryRow(`select count(*) from entries where build_id = ?`, id).Scan(&entries); err != nil {
		return false, err
	}
	return entries > 0, nil
}

// DeleteBuild removes a build and its photo records, returning the photo file
// names so the caller can remove the files. A build that has entered a
// competition stays, because entries, votes, and trophies point at it.
func (s *Store) DeleteBuild(id, userID int64) ([]string, error) {
	build, err := s.BuildByID(id)
	if err != nil {
		return nil, err
	}
	if build.UserID != userID {
		return nil, ErrNotFound
	}
	entered, err := s.BuildHasEntries(id)
	if err != nil {
		return nil, err
	}
	if entered {
		return nil, ErrInUse
	}
	photos, err := s.Photos(id)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`delete from photos where build_id = ?`, id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`delete from build_likes where build_id = ?`, id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`delete from build_votes where build_id = ?`, id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`update stash set build_id = null where build_id = ?`, id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`delete from reports where build_id = ?`, id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`delete from builds where id = ?`, id); err != nil {
		return nil, err
	}
	return photos, tx.Commit()
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

// VoteBuild records one member's vote for a build toward the featured spot.
// Members cannot vote for their own build, and one vote per build each.
func (s *Store) VoteBuild(buildID, userID int64) error {
	build, err := s.BuildByID(buildID)
	if err != nil {
		return err
	}
	if build.UserID == userID {
		return errors.New("store: no voting for your own build")
	}
	_, err = s.db.Exec(`insert or ignore into build_votes (build_id, user_id, created_at) values (?, ?, ?)`,
		buildID, userID, now())
	return err
}

func (s *Store) UnvoteBuild(buildID, userID int64) error {
	_, err := s.db.Exec(`delete from build_votes where build_id = ? and user_id = ?`, buildID, userID)
	return err
}

// LikeBuild records that a member likes a build. A like is appreciation
// only: it says nothing about the featured spot, which votes decide. Nobody
// likes their own build, and one like per build each.
func (s *Store) LikeBuild(buildID, userID int64) error {
	build, err := s.BuildByID(buildID)
	if err != nil {
		return err
	}
	if build.UserID == userID {
		return errors.New("store: no liking your own build")
	}
	var count int
	if err := s.db.QueryRow(`select count(*) from build_likes where user_id = ?`, userID).Scan(&count); err != nil {
		return err
	}
	if count >= MaxLikesPerMember {
		return ErrLimit
	}
	_, err = s.db.Exec(`insert or ignore into build_likes (build_id, user_id, created_at) values (?, ?, ?)`,
		buildID, userID, now())
	return err
}

func (s *Store) UnlikeBuild(buildID, userID int64) error {
	_, err := s.db.Exec(`delete from build_likes where build_id = ? and user_id = ?`, buildID, userID)
	return err
}

func (s *Store) HasLikedBuild(buildID, userID int64) (bool, error) {
	var count int
	err := s.db.QueryRow(`select count(*) from build_likes where build_id = ? and user_id = ?`, buildID, userID).Scan(&count)
	return count > 0, err
}

// LikedBuilds returns the builds a member has liked, most recently liked
// first. A build that has since gone private or been hidden drops out.
func (s *Store) LikedBuilds(userID int64, limit int) ([]Build, error) {
	if userID <= 0 {
		return nil, ErrNotFound
	}
	if limit <= 0 || limit > MaxLikesShown {
		limit = MaxLikesShown
	}
	rows, err := s.db.Query(`select `+buildColumns+` from build_likes l
		join builds b on b.id = l.build_id
		join users u on u.id = b.user_id
		where l.user_id = ? and b.hidden = 0 and (b.private = 0 or b.user_id = ?)
		order by l.created_at desc, b.id desc limit ?`, userID, userID, limit)
	if err != nil {
		return nil, err
	}
	return s.scanBuilds(rows)
}

func (s *Store) HasVotedBuild(buildID, userID int64) (bool, error) {
	var count int
	err := s.db.QueryRow(`select count(*) from build_votes where build_id = ? and user_id = ?`, buildID, userID).Scan(&count)
	return count > 0, err
}

// FeaturedBuilds returns the builds with the most votes cast since the given
// time, most voted first. Builds with no recent votes never feature.
func (s *Store) FeaturedBuilds(since time.Time, limit int) ([]Build, error) {
	if limit <= 0 || limit > MaxFeatured {
		limit = MaxFeatured
	}
	rows, err := s.db.Query(`with recent as (
			select build_id, count(*) as votes from build_votes where created_at >= ? group by build_id
		)
		select `+buildColumns+` from builds b join users u on u.id = b.user_id join recent r on r.build_id = b.id
		where b.private = 0 and b.hidden = 0 order by r.votes desc, b.id desc limit ?`, since.UTC().Format(time.RFC3339), limit)
	if err != nil {
		return nil, err
	}
	return s.scanBuilds(rows)
}

// RemovePhoto drops one photo and closes the gap in positions.
func (s *Store) RemovePhoto(buildID int64, fileName string) error {
	names, err := s.Photos(buildID)
	if err != nil {
		return err
	}
	kept := make([]string, 0, len(names))
	for _, name := range names {
		if name != fileName {
			kept = append(kept, name)
		}
	}
	if len(kept) == len(names) {
		return ErrNotFound
	}
	return s.writePhotoOrder(buildID, kept)
}

// SetCoverPhoto moves one photo to the front; the first photo is the cover.
func (s *Store) SetCoverPhoto(buildID int64, fileName string) error {
	names, err := s.Photos(buildID)
	if err != nil {
		return err
	}
	ordered := make([]string, 0, len(names))
	found := false
	for _, name := range names {
		if name == fileName {
			found = true
			continue
		}
		ordered = append(ordered, name)
	}
	if !found {
		return ErrNotFound
	}
	return s.writePhotoOrder(buildID, append([]string{fileName}, ordered...))
}

// writePhotoOrder replaces a build's photo rows with names in the given order.
func (s *Store) writePhotoOrder(buildID int64, names []string) error {
	if len(names) > MaxPhotosPerBuild {
		return ErrLimit
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`delete from photos where build_id = ?`, buildID); err != nil {
		return err
	}
	for position, name := range names {
		if _, err := tx.Exec(`insert into photos (build_id, position, file_name) values (?, ?, ?)`,
			buildID, position, name); err != nil {
			return err
		}
	}
	return tx.Commit()
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
	ID           int64
	Slug         string
	Title        string
	Description  string
	CreatorID    int64
	CreatorName  string
	CreatorSlug  string
	EntriesClose string // last day entries are accepted, YYYY-MM-DD
	VotingCloses string // last day votes are accepted, YYYY-MM-DD
	Entries      int    // how many builds have been entered
	Status       string // open, voting, decided
	Official     bool   // a sprue competition with a fixed brief
	Theme        string // which sprue brief, empty for a community competition
	Category     string // one of Categories, the subject members filter by
	CreatedAt    string
}

// Categories are the subjects a competition can be filed under.
var Categories = []string{"fighter", "heavy bomber", "medium bomber", "tank", "other"}

func validCategory(category string) bool {
	for _, known := range Categories {
		if category == known {
			return true
		}
	}
	return false
}

const competitionColumns = `c.id, c.slug, c.title, c.description, c.creator_id, u.display_name, u.slug,
	c.entries_close, c.voting_closes, c.status, c.official, c.theme, c.category, c.created_at,
	(select count(*) from entries e where e.competition_id = c.id)
	from competitions c join users u on u.id = c.creator_id`

const dayLayout = "2006-01-02"

// CreateCompetition opens a competition on behalf of a member. Dates are
// checked against today: entries must close in the future and within
// MaxEntryDays, voting must close after entries and within MaxVotingDays.
func (s *Store) CreateCompetition(c Competition, today time.Time) (Competition, error) {
	c.Title = clip(c.Title, maxShortField)
	c.Description = clip(c.Description, maxTextField)
	if c.Title == "" || c.CreatorID <= 0 {
		return Competition{}, errors.New("store: competition needs a title and a creator")
	}
	if !validCategory(c.Category) {
		return Competition{}, errors.New("store: competition needs a category")
	}
	if err := checkCompetitionDates(c.EntriesClose, c.VotingCloses, today); err != nil {
		return Competition{}, err
	}
	var total, running int
	if err := s.db.QueryRow(`select count(*) from competitions`).Scan(&total); err != nil {
		return Competition{}, err
	}
	if total >= MaxCompetitions {
		return Competition{}, ErrLimit
	}
	err := s.db.QueryRow(`select count(*) from competitions where creator_id = ? and status != 'decided'`, c.CreatorID).Scan(&running)
	if err != nil {
		return Competition{}, err
	}
	if running >= MaxOpenPerCreator {
		return Competition{}, ErrLimit
	}
	slug, err := s.uniqueSlug("competitions", Slugify(c.Title))
	if err != nil {
		return Competition{}, err
	}
	_, err = s.db.Exec(`insert into competitions (slug, title, description, creator_id, entries_close, voting_closes, status, official, theme, category, created_at)
		values (?, ?, ?, ?, ?, ?, 'open', ?, ?, ?, ?)`,
		slug, c.Title, c.Description, c.CreatorID, c.EntriesClose, c.VotingCloses, boolInt(c.Official), clip(c.Theme, maxShortField), c.Category, now())
	if err != nil {
		return Competition{}, err
	}
	return s.CompetitionBySlug(slug)
}

var ErrBadDates = errors.New("bad dates")

func checkCompetitionDates(entriesClose, votingCloses string, today time.Time) error {
	entries, err := time.Parse(dayLayout, entriesClose)
	if err != nil {
		return ErrBadDates
	}
	voting, err := time.Parse(dayLayout, votingCloses)
	if err != nil {
		return ErrBadDates
	}
	day := today.UTC().Truncate(24 * time.Hour)
	if !entries.After(day) || entries.Sub(day) > MaxEntryDays*24*time.Hour {
		return ErrBadDates
	}
	if !voting.After(entries) || voting.Sub(entries) > MaxVotingDays*24*time.Hour {
		return ErrBadDates
	}
	return nil
}

func (s *Store) CompetitionByID(id int64) (Competition, error) {
	return s.competitionBy("c.id = ?", id)
}

func (s *Store) CompetitionBySlug(slug string) (Competition, error) {
	return s.competitionBy("c.slug = ?", slug)
}

func (s *Store) competitionBy(where string, arg any) (Competition, error) {
	rows, err := s.db.Query(`select `+competitionColumns+` where `+where, arg)
	if err != nil {
		return Competition{}, err
	}
	list, err := scanCompetitions(rows)
	if err != nil {
		return Competition{}, err
	}
	if len(list) == 0 {
		return Competition{}, ErrNotFound
	}
	return list[0], nil
}

// Competitions lists newest first, all of them or one category.
func (s *Store) Competitions(category string) ([]Competition, error) {
	if !validCategory(category) {
		category = ""
	}
	rows, err := s.db.Query(`select `+competitionColumns+` where c.category = ? or ? = '' order by c.id desc limit ?`,
		category, category, MaxCompetitions)
	if err != nil {
		return nil, err
	}
	return scanCompetitions(rows)
}

func scanCompetitions(rows *sql.Rows) ([]Competition, error) {
	defer rows.Close()
	var list []Competition
	for rows.Next() {
		var c Competition
		var official int
		err := rows.Scan(&c.ID, &c.Slug, &c.Title, &c.Description, &c.CreatorID, &c.CreatorName, &c.CreatorSlug,
			&c.EntriesClose, &c.VotingCloses, &c.Status, &official, &c.Theme, &c.Category, &c.CreatedAt,
			&c.Entries)
		if err != nil {
			return nil, err
		}
		c.Official = official != 0
		list = append(list, c)
	}
	return list, rows.Err()
}

// ThemeStartedInMonth reports whether a sprue competition with this theme was
// already created in the month, given as YYYY-MM.
func (s *Store) ThemeStartedInMonth(theme, month string) (bool, error) {
	var count int
	err := s.db.QueryRow(`select count(*) from competitions where theme = ? and created_at like ?`, theme, month+"%").Scan(&count)
	return count > 0, err
}

// AdminUser returns the first admin account, which owns scheduled competitions.
func (s *Store) AdminUser() (User, error) {
	rows, err := s.db.Query(`select ` + userColumns + ` from users where is_admin = 1 order by id limit 1`)
	if err != nil {
		return User{}, err
	}
	users, err := scanUsers(rows)
	if err != nil {
		return User{}, err
	}
	if len(users) == 0 {
		return User{}, ErrNotFound
	}
	return users[0], nil
}

// Advance moves competitions along by date: open ones whose entry day has
// passed start voting, voting ones whose voting day has passed are decided.
// Call it on a timer and before showing competitions.
// Advance moves competitions on by date and returns the ones it decided in
// this run, so the caller can tell the people who entered them.
func (s *Store) Advance(today time.Time) ([]Competition, error) {
	day := today.UTC().Format(dayLayout)
	if _, err := s.db.Exec(`update competitions set status = 'voting' where status = 'open' and entries_close < ?`, day); err != nil {
		return nil, fmt.Errorf("store: advance to voting: %w", err)
	}
	rows, err := s.db.Query(`select id from competitions where status = 'voting' and voting_closes < ? limit ?`, day, MaxCompetitions)
	if err != nil {
		return nil, err
	}
	var due []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		due = append(due, id)
	}
	rows.Close()
	decided := make([]Competition, 0, len(due))
	for _, id := range due {
		if err := s.Decide(id); err != nil {
			return nil, fmt.Errorf("store: decide %d: %w", id, err)
		}
		comp, err := s.CompetitionByID(id)
		if err != nil {
			return nil, err
		}
		decided = append(decided, comp)
	}
	return decided, nil
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
	if build.Private {
		return errors.New("store: a private build cannot enter a competition")
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

// RemoveEntry takes an entry and its votes out of a competition that has not
// been decided, for entries that miss the brief.
func (s *Store) RemoveEntry(competitionID, entryID int64) error {
	var status string
	err := s.db.QueryRow(`select status from competitions where id = ?`, competitionID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status == "decided" {
		return ErrInUse
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`delete from votes where competition_id = ? and entry_id = ?`, competitionID, entryID); err != nil {
		return err
	}
	result, err := tx.Exec(`delete from entries where id = ? and competition_id = ?`, entryID, competitionID)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// WithdrawEntry takes the member's own build back out of a competition while
// entries are still open. Once voting starts an entry stays put, because
// votes have been cast on the field as it stands.
func (s *Store) WithdrawEntry(competitionID, userID int64) error {
	var status string
	err := s.db.QueryRow(`select status from competitions where id = ?`, competitionID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "open" {
		return ErrInUse
	}
	var entryID int64
	err = s.db.QueryRow(`select e.id from entries e join builds b on b.id = e.build_id
		where e.competition_id = ? and b.user_id = ?`, competitionID, userID).Scan(&entryID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return s.RemoveEntry(competitionID, entryID)
}

// CompetitionsForBuild lists the competitions a build has been entered in,
// newest first, so its page can say where it is running.
func (s *Store) CompetitionsForBuild(buildID int64) ([]Competition, error) {
	rows, err := s.db.Query(`select `+competitionColumns+`
		where c.id in (select e.competition_id from entries e where e.build_id = ?)
		order by c.id desc limit ?`, buildID, MaxCompetitions)
	if err != nil {
		return nil, err
	}
	return scanCompetitions(rows)
}

// EntriesWithVotes lists a competition's entries in entry order with counts.
func (s *Store) EntriesWithVotes(competitionID int64) ([]Entry, error) {
	rows, err := s.db.Query(`select e.id, `+buildColumns+`,
		(select count(*) from votes v where v.entry_id = e.id)
		from entries e join builds b on b.id = e.build_id join users u on u.id = b.user_id
		where e.competition_id = ? and b.hidden = 0 order by e.id limit ?`, competitionID, MaxEntriesPerComp)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Entry
	for rows.Next() {
		var entry Entry
		var pinned, private, hidden int
		b := &entry.Build
		targets := append([]any{&entry.ID}, buildTargets(b, &pinned, &private, &hidden)...)
		err := rows.Scan(append(targets, &entry.Votes)...)
		if err != nil {
			return nil, err
		}
		b.Pinned = pinned != 0
		b.Private = private != 0
		b.Hidden = hidden != 0
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
// Entries with zero votes never place; with no votes at all there are no
// trophies and the competition is simply closed. Deciding an already decided
// competition changes nothing.
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
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`update competitions set status = 'decided' where id = ? and status != 'decided'`, competitionID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return nil
	}
	for i, winner := range winners {
		_, err := tx.Exec(`insert into trophies (competition_id, place, user_id, build_id) values (?, ?, ?, ?)`,
			competitionID, i+1, winner.Build.UserID, winner.Build.ID)
		if err != nil {
			return err
		}
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
	OwnerName        string
	OwnerSlug        string
	OwnerFlair       string
	BuildID          int64
	BuildTitle       string
	CoverPhoto       string
	DownloadsLeft    int
}

const trophyColumns = `t.id, t.competition_id, c.slug, c.title, t.place, t.user_id, u.display_name, u.slug, u.flair,
	t.build_id, b.title,
	coalesce((select p.file_name from photos p where p.build_id = b.id order by p.position limit 1), ''),
	t.downloads_left
	from trophies t join competitions c on c.id = t.competition_id join builds b on b.id = t.build_id join users u on u.id = t.user_id`

func (s *Store) scanTrophies(rows *sql.Rows) ([]Trophy, error) {
	defer rows.Close()
	var list []Trophy
	for rows.Next() {
		var t Trophy
		err := rows.Scan(&t.ID, &t.CompetitionID, &t.CompetitionSlug, &t.CompetitionTitle,
			&t.Place, &t.UserID, &t.OwnerName, &t.OwnerSlug, &t.OwnerFlair, &t.BuildID, &t.BuildTitle, &t.CoverPhoto, &t.DownloadsLeft)
		if err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

// UnseenTrophy returns one trophy the member has not been told about yet and
// marks it seen, so the site congratulates them once.
func (s *Store) UnseenTrophy(userID int64) (Trophy, error) {
	rows, err := s.db.Query(`select `+trophyColumns+` where t.user_id = ? and t.seen = 0 order by t.id limit 1`, userID)
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
	_, err = s.db.Exec(`update trophies set seen = 1 where id = ?`, list[0].ID)
	return list[0], err
}

// DecidedCompetitions lists finished competitions, newest first.
func (s *Store) DecidedCompetitions(limit int) ([]Competition, error) {
	if limit <= 0 || limit > MaxCompetitions {
		limit = MaxCompetitions
	}
	rows, err := s.db.Query(`select `+competitionColumns+` where c.status = 'decided' order by c.voting_closes desc, c.id desc limit ?`, limit)
	if err != nil {
		return nil, err
	}
	return scanCompetitions(rows)
}

// Entrant is one member who put a build into a competition, with the place
// they took, for the email that goes out when a competition is decided.
type Entrant struct {
	Email       string
	DisplayName string
	BuildTitle  string
	Place       int   // 0 for no placing
	TrophyID    int64 // 0 unless they placed
}

// Entrants lists everyone who entered a competition, with any placing.
// Bounded by the entry cap.
func (s *Store) Entrants(competitionID int64) ([]Entrant, error) {
	rows, err := s.db.Query(`select u.email, u.display_name, b.title,
		coalesce(t.place, 0), coalesce(t.id, 0)
		from entries e
		join builds b on b.id = e.build_id
		join users u on u.id = b.user_id
		left join trophies t on t.competition_id = e.competition_id and t.build_id = b.id
		where e.competition_id = ? order by coalesce(t.place, 4), e.id limit ?`,
		competitionID, MaxEntriesPerComp)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Entrant
	for rows.Next() {
		var one Entrant
		if err := rows.Scan(&one.Email, &one.DisplayName, &one.BuildTitle, &one.Place, &one.TrophyID); err != nil {
			return nil, err
		}
		list = append(list, one)
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
// New members get this name and a slug of builder, builder-2, and so on
// until they choose their own in settings.
const (
	defaultDisplayName = "Builder"
	defaultSlugBase    = "builder"
	minSlugLength      = 3
	maxSlugLength      = 40
)

// reservedSlugPattern is the shape given out on sign-up. Nobody may take
// one, so the next new member always has a free address waiting.
var reservedSlugPattern = regexp.MustCompile(`^builder(-[0-9]+)?$`)

// SetSlug lets a member choose their page address once. Every account
// starts unchosen, including those made before addresses were pickable, so
// each member gets exactly one choice. After that it is permanent and
// shared links keep working. A taken address surfaces as the unique
// constraint error.
func (s *Store) SetSlug(userID int64, slug string) error {
	if userID <= 0 || slug != Slugify(slug) || len(slug) < minSlugLength || len(slug) > maxSlugLength || reservedSlugPattern.MatchString(slug) {
		return errors.New("store: bad slug")
	}
	result, err := s.db.Exec(`update users set slug = ?, slug_chosen = 1 where id = ? and slug_chosen = 0`,
		slug, userID)
	if err != nil {
		return fmt.Errorf("store: set slug: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return ErrNotFound
	}
	return nil
}

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
