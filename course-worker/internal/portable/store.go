package portable

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"buct-course-local/worker/internal/course"
	"buct-course-local/worker/internal/courseflow"
	_ "github.com/mattn/go-sqlite3"
)

type Store struct{ db *sql.DB }

type Task struct {
	course.Task
	Completed bool
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`
		PRAGMA journal_mode=WAL;
		PRAGMA foreign_keys=ON;
		PRAGMA busy_timeout=5000;
		CREATE TABLE IF NOT EXISTS task (
			id TEXT PRIMARY KEY, kind TEXT NOT NULL, subject TEXT NOT NULL, title TEXT NOT NULL,
			details TEXT NOT NULL, url TEXT NOT NULL, deadline INTEGER NOT NULL,
			completed INTEGER NOT NULL DEFAULT 0, listed INTEGER NOT NULL DEFAULT 1);
		CREATE TABLE IF NOT EXISTS task_resource (task_id TEXT PRIMARY KEY REFERENCES task(id) ON DELETE CASCADE,
			course_id TEXT NOT NULL,item_id TEXT NOT NULL,url TEXT NOT NULL);
		CREATE TABLE IF NOT EXISTS task_filter (kind TEXT NOT NULL,key TEXT NOT NULL,title TEXT NOT NULL,PRIMARY KEY(kind,key));
		CREATE TABLE IF NOT EXISTS setting (key TEXT PRIMARY KEY,value INTEGER NOT NULL);
		CREATE TABLE IF NOT EXISTS sent_notice (task_id TEXT NOT NULL REFERENCES task(id) ON DELETE CASCADE,
			deadline INTEGER NOT NULL,hours INTEGER NOT NULL,PRIMARY KEY(task_id,deadline,hours));
		CREATE TABLE IF NOT EXISTS courseflow_state (id INTEGER PRIMARY KEY CHECK(id=1), revision INTEGER NOT NULL,payload TEXT NOT NULL);
		CREATE TABLE IF NOT EXISTS courseflow_notice (id TEXT PRIMARY KEY,sent_at INTEGER NOT NULL);
		CREATE TABLE IF NOT EXISTS course_cache (key TEXT PRIMARY KEY,payload TEXT NOT NULL);
		CREATE TABLE IF NOT EXISTS portable_vault (id INTEGER PRIMARY KEY CHECK(id=1),payload BLOB NOT NULL);
		CREATE TABLE IF NOT EXISTS portable_owner (id INTEGER PRIMARY KEY CHECK(id=1),fingerprint TEXT NOT NULL);
		CREATE TABLE IF NOT EXISTS portable_alarm (key TEXT PRIMARY KEY,id TEXT NOT NULL,at INTEGER NOT NULL);
	`); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Tasks() ([]Task, error) {
	rows, err := s.db.Query(`SELECT t.id,t.kind,t.subject,t.title,t.details,t.url,t.deadline,t.completed,r.course_id,r.item_id,r.url
		FROM task t LEFT JOIN task_resource r ON r.task_id=t.id WHERE t.listed=1
		AND NOT EXISTS (SELECT 1 FROM task_filter f WHERE (f.kind IN ('task','todo') AND f.key=t.id)
		OR (f.kind='course' AND t.kind<>'todo' AND (f.key='id:'||r.course_id OR f.key='name:'||t.subject)))
		ORDER BY t.completed,t.deadline<=0,t.deadline,t.title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Task
	for rows.Next() {
		var t Task
		var c, i, u sql.NullString
		if err := rows.Scan(&t.ID, &t.Kind, &t.Subject, &t.Title, &t.Details, &t.URL, &t.Deadline, &t.Completed, &c, &i, &u); err != nil {
			return nil, err
		}
		if c.Valid {
			t.Resource = &course.TaskResource{CourseID: c.String, ItemID: i.String, URL: u.String}
		}
		items = append(items, t)
	}
	return items, rows.Err()
}

func upsert(tx *sql.Tx, t course.Task) error {
	if _, err := tx.Exec(`INSERT INTO task (id,kind,subject,title,details,url,deadline) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET subject=excluded.subject,title=excluded.title,details=excluded.details,
		url=excluded.url,deadline=excluded.deadline,listed=1`, t.ID, t.Kind, t.Subject, t.Title, t.Details, t.URL, t.Deadline); err != nil {
		return err
	}
	if t.Resource != nil {
		_, err := tx.Exec(`INSERT OR REPLACE INTO task_resource VALUES(?,?,?,?)`, t.ID, t.Resource.CourseID, t.Resource.ItemID, t.Resource.URL)
		return err
	}
	return nil
}

func (s *Store) SaveSync(result course.Result, now int64) error {
	if result.Error != "" || result.NeedsLogin {
		return errors.New("同步未成功，已保留上次数据")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, kind := range []string{"homework", "test"} {
		if !contains(result.IncompleteKinds, kind) {
			if _, err := tx.Exec(`UPDATE task SET listed=0 WHERE kind=?`, kind); err != nil {
				return err
			}
		}
	}
	for _, task := range result.Tasks {
		if err := upsert(tx, task); err != nil {
			return err
		}
	}
	if len(result.IncompleteKinds) == 0 {
		if _, err = tx.Exec(`INSERT OR REPLACE INTO setting VALUES('last_sync',?)`, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func (s *Store) SaveTodo(task course.Task) error {
	if task.Kind != "todo" || strings.TrimSpace(task.Title) == "" {
		return errors.New("请填写待办标题")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := upsert(tx, task); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Complete(task Task) error {
	_, err := s.db.Exec(`UPDATE task SET completed=? WHERE id=?`, !task.Completed, task.ID)
	return err
}

type Filter struct{ Kind, Key, Title string }

func (s *Store) Hide(filter Filter) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO task_filter VALUES(?,?,?)`, filter.Kind, filter.Key, filter.Title)
	return err
}

