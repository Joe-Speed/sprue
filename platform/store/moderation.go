package store

import "errors"

const maxReportReason = 500

// ReportBuild records that a member flagged a build. One report per member
// per build; a second attempt is ignored.
func (s *Store) ReportBuild(buildID, userID int64, reason string) error {
	build, err := s.BuildByID(buildID)
	if err != nil {
		return err
	}
	if build.UserID == userID {
		return errors.New("store: cannot report your own build")
	}
	_, err = s.db.Exec(`insert or ignore into reports (build_id, user_id, reason, created_at) values (?, ?, ?, ?)`,
		buildID, userID, clip(reason, maxReportReason), now())
	return err
}

// Report is one build's open reports, for the admin page.
type Report struct {
	BuildID    int64
	BuildTitle string
	OwnerName  string
	Hidden     bool
	Count      int
	Reason     string // the most recent reason given, if any
	LatestAt   string
}

func (s *Store) OpenReports() ([]Report, error) {
	rows, err := s.db.Query(`select b.id, b.title, u.display_name, b.hidden, count(*), max(r.created_at),
		coalesce((select reason from reports where build_id = b.id and reason != '' order by id desc limit 1), '')
		from reports r join builds b on b.id = r.build_id join users u on u.id = b.user_id
		group by b.id order by max(r.created_at) desc limit ?`, MaxCompetitions)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Report
	for rows.Next() {
		var report Report
		var hidden int
		if err := rows.Scan(&report.BuildID, &report.BuildTitle, &report.OwnerName, &hidden, &report.Count, &report.LatestAt, &report.Reason); err != nil {
			return nil, err
		}
		report.Hidden = hidden != 0
		list = append(list, report)
	}
	return list, rows.Err()
}

// SetBuildHidden takes a build out of every public list and page while its
// owner can still see it. Hiding keeps the reports; dismissing clears them.
func (s *Store) SetBuildHidden(buildID int64, hidden bool) error {
	return s.updateOwned(`update builds set hidden = ? where id = ?`, boolInt(hidden), buildID)
}

func (s *Store) DismissReports(buildID int64) error {
	_, err := s.db.Exec(`delete from reports where build_id = ?`, buildID)
	return err
}
