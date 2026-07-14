package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"time"
)

// AnswerPayload captures user response checks from the frontend
type AnswerPayload struct {
	ID      string `json:"question_id"`
	SelectedAnswer int `json:"selected_answer"`
}

// MarkKnownPayload captures requests to filter a question out
type MarkKnownPayload struct {
	ID string `json:"id"`
}

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

// Write intercepts the response body if an error code is present
func (lrw *loggingResponseWriter) Write(b []byte) (int, error) {
	if lrw.statusCode >= 400 {
		log.Printf("⚠️ ERROR RESPONSE BODY: %s", string(b))
	}
	return lrw.ResponseWriter.Write(b)
}

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		lrw := &loggingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(lrw, r)

		log.Printf(
			"%-6s %s %d %s",
			r.Method,
			r.URL.Path,
			lrw.statusCode,
			time.Since(start),
		)
	})
}

// JSONError helper to return API errors consistently
func JSONError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

//go:embed frontend/dist
var embeddedFrontend embed.FS

type WebServer struct {
	DB          *DBClient
	LicenseType string
}

func NewWebServer(db *DBClient, licenseType string) *WebServer {
	return &WebServer{DB: db, LicenseType: licenseType}
}

func (ws *WebServer) Start(port string) error {
	mux := http.NewServeMux()

	// 1. API Routes
	mux.HandleFunc("/api/question", ws.handleGetQuestion)
	mux.HandleFunc("/api/question/answer", ws.handlePostAnswer)
	mux.HandleFunc("/api/question/mark-known", ws.handlePostMarkKnown)
	mux.HandleFunc("/api/session/mode", ws.handlePostSessionMode)
	mux.HandleFunc("/api/status", ws.handleGetStatus)
	// mux.Handle("/images/", http.StripPrefix("/images/", http.FileServer(http.Dir("./data/images"))))

	// 2. Dynamic Image Route (Serves physical file stream from data/images)
	imagesDir := "./data/images"
	mux.Handle("/data/images/", http.StripPrefix("/data/images/", http.FileServer(http.Dir(imagesDir))))

	// 3. Embedded Static Frontend Route
	distFolder, err := fs.Sub(embeddedFrontend, "frontend/dist")
	if err != nil {
		return err
	}

	fileServer := http.FileServer(http.FS(distFolder))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			// Now that the prefix is cleanly stripped, index.html is accessible at the root
			indexData, err := distFolder.Open("index.html")
			if err != nil {
				http.Error(w, "Index not found inside embedded FS", http.StatusNotFound)
				return
			}
			defer indexData.Close()

			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.Copy(w, indexData)
			return
		}

		fileServer.ServeHTTP(w, r)
	})

	loggedHandler := LoggingMiddleware(mux)

	return http.ListenAndServe(":"+port, loggedHandler)
}

func (ws *WebServer) handleGetQuestion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userId := "default_user"
	var mode string
	var currentIndex int

	// Read active mode context
	err := ws.DB.Conn.QueryRow("SELECT session_mode, current_question_index FROM user_sessions WHERE user_id = ?", userId).Scan(&mode, &currentIndex)
	if err != nil {
		mode = "all" // Fail open gracefully to global run loop
	}

	var q *Question

	if mode == "simulator" {
		if currentIndex >= 30 {
			JSONError(w, "Exam complete", http.StatusNotFound)
			return
		}

		// Fetch the explicit question tied to the sequence index position
		query := `
			SELECT q.id, q.text, q.image_url, q.ans1, q.ans2, q.ans3, q.ans4, q.correct_ans 
			FROM questions q 
			JOIN mock_exam_questions m ON q.id = m.question_id 
			WHERE m.user_id = ? AND m.sort_order = ?`

		q = &Question{}
		err = ws.DB.Conn.QueryRow(query, userId, currentIndex).Scan(
			&q.ID, &q.Text, &q.ImageURL, &q.Answers[0], &q.Answers[1], &q.Answers[2], &q.Answers[3], &q.CorrectAnswer,
		)
		if err != nil {
			JSONError(w, "Exam question track exhausted", http.StatusNotFound)
			return
		}

		// Progress index pointer to the next position setup
		ws.DB.Conn.Exec("UPDATE user_sessions SET current_question_index = current_question_index + 1 WHERE user_id = ?", userId)

	} else {
		// Default Mode: Run through random questions like before
		q, err = ws.DB.GetNextRandomQuestion(ws.LicenseType)
		if err != nil {
			JSONError(w, "Database failure", http.StatusInternalServerError)
			return
		}
	}

	if q == nil {
		JSONError(w, "No remaining questions", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(q)
}

func (ws *WebServer) handlePostAnswer(w http.ResponseWriter, r *http.Request) {
	bodyBytes, _ := io.ReadAll(r.Body)

	// 2. Print the raw string input to your terminal
	log.Printf("🔌 RAW FRONTEND INPUT JSON: %s", string(bodyBytes))

	// 3. IMPORTANT: Restore the body so the JSON decoder can read it next
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	if r.Method != http.MethodPost {
		JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload AnswerPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		JSONError(w, "Bad request body", http.StatusBadRequest)
		return
	}

	correctIndex, err := ws.DB.GetCorrectAnswerByID(payload.ID)
	if err != nil {
		log.Printf("❌ Failed to look up question truth: %v", err)
		JSONError(w, "Question not found", http.StatusNotFound)
		return
	}
	isCorrect := (payload.SelectedAnswer == correctIndex)
	log.Printf("📥 ID: %s, Selected Answer: %d. Correct answer %d", payload.ID, payload.SelectedAnswer, correctIndex)

	if err := ws.DB.RecordAnswerResult(payload.ID, isCorrect); err != nil {
		JSONError(w, "Failed to update progress", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (ws *WebServer) handlePostMarkKnown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload MarkKnownPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		JSONError(w, "Bad request body", http.StatusBadRequest)
		return
	}

	if err := ws.DB.MarkAsKnown(payload.ID); err != nil {
		JSONError(w, "Failed to flag question state", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (ws *WebServer) handlePostSessionMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload map[string]string
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		JSONError(w, "Bad request", http.StatusBadRequest)
		return
	}

	mode := payload["mode"]  // "all" or "mock"
	userId := "default_user" // Hardcoded for now, can easily be extracted from headers/auth cookies later!

	if mode == "simulator" {
		if err := ws.DB.CreateMockExam(userId, ws.LicenseType); err != nil {
			JSONError(w, "Failed to initialize mock exam track", http.StatusInternalServerError)
			return
		}
	} else {
		if err := ws.DB.SetStudyAllMode(userId); err != nil {
			JSONError(w, "Failed to update session state", http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "mode successfully changed", "mode": mode})
}

func (ws *WebServer) handleGetStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status, err := ws.DB.GetProgressStatus(ws.LicenseType)
	if err != nil {
		println("Database Status Error:", err.Error())
		JSONError(w, "Metrics resolution error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}