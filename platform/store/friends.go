package store

import (
	"database/sql"
	"errors"
	"strings"
)

const (
	MaxFriendsPerUser = 500
	MaxSearchResults  = 50
	FriendsNone       = ""          // no row between the two members
	FriendsRequested  = "requested" // the viewer asked, waiting on the other
	FriendsIncoming   = "incoming"  // the other asked, waiting on the viewer
	FriendsAccepted   = "friends"
)

// Member is a user as shown in lists: search results and friend lists.
type Member struct {
	ID          int64
	DisplayName string
	Slug        string
	CreatedAt   string
	BuildCount  int // public builds only
}

const memberColumns = `u.id, u.display_name, u.slug, u.created_at,
	(select count(*) from builds b where b.user_id = u.id and b.private = 0 and b.hidden = 0)`

func scanMembers(rows *sql.Rows) ([]Member, error) {
	defer rows.Close()
	var list []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.DisplayName, &m.Slug, &m.CreatedAt, &m.BuildCount); err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	return list, rows.Err()
}

// SearchMembers finds members whose display name or slug contains the query,
// case insensitively. An empty query lists the newest members.
func (s *Store) SearchMembers(query string, limit int) ([]Member, error) {
	if limit <= 0 || limit > MaxSearchResults {
		limit = MaxSearchResults
	}
	query = strings.ToLower(strings.TrimSpace(clip(query, maxShortField)))
	if query == "" {
		rows, err := s.db.Query(`select `+memberColumns+` from users u order by u.id desc limit ?`, limit)
		if err != nil {
			return nil, err
		}
		return scanMembers(rows)
	}
	pattern := "%" + escapeLike(query) + "%"
	rows, err := s.db.Query(`select `+memberColumns+` from users u
		where lower(u.display_name) like ? escape '\' or u.slug like ? escape '\'
		order by u.display_name limit ?`, pattern, pattern, limit)
	if err != nil {
		return nil, err
	}
	return scanMembers(rows)
}

func escapeLike(text string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(text)
}

// RequestFriend records that from wants to be friends with to. The pair index
// refuses a second row in either direction, which surfaces as ErrInUse.
func (s *Store) RequestFriend(from, to int64) error {
	if from <= 0 || to <= 0 || from == to {
		return errors.New("store: bad friend request")
	}
	var count int
	if err := s.db.QueryRow(`select count(*) from friendships where requester_id = ? or addressee_id = ?`, from, from).Scan(&count); err != nil {
		return err
	}
	if count >= MaxFriendsPerUser {
		return ErrLimit
	}
	if _, err := s.userBy("id = ?", to); err != nil {
		return err
	}
	_, err := s.db.Exec(`insert into friendships (requester_id, addressee_id, status, created_at) values (?, ?, 'pending', ?)`,
		from, to, now())
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return ErrInUse
	}
	return err
}

// AcceptFriend confirms a request that was sent to userID by requesterID.
func (s *Store) AcceptFriend(userID, requesterID int64) error {
	return s.updateOwned(`update friendships set status = 'accepted' where requester_id = ? and addressee_id = ? and status = 'pending'`,
		requesterID, userID)
}

// RemoveFriend deletes whatever exists between the two, in either direction:
// a request either of them sent, or an accepted friendship.
func (s *Store) RemoveFriend(userID, otherID int64) error {
	return s.updateOwned(`delete from friendships where (requester_id = ? and addressee_id = ?) or (requester_id = ? and addressee_id = ?)`,
		userID, otherID, otherID, userID)
}

// Friendship reports where the viewer stands with another member.
func (s *Store) Friendship(viewerID, otherID int64) (string, error) {
	var requester int64
	var status string
	err := s.db.QueryRow(`select requester_id, status from friendships
		where (requester_id = ? and addressee_id = ?) or (requester_id = ? and addressee_id = ?)`,
		viewerID, otherID, otherID, viewerID).Scan(&requester, &status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return FriendsNone, nil
		}
		return FriendsNone, err
	}
	if status == "accepted" {
		return FriendsAccepted, nil
	}
	if requester == viewerID {
		return FriendsRequested, nil
	}
	return FriendsIncoming, nil
}

// Friends lists accepted friends by name.
func (s *Store) Friends(userID int64) ([]Member, error) {
	rows, err := s.db.Query(`select `+memberColumns+` from friendships f
		join users u on u.id = case when f.requester_id = ? then f.addressee_id else f.requester_id end
		where (f.requester_id = ? or f.addressee_id = ?) and f.status = 'accepted'
		order by u.display_name limit ?`, userID, userID, userID, MaxFriendsPerUser)
	if err != nil {
		return nil, err
	}
	return scanMembers(rows)
}

// FriendRequests lists members waiting on the user's answer, newest first.
func (s *Store) FriendRequests(userID int64) ([]Member, error) {
	rows, err := s.db.Query(`select `+memberColumns+` from friendships f join users u on u.id = f.requester_id
		where f.addressee_id = ? and f.status = 'pending' order by f.created_at desc limit ?`, userID, MaxFriendsPerUser)
	if err != nil {
		return nil, err
	}
	return scanMembers(rows)
}

// SentRequests lists members the user has asked and who have not answered.
func (s *Store) SentRequests(userID int64) ([]Member, error) {
	rows, err := s.db.Query(`select `+memberColumns+` from friendships f join users u on u.id = f.addressee_id
		where f.requester_id = ? and f.status = 'pending' order by f.created_at desc limit ?`, userID, MaxFriendsPerUser)
	if err != nil {
		return nil, err
	}
	return scanMembers(rows)
}

func (s *Store) PendingRequestCount(userID int64) (int, error) {
	var count int
	err := s.db.QueryRow(`select count(*) from friendships where addressee_id = ? and status = 'pending'`, userID).Scan(&count)
	return count, err
}
