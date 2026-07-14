package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"log"
	_ "modernc.org/sqlite"
)

type DBClient struct {
	Conn *sql.DB
}

// InitDB ensures the storage directory exists and initializes tables
func InitDB(dbPath string) (*DBClient, error) {
	// Create the directory path if it doesn't exist (crucial for Docker volumes)
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	schema := `

	CREATE TABLE IF NOT EXISTS licenses (
		code TEXT PRIMARY KEY,       -- e.g., 'B', 'A', 'C1'
		name_he TEXT NOT NULL,       -- e.g., 'רכב פרטי', 'אופנוע'
		total_exam_questions INTEGER DEFAULT 30
	);

	CREATE TABLE IF NOT EXISTS questions (
		id TEXT PRIMARY KEY,
		text TEXT NOT NULL,
		image_url TEXT,
		ans1 TEXT NOT NULL,
		ans2 TEXT NOT NULL,
		ans3 TEXT NOT NULL,
		ans4 TEXT NOT NULL,
		correct_ans INTEGER NOT NULL,
		license_type TEXT NOT NULL,
		is_known INTEGER DEFAULT 0
	);

	-- Junction table to link questions to their allowed license tracks
	CREATE TABLE IF NOT EXISTS question_licenses (
		question_id TEXT,
		license_code TEXT,
		PRIMARY KEY (question_id, license_code),
		FOREIGN KEY(question_id) REFERENCES questions(id),
		FOREIGN KEY(license_code) REFERENCES licenses(code)
	);

	CREATE TABLE IF NOT EXISTS history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		question_id TEXT NOT NULL,
		is_correct INTEGER NOT NULL,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(question_id) REFERENCES questions(id)
	);

	-- New Tables for Stage 2 Session Adjustments
	CREATE TABLE IF NOT EXISTS user_sessions (
		user_id TEXT DEFAULT 'default_user',
		session_mode TEXT NOT NULL,          -- 'all' or 'mock'
		current_question_index INTEGER DEFAULT 0,
		exam_started_at DATETIME,
		PRIMARY KEY (user_id)
	);

	CREATE TABLE IF NOT EXISTS mock_exam_questions (
		user_id TEXT DEFAULT 'default_user',
		question_id TEXT NOT NULL,
		sort_order INTEGER NOT NULL,
		PRIMARY KEY (user_id, question_id)
	);`

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}

	// Seed core categories automatically
	seedLicenses := `
	INSERT INTO licenses (code, name_he) VALUES 
		('B', 'רכב פרטי'),
		('C1', 'רכב כבד'),
		('A', 'אופנוע'),
		('C', 'רכב מסחרי כבד'),
		('D', 'רכב ציבורי'),
		('E', 'רכב נגרר')
	ON CONFLICT(code) DO NOTHING;`
	
	if _, err := db.Exec(seedLicenses); err != nil {
		return nil, err
	}

	return &DBClient{Conn: db}, nil
}

func (db *DBClient) createTables() error {
	// 1. Core Question Bank
	queries := []string{
		`CREATE TABLE IF NOT EXISTS questions (
			id TEXT PRIMARY KEY,
			question_text TEXT NOT NULL,
			local_image_path TEXT,
			ans_1 TEXT NOT NULL,
			ans_2 TEXT NOT NULL,
			ans_3 TEXT NOT NULL,
			ans_4 TEXT NOT NULL,
			correct_answer INTEGER NOT NULL
		);`,
		// 2. Junction table linking questions to license categories (e.g., C1)
		`CREATE TABLE IF NOT EXISTS question_licenses (
			question_id TEXT,
			license_type TEXT,
			PRIMARY KEY (question_id, license_type),
			FOREIGN KEY(question_id) REFERENCES questions(id) ON DELETE CASCADE
		);`,
		// 3. User study progress tracker
		`CREATE TABLE IF NOT EXISTS user_progress (
			question_id TEXT PRIMARY KEY,
			is_known INTEGER DEFAULT 0,
			times_wrong INTEGER DEFAULT 0,
			last_answered DATETIME,
			FOREIGN KEY(question_id) REFERENCES questions(id) ON DELETE CASCADE
		);`,
	}

	for _, q := range queries {
		if _, err := db.Conn.Exec(q); err != nil {
			return fmt.Errorf("failed creating schema: %w", err)
		}
	}
	return nil
}

