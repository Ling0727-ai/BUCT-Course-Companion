package portable

type Alarm struct {
	Key, ID string
	At      int64
}

func (s *Store) Alarms() ([]Alarm, error) {
	rows, err := s.db.Query(`SELECT key,id,at FROM portable_alarm`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var alarms []Alarm
	for rows.Next() {
		var alarm Alarm
		if err := rows.Scan(&alarm.Key, &alarm.ID, &alarm.At); err != nil {
			return nil, err
		}
		alarms = append(alarms, alarm)
	}
	return alarms, rows.Err()
}

func (s *Store) SaveAlarm(alarm Alarm) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO portable_alarm VALUES(?,?,?)`, alarm.Key, alarm.ID, alarm.At)
	return err
}

func (s *Store) DropAlarm(key string) error {
	_, err := s.db.Exec(`DELETE FROM portable_alarm WHERE key=?`, key)
	return err
}

func (s *Store) TaskNoticeSent(t Task, hours int) bool {
	var found int
	return s.db.QueryRow(`SELECT 1 FROM sent_notice WHERE task_id=? AND deadline=? AND hours=?`, t.ID, t.Deadline, hours).Scan(&found) == nil
}

func (s *Store) ScheduleNoticeSent(key string) bool {
	var found int
	return s.db.QueryRow(`SELECT 1 FROM courseflow_notice WHERE id=?`, key).Scan(&found) == nil
}