func (s *Store) Restore(filter Filter) error {
	_, err := s.db.Exec(`DELETE FROM task_filter WHERE kind=? AND key=?`, filter.Kind, filter.Key)
	return err
}

func (s *Store) Filters() ([]Filter, error) {
	rows, err := s.db.Query(`SELECT kind,key,title FROM task_filter ORDER BY kind,title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Filter
	for rows.Next() {
		var f Filter
		if err := rows.Scan(&f.Kind, &f.Key, &f.Title); err != nil {
			return nil, err
		}
		result = append(result, f)
	}
	return result, rows.Err()
}

func (s *Store) Schedule() (courseflow.State, error) {
	var payload string
	err := s.db.QueryRow(`SELECT payload FROM courseflow_state WHERE id=1`).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return courseflow.DefaultState(), nil
	}
	if err != nil {
		return courseflow.State{}, err
	}
	var state courseflow.State
	err = json.Unmarshal([]byte(payload), &state)
	return state, err
}

func (s *Store) SaveSchedule(state courseflow.State) error {
	if err := state.Validate(); err != nil {
		return err
	}
	revision := state.Revision
	state.Revision++
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	result, err := s.db.Exec(`INSERT INTO courseflow_state (id,revision,payload) VALUES (1,?,?)
		ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,payload=excluded.payload WHERE courseflow_state.revision=?`, state.Revision, payload, revision)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("课表已更新，请重新打开编辑窗口")
	}
	return nil
}

func (s *Store) Cache(key string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT OR REPLACE INTO course_cache VALUES(?,?)`, key, payload)
	return err
}

func (s *Store) ReadCache(key string, target any) error {
	var payload string
	if err := s.db.QueryRow(`SELECT payload FROM course_cache WHERE key=?`, key).Scan(&payload); err != nil {
		return err
	}
	return json.Unmarshal([]byte(payload), target)
}

func (s *Store) Setting(key string, fallback int64) int64 {
	var value int64
	if err := s.db.QueryRow(`SELECT value FROM setting WHERE key=?`, key).Scan(&value); err != nil {
		return fallback
	}
	return value
}

func (s *Store) SetSetting(key string, value int64) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO setting VALUES(?,?)`, key, value)
	return err
}

func (s *Store) SaveAccount(studentID string, value []byte) error {
	owner := fmt.Sprintf("%x", sha256.Sum256([]byte(studentID)))
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous string
	err = tx.QueryRow(`SELECT fingerprint FROM portable_owner WHERE id=1`).Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if previous != "" && previous != owner {
		if err := clearSchool(tx); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`INSERT OR REPLACE INTO portable_owner VALUES(1,?)`, owner); err != nil {
		return err
	}
	if len(value) == 0 {
		_, err = tx.Exec(`DELETE FROM portable_vault`)
	} else {
		_, err = tx.Exec(`INSERT OR REPLACE INTO portable_vault VALUES(1,?)`, value)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Vault() ([]byte, error) {
	var data []byte
	err := s.db.QueryRow(`SELECT payload FROM portable_vault WHERE id=1`).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return data, err
}

func (s *Store) SignOut() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := clearSchool(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func clearSchool(tx *sql.Tx) error {
	for _, command := range []string{`DELETE FROM portable_vault`, `DELETE FROM portable_owner`, `DELETE FROM task WHERE kind<>'todo'`,
		`DELETE FROM setting WHERE key='last_sync'`, `DELETE FROM task_filter WHERE kind<>'todo'`,
		`DELETE FROM course_cache WHERE key<>'schedule'`} {
		if _, err := tx.Exec(command); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) MarkTaskNotice(t Task, hours int) (bool, error) {
	result, err := s.db.Exec(`INSERT OR IGNORE INTO sent_notice VALUES(?,?,?)`, t.ID, t.Deadline, hours)
	return newRow(result, err)
}

func (s *Store) MarkScheduleNotice(key string, now int64) (bool, error) {
	result, err := s.db.Exec(`INSERT OR IGNORE INTO courseflow_notice VALUES(?,?)`, key, now)
	return newRow(result, err)
}

func newRow(result sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("notice persistence: %w", err)
	}
	return count == 1, nil
}
