package store

import (
	"database/sql"
	"errors"
	"time"
)

const (
	MaxStashPerUser   = 500
	MaxJournalPerItem = 200
	NudgeOff          = "off"
	NudgeWeekly       = "weekly"
	NudgeMonthly      = "monthly"
)

// StashItem is a kit a member owns. Unbuilt and building items are the
// member's model debt; a built item points at the build it became.
type StashItem struct {
	ID         int64
	UserID     int64
	Title      string
	Brand      string
	Scale      string
	Note       string
	CostPence  int64
	Status     string // unbuilt, building, built
	Next       bool   // the one kit the member has chosen to build next
	BuildID    int64  // set once built
	AddedAt    string
	FinishedAt string
}

const stashColumns = `id, user_id, title, brand, scale, note, cost_pence, status, next, coalesce(build_id, 0), added_at, finished_at`

func scanStash(rows *sql.Rows) ([]StashItem, error) {
	defer rows.Close()
	var items []StashItem
	for rows.Next() {
		var item StashItem
		var next int
		err := rows.Scan(&item.ID, &item.UserID, &item.Title, &item.Brand, &item.Scale, &item.Note,
			&item.CostPence, &item.Status, &next, &item.BuildID, &item.AddedAt, &item.FinishedAt)
		if err != nil {
			return nil, err
		}
		item.Next = next != 0
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateStashItem(item StashItem) (int64, error) {
	item.Title = clip(item.Title, maxShortField)
	if item.UserID <= 0 || item.Title == "" || item.CostPence < 0 {
		return 0, errors.New("store: stash item needs an owner, a title, and a cost of zero or more")
	}
	var count int
	if err := s.db.QueryRow(`select count(*) from stash where user_id = ?`, item.UserID).Scan(&count); err != nil {
		return 0, err
	}
	if count >= MaxStashPerUser {
		return 0, ErrLimit
	}
	result, err := s.db.Exec(`insert into stash (user_id, title, brand, scale, note, cost_pence, status, added_at)
		values (?, ?, ?, ?, ?, ?, 'unbuilt', ?)`,
		item.UserID, item.Title, clip(item.Brand, maxShortField), clip(item.Scale, maxShortField),
		clip(item.Note, maxTextField), item.CostPence, now())
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// StashForUser lists the next kit first, then building, then unbuilt oldest
// first, then built newest first.
func (s *Store) StashForUser(userID int64) ([]StashItem, error) {
	rows, err := s.db.Query(`select `+stashColumns+` from stash where user_id = ?
		order by next desc, case status when 'building' then 0 when 'unbuilt' then 1 else 2 end,
		case when status = 'built' then finished_at end desc, added_at, id limit ?`, userID, MaxStashPerUser)
	if err != nil {
		return nil, err
	}
	return scanStash(rows)
}

func (s *Store) StashItemFor(id, userID int64) (StashItem, error) {
	rows, err := s.db.Query(`select `+stashColumns+` from stash where id = ? and user_id = ?`, id, userID)
	if err != nil {
		return StashItem{}, err
	}
	items, err := scanStash(rows)
	if err != nil {
		return StashItem{}, err
	}
	if len(items) == 0 {
		return StashItem{}, ErrNotFound
	}
	return items[0], nil
}

// SetStashStatus moves an item between unbuilt and building. Built is reached
// only through FinishStashItem.
func (s *Store) SetStashStatus(id, userID int64, status string) error {
	if status != "unbuilt" && status != "building" {
		return errors.New("store: bad stash status")
	}
	return s.updateOwned(`update stash set status = ? where id = ? and user_id = ? and status != 'built'`, status, id, userID)
}

// SetStashNext marks one item as the kit to build next and clears the rest.
func (s *Store) SetStashNext(id, userID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`update stash set next = 0 where user_id = ?`, userID); err != nil {
		return err
	}
	result, err := tx.Exec(`update stash set next = 1 where id = ? and user_id = ? and status != 'built'`, id, userID)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// FinishStashItem turns a kit into a posted build, returning the new build's
// ID. The item keeps its journal and points at the build.
func (s *Store) FinishStashItem(id, userID int64) (int64, error) {
	item, err := s.StashItemFor(id, userID)
	if err != nil {
		return 0, err
	}
	if item.Status == "built" {
		return item.BuildID, nil
	}
	kit := item.Title
	if item.Brand != "" || item.Scale != "" {
		kit = joinNonEmpty(item.Brand, item.Scale, item.Title)
	}
	buildID, err := s.CreateBuild(Build{UserID: userID, Title: item.Title, Kit: kit, Brand: item.Brand, Scale: item.Scale, BuiltOn: time.Now().UTC().Format(dayLayout)})
	if err != nil {
		return 0, err
	}
	err = s.updateOwned(`update stash set status = 'built', next = 0, build_id = ?, finished_at = ? where id = ? and user_id = ?`,
		buildID, now(), id, userID)
	return buildID, err
}

func joinNonEmpty(parts ...string) string {
	out := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		if out != "" {
			out += " "
		}
		out += part
	}
	return out
}

func (s *Store) DeleteStashItem(id, userID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`delete from journal where stash_id in (select id from stash where id = ? and user_id = ?)`, id, userID); err != nil {
		return err
	}
	result, err := tx.Exec(`delete from stash where id = ? and user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// updateOwned runs an update that must touch exactly one of the caller's rows.
func (s *Store) updateOwned(query string, args ...any) error {
	result, err := s.db.Exec(query, args...)
	if err != nil {
		return err
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

type JournalEntry struct {
	ID        int64
	StashID   int64
	Text      string
	CreatedAt string
}

func (s *Store) AddJournalEntry(stashID, userID int64, text string) error {
	text = clip(text, maxTextField)
	if text == "" {
		return errors.New("store: empty journal entry")
	}
	if _, err := s.StashItemFor(stashID, userID); err != nil {
		return err
	}
	var count int
	if err := s.db.QueryRow(`select count(*) from journal where stash_id = ?`, stashID).Scan(&count); err != nil {
		return err
	}
	if count >= MaxJournalPerItem {
		return ErrLimit
	}
	_, err := s.db.Exec(`insert into journal (stash_id, text, created_at) values (?, ?, ?)`, stashID, text, now())
	return err
}

// JournalFor lists an item's entries, newest first.
func (s *Store) JournalFor(stashID int64) ([]JournalEntry, error) {
	rows, err := s.db.Query(`select id, stash_id, text, created_at from journal where stash_id = ? order by id desc limit ?`,
		stashID, MaxJournalPerItem)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []JournalEntry
	for rows.Next() {
		var entry JournalEntry
		if err := rows.Scan(&entry.ID, &entry.StashID, &entry.Text, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// SetGoal records how many kits the member means to finish and by when.
// A count of zero clears the goal.
func (s *Store) SetGoal(userID int64, count int, by string) error {
	if count < 0 || count > MaxStashPerUser {
		return errors.New("store: bad goal")
	}
	if count == 0 {
		return s.updateOwned(`update users set goal_count = 0, goal_by = '', goal_set_at = '' where id = ?`, userID)
	}
	if _, err := time.Parse(dayLayout, by); err != nil {
		return ErrBadDates
	}
	return s.updateOwned(`update users set goal_count = ?, goal_by = ?, goal_set_at = ? where id = ?`, count, by, now(), userID)
}

// FinishedSince counts kits the member finished on or after a stamp, which
// is progress toward a goal set at that stamp.
func (s *Store) FinishedSince(userID int64, since string) (int, error) {
	var count int
	err := s.db.QueryRow(`select count(*) from stash where user_id = ? and status = 'built' and finished_at >= ?`, userID, since).Scan(&count)
	return count, err
}

// SetNudge sets how often the member is emailed about their stash. The clock
// starts now, so the first email comes one period after opting in.
func (s *Store) SetNudge(userID int64, frequency string) error {
	if frequency != NudgeOff && frequency != NudgeWeekly && frequency != NudgeMonthly {
		return errors.New("store: bad nudge frequency")
	}
	return s.updateOwned(`update users set nudge = ?, nudged_at = ? where id = ?`, frequency, now(), userID)
}

// UsersDueNudge lists members whose last nudge is older than their chosen
// period. Callers mark each one nudged after sending.
func (s *Store) UsersDueNudge(at time.Time, limit int) ([]User, error) {
	weekAgo := at.UTC().Add(-7 * 24 * time.Hour).Format(time.RFC3339)
	monthAgo := at.UTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	rows, err := s.db.Query(`select `+userColumns+` from users
		where (nudge = 'weekly' and nudged_at < ?) or (nudge = 'monthly' and nudged_at < ?)
		order by nudged_at limit ?`, weekAgo, monthAgo, limit)
	if err != nil {
		return nil, err
	}
	return scanUsers(rows)
}

func (s *Store) MarkNudged(userID int64) error {
	return s.updateOwned(`update users set nudged_at = ? where id = ?`, now(), userID)
}