// SaveQuestion atomic transaction to insert both core details and category tags
func (db *DBClient) SaveQuestion(q Question, licenseType string) error {
	tx, err := db.Conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Insert question details (Skip/Ignore if already scraped in another pass)
	// Inside db.go -> SaveQuestion()
	_, err = tx.Exec(`
		INSERT INTO questions (id, text, image_url, ans1, ans2, ans3, ans4, correct_ans, license_type)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		q.ID, q.Text, q.ImageURL, q.Answers[0], q.Answers[1], q.Answers[2], q.Answers[3], q.CorrectAnswer, licenseType) // We reuse q.ImageURL to temporarily pass the local path
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to insert question %s: %w", q.ID, err)
	}

	// Link to this license type
	_, err = tx.Exec(`
		INSERT OR IGNORE INTO question_licenses (question_id, license_code)
		VALUES (?, ?)`,
		q.ID, licenseType)
	if err != nil {
		return fmt.Errorf("failed to link license for %s: %w", q.ID, err)
	}

	return tx.Commit()
}

// IsDatabaseEmpty checks if we actually need to run the scraper
func (db *DBClient) IsDatabaseEmpty(licenseType string) (bool, error) {
	var count int
	err := db.Conn.QueryRow(`
		SELECT COUNT(*) FROM questions WHERE license_type = ?`, licenseType).Scan(&count)
	return count == 0, err
}

// GetNextRandomQuestion pulls a random question that has not been successfully marked as known
func (db *DBClient) GetNextRandomQuestion(licenseType string) (*Question, error) {
	query := `
		SELECT q.id, q.text, q.image_url, q.ans1, q.ans2, q.ans3, q.ans4, q.correct_ans 
		FROM questions q
		WHERE license_type = ?  COLLATE NOCASE
		ORDER BY RANDOM()
		LIMIT 1;`

	var q Question
	var imgPath sql.NullString

	err := db.Conn.QueryRow(query, licenseType).Scan(
		&q.ID, &q.Text, &imgPath, &q.Answers[0], &q.Answers[1], &q.Answers[2], &q.Answers[3], &q.CorrectAnswer,
	)
	if err == sql.ErrNoRows {
		return nil, nil // All questions cleared!
	}
	if err != nil {
		return nil, err
	}

	if imgPath.Valid {
		q.ImageURL = imgPath.String // Reuse field to hold local filename for frontend rendering
	}

	return &q, nil
}

// RecordAnswerResult updates the user study metrics based on correctness
func (db *DBClient) RecordAnswerResult(questionID string, isCorrect bool) error {
	var points int
	log.Printf("📥 Is correct: %t", isCorrect)
	if isCorrect {
		points = 1
	} else {
		points = 0
	}

	log.Printf("📥 Received Answer Submission - ID: '%s', Answer: %d", questionID, points)

	query := `
    INSERT INTO history (question_id, is_correct, timestamp)
    VALUES (?, ?, CURRENT_TIMESTAMP)`

	_, err := db.Conn.Exec(query, questionID, points)
	return err
}

// MarkAsKnown cleanly drops the question out of future study rotations
func (db *DBClient) MarkAsKnown(questionID string) error {
	query := `
		INSERT INTO user_progress (question_id, is_known, last_answered)
		VALUES (?, 1, 0, CURRENT_TIMESTAMP)
		ON CONFLICT(question_id) DO UPDATE SET
			is_known = 1,
			last_answered = CURRENT_TIMESTAMP;`

	_, err := db.Conn.Exec(query, questionID)
	return err
}

func (db *DBClient) CreateMockExam(userId string, licenseType string) error {
	// 1. Clear previous mock exam context
	_, err := db.Conn.Exec("DELETE FROM mock_exam_questions WHERE user_id = ?", userId)
	if err != nil {
		return err
	}

	// 2. Fetch 30 random un-mastered questions from the license group
	query := `
		SELECT id FROM questions 
		WHERE license_type = ?  COLLATE NOCASE AND is_known = 0 
		ORDER BY RANDOM() LIMIT 30`

	rows, err := db.Conn.Query(query, licenseType)
	if err != nil {
		return err
	}
	defer rows.Close()

	var qIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			qIDs = append(qIDs, id)
		}
	}

	// 3. Save the specific question track sequence
	tx, err := db.Conn.Begin()
	if err != nil {
		return err
	}
	for idx, qID := range qIDs {
		_, err = tx.Exec(`
			INSERT INTO mock_exam_questions (user_id, question_id, sort_order) 
			VALUES (?, ?, ?)`, userId, qID, idx)
		if err != nil {
			tx.Rollback()
			return err
		}
	}

	// 4. Update the active mode state
	_, err = tx.Exec(`
		INSERT INTO user_sessions (user_id, session_mode, current_question_index, exam_started_at) 
		VALUES (?, 'mock', 0, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id) DO UPDATE SET 
			session_mode='mock', current_question_index=0, exam_started_at=CURRENT_TIMESTAMP`, userId)
	if err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

// Set global mode state to study all questions
func (db *DBClient) SetStudyAllMode(userId string) error {
	_, err := db.Conn.Exec(`
		INSERT INTO user_sessions (user_id, session_mode, current_question_index) 
		VALUES (?, 'all', 0)
		ON CONFLICT(user_id) DO UPDATE SET session_mode='all'`, userId)
	return err
}

// Fetch metrics summary data
type ProgressStatus struct {
	TotalQuestions int `json:"total_questions"`
	MasteredCount  int `json:"mastered_count"`
	CorrectAnswers int `json:"correct_answers"`
	WrongAnswers   int `json:"wrong_answers"`
}

func (db *DBClient) GetProgressStatus(licenseType string) (ProgressStatus, error) {
	var status ProgressStatus

	// Total questions count
	err := db.Conn.QueryRow("SELECT COUNT(*) FROM questions WHERE license_type = ?", licenseType).Scan(&status.TotalQuestions)
	if err != nil {
		return status, err
	}

	// Mastered flags count
	err = db.Conn.QueryRow("SELECT COUNT(*) FROM questions WHERE license_type = ? AND is_known = 1", licenseType).Scan(&status.MasteredCount)
	if err != nil {
		return status, err
	}

	// History counts
	err = db.Conn.QueryRow("SELECT COUNT(*) FROM history WHERE is_correct = 1").Scan(&status.CorrectAnswers)
	if err != nil {
		return status, err
	}
	err = db.Conn.QueryRow("SELECT COUNT(*) FROM history WHERE is_correct = 0").Scan(&status.WrongAnswers)

	return status, err
}

func (db *DBClient) GetCorrectAnswerByID(id string) (int, error) {
	var correctAns int
	query := `SELECT correct_ans FROM questions WHERE id = ? LIMIT 1`
	
	err := db.Conn.QueryRow(query, id).Scan(&correctAns)
	if err != nil {
		return 0, err
	}
	return correctAns, nil
}