package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestJSONError(t *testing.T) {
	rr := httptest.NewRecorder()
	JSONError(rr, "bad thing", http.StatusBadRequest)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d got %d", http.StatusBadRequest, rr.Code)
	}

	var out map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}
	if out["error"] != "bad thing" {
		t.Fatalf("unexpected error message: %v", out["error"])
	}
}

func TestLoggingMiddleware_preservesResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		w.Write([]byte("hello"))
	})

	wrapped := LoggingMiddleware(handler)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusTeapot {
		t.Fatalf("expected status %d got %d", http.StatusTeapot, rr.Code)
	}
	if rr.Body.String() != "hello" {
		t.Fatalf("unexpected body: %s", rr.Body.String())
	}
}

// helper to create a temporary DB initialized via InitDB
func makeTestDB(t *testing.T) (*DBClient, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	cleanup := func() {
		if db != nil && db.Conn != nil {
			db.Conn.Close()
		}
		os.Remove(dbPath)
	}
	return db, cleanup
}

func TestHandleGetQuestion_and_PostAnswer(t *testing.T) {
	db, cleanup := makeTestDB(t)
	defer cleanup()

	// Insert a sample question that matches queries used by handlers
	_, err := db.Conn.Exec(`INSERT INTO questions (id, text, image_url, ans1, ans2, ans3, ans4, correct_ans, license_type) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"q1", "What?", "", "a1", "a2", "a3", "a4", 1, "C1")
	if err != nil {
		t.Fatalf("failed insert question: %v", err)
	}

	ws := NewWebServer(db, "C1")

	// Test GET /api/question
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/question", nil)
	ws.handleGetQuestion(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body:%s", rr.Code, rr.Body.String())
	}

	var q Question
	if err := json.NewDecoder(rr.Body).Decode(&q); err != nil {
		t.Fatalf("failed to decode question json: %v", err)
	}
	if q.ID != "q1" {
		t.Fatalf("unexpected question id: %s", q.ID)
	}

	// Test POST /api/question/answer with correct answer
	payload := map[string]interface{}{"question_id": "q1", "selected_answer": 1}
	b, _ := json.Marshal(payload)
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/question/answer", bytes.NewReader(b))
	ws.handlePostAnswer(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("expected 200 on correct answer got %d body:%s", rr2.Code, rr2.Body.String())
	}

	// Verify history row was added
	var cnt int
	err = db.Conn.QueryRow("SELECT COUNT(*) FROM history WHERE question_id = ? AND is_correct = 1", "q1").Scan(&cnt)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("query error: %v", err)
	}
	if cnt == 0 {
		t.Fatalf("expected history row for correct answer")
	}

	// Test POST /api/question/answer with incorrect answer
	payload2 := map[string]interface{}{"question_id": "q1", "selected_answer": 2}
	b2, _ := json.Marshal(payload2)
	rr3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/api/question/answer", bytes.NewReader(b2))
	ws.handlePostAnswer(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("expected 200 on incorrect answer got %d body:%s", rr3.Code, rr3.Body.String())
	}

	// Verify history row for incorrect
	var wrongCnt int
	err = db.Conn.QueryRow("SELECT COUNT(*) FROM history WHERE question_id = ? AND is_correct = 0", "q1").Scan(&wrongCnt)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("query error: %v", err)
	}
	if wrongCnt == 0 {
		t.Fatalf("expected history row for incorrect answer")
	}
}
