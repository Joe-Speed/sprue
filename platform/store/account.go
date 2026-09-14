package store

import (
	"database/sql"
	"fmt"
	"strconv"
)

// RemovedFiles lists the files a deleted account left on disk, for the
// caller to unlink once the database no longer refers to them.
type RemovedFiles struct {
	Photos []RemovedPhoto
	Avatar string
}

type RemovedPhoto struct {
	BuildID  int64
	FileName string
}

// DeleteAccount removes everything a member owns and leaves a blank user
// row behind, because competitions they started and podium places they
// won still point at it. Builds that entered a competition stay as hidden
// empty rows for the same reason; all other builds go entirely.
func (s *Store) DeleteAccount(userID int64) (RemovedFiles, error) {
	user, err := s.userBy("id = ?", userID)
	if err != nil {
		return RemovedFiles{}, err
	}
	removed := RemovedFiles{Avatar: user.Avatar}
	buildIDs, err := s.ids(`select id from builds where user_id = ? order by id limit ?`, userID, MaxBuildsPerUser)
	if err != nil {
		return RemovedFiles{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return RemovedFiles{}, err
	}
	defer tx.Rollback()
	for _, buildID := range buildIDs {
		photos, err := removeBuildContent(tx, buildID)
		if err != nil {
			return RemovedFiles{}, fmt.Errorf("store: delete account build %d: %w", buildID, err)
		}
		for _, name := range photos {
			removed.Photos = append(removed.Photos, RemovedPhoto{BuildID: buildID, FileName: name})
		}
	}
	if err := removeMemberRows(tx, userID, user.Email); err != nil {
		return RemovedFiles{}, fmt.Errorf("store: delete account %d: %w", userID, err)
	}
	if err := blankUser(tx, userID); err != nil {
		return RemovedFiles{}, fmt.Errorf("store: blank user %d: %w", userID, err)
	}
	return removed, tx.Commit()
}

// removeBuildContent drops a build's photos, likes, votes, and reports and
// returns the photo file names. The build row itself goes unless a
// competition entry points at it, in which case it is emptied and hidden.
func removeBuildContent(tx *sql.Tx, buildID int64) ([]string, error) {
	rows, err := tx.Query(`select file_name from photos where build_id = ? limit ?`, buildID, MaxPhotosPerBuild)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, 8)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		names = append(names, name)
	}
	rows.Close()
	for _, statement := range []string{
		`delete from photos where build_id = ?`,
		`delete from build_likes where build_id = ?`,
		`delete from build_votes where build_id = ?`,
		`delete from reports where build_id = ?`,
		`update stash set build_id = null where build_id = ?`,
	} {
		if _, err := tx.Exec(statement, buildID); err != nil {
			return nil, err
		}
	}
	var entries int
	if err := tx.QueryRow(`select count(*) from entries where build_id = ?`, buildID).Scan(&entries); err != nil {
		return nil, err
	}
	if entries > 0 {
		_, err := tx.Exec(`update builds set title = 'Removed build', kit = '', brand = '', scale = '', description = '', built_on = '', pinned = 0, hidden = 1 where id = ?`, buildID)
		return names, err
	}
	_, err = tx.Exec(`delete from builds where id = ?`, buildID)
	return names, err
}

func removeMemberRows(tx *sql.Tx, userID int64, email string) error {
	if _, err := tx.Exec(`delete from magic_tokens where email = ?`, email); err != nil {
		return err
	}
	if _, err := tx.Exec(`delete from journal where stash_id in (select id from stash where user_id = ?)`, userID); err != nil {
		return err
	}
	for _, statement := range []string{
		`delete from sessions where user_id = ?`,
		`delete from stash where user_id = ?`,
		`delete from friendships where requester_id = ?1 or addressee_id = ?1`,
		`delete from build_likes where user_id = ?`,
		`delete from build_votes where user_id = ?`,
		`delete from reports where user_id = ?`,
		`delete from votes where user_id = ?`,
	} {
		if _, err := tx.Exec(statement, userID); err != nil {
			return err
		}
	}
	return nil
}

// blankUser keeps the row so foreign keys hold, with nothing personal left
// in it. The email is replaced by an address nobody can sign in with.
func blankUser(tx *sql.Tx, userID int64) error {
	tag := strconv.FormatInt(userID, 10)
	_, err := tx.Exec(`update users set email = ?, display_name = 'Removed member', slug = ?, is_admin = 0,
		goal_count = 0, goal_by = '', goal_set_at = '', nudge = 'off', nudged_at = '', flair = '', avatar = '', bio = '', slug_chosen = 1
		where id = ?`, "removed-"+tag+"@removed.invalid", "removed-"+tag, userID)
	return err
}
