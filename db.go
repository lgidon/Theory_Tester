package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
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

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	client := &DBClient{Conn: db}
	if err := client.createTables(); err != nil {
		db.Close()
		return nil, err
	}

	return client, nil
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
		INSERT OR IGNORE INTO questions (id, question_text, local_image_path, ans_1, ans_2, ans_3, ans_4, correct_answer)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		q.ID, q.Text, q.ImageURL, q.Answers[0], q.Answers[1], q.Answers[2], q.Answers[3], q.CorrectAnswer) // We reuse q.ImageURL to temporarily pass the local path
	if err != nil {
		return fmt.Errorf("failed to insert question %s: %w", q.ID, err)
	}

	// Link to this license type
	_, err = tx.Exec(`
		INSERT OR IGNORE INTO question_licenses (question_id, license_type)
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
		SELECT COUNT(*) FROM question_licenses WHERE license_type = ?`, licenseType).Scan(&count)
	return count == 0, err
}

// GetNextRandomQuestion pulls a random question that has not been successfully marked as known
func (db *DBClient) GetNextRandomQuestion(licenseType string) (*Question, error) {
	query := `
		SELECT q.id, q.question_text, q.local_image_path, q.ans_1, q.ans_2, q.ans_3, q.ans_4, q.correct_answer
		FROM questions q
		JOIN question_licenses ql ON q.id = ql.question_id
		LEFT JOIN user_progress up ON q.id = up.question_id
		WHERE ql.license_type = ? AND (up.is_known IS NULL OR up.is_known = 0)
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
	if isCorrect {
		points = 0
	} else {
		points = 1
	}

	query := `
		INSERT INTO user_progress (question_id, is_known, times_wrong, last_answered)
		VALUES (?, 0, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(question_id) DO UPDATE SET
			times_wrong = times_wrong + ?,
			last_answered = CURRENT_TIMESTAMP;`

	_, err := db.Conn.Exec(query, questionID, points, points)
	return err
}

// MarkAsKnown cleanly drops the question out of future study rotations
func (db *DBClient) MarkAsKnown(questionID string) error {
	query := `
		INSERT INTO user_progress (question_id, is_known, times_wrong, last_answered)
		VALUES (?, 1, 0, CURRENT_TIMESTAMP)
		ON CONFLICT(question_id) DO UPDATE SET
			is_known = 1,
			last_answered = CURRENT_TIMESTAMP;`

	_, err := db.Conn.Exec(query, questionID)
	return err
}