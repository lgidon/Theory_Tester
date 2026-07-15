package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"log"
	"net/http"
	"time"
	// "strings"
)

// AnswerPayload captures user response checks from the frontend
type AnswerPayload struct {
	ID             string `json:"question_id"`
	SelectedAnswer int    `json:"selected_answer"`
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

		infoPrintf(
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
	httpServer  *http.Server
}

func NewWebServer(db *DBClient, licenseType string) *WebServer {
	return &WebServer{
		DB:          db,
		LicenseType: licenseType,
	}
}

// func (ws *WebServer) Start(port string) error {
func (ws *WebServer) SetupRoutes(port string) http.Handler {
	mux := http.NewServeMux()

	// 1. API Routes
	mux.HandleFunc("/api/question", ws.handleGetQuestion)
	mux.HandleFunc("/api/question/answer", ws.handlePostAnswer)
	mux.HandleFunc("/api/question/mark-known", ws.handlePostMarkKnown)
	mux.HandleFunc("/api/session/mode", ws.handlePostSessionMode)
	mux.HandleFunc("/api/status", ws.handleGetStatus)
	mux.HandleFunc("/api/categories", ws.handleGetCategories)
	mux.HandleFunc("/api/categories/select", ws.handleSelectCategory)
	mux.HandleFunc("/api/categories/reload", ws.handleReloadCategory)
	// mux.Handle("/images/", http.StripPrefix("/images/", http.FileServer(http.Dir("./data/images"))))

	// 2. Dynamic Image Route (Serves physical file stream from data/images)
	imagesDir := "./data/images"
	mux.Handle("/data/images/", http.StripPrefix("/data/images/", http.FileServer(http.Dir(imagesDir))))

	// 3. Embedded Static Frontend Route
	distFolder, err := fs.Sub(embeddedFrontend, "frontend/dist")
	if err != nil {
		log.Printf("Warning: Failed to load embedded frontend: %v", err)
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

	// loggedHandler := LoggingMiddleware(mux)

	return mux
}

func (ws *WebServer) Start(addr string) error {
	router := ws.SetupRoutes(addr)
	loggedHandler := LoggingMiddleware(router)

	// Create and store the http.Server instance
	ws.httpServer = &http.Server{
		Addr:    addr,
		Handler: loggedHandler,
	}

	// Use the stored server to listen and serve
	infoPrintf("DEBUG: Server is attempting to bind to address: %q", addr)

	err := ws.httpServer.ListenAndServe()
	// When Shutdown is called, ListenAndServe returns http.ErrServerClosed.
	// We want to return nil (no error) in this case so our main routine exits cleanly.
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Shutdown gracefully stops the server without interrupting active connections
func (ws *WebServer) Shutdown(ctx context.Context) error {
	if ws.httpServer == nil {
		return nil
	}
	return ws.httpServer.Shutdown(ctx)
}

func (ws *WebServer) handleGetQuestion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dbPath := "./data/theory.db"
	infoPrintf("DEBUG: Received GET /api/question request with query: %s", r.URL.RawQuery)
	category := r.URL.Query().Get("category") // empty if not provided

	if category == "" {
		category = "c1"
	}

	ws.LicenseType = category

	needsScrape, err := ws.DB.IsDatabaseEmpty(ws.LicenseType)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to verify database state: %v\n", err)
		ws.DB.Conn.Close()
		os.Exit(1)
	}

	if needsScrape {
		fmt.Printf("Preparing your database for %s (this only happens once)... ", ws.LicenseType)
		if err := Run(category, dbPath); err != nil {
			log.Fatalf("Scraper exited with error: %v", err)
		fmt.Println("Done! 🎉")
		}
	}

	userId := "default_user"
	var mode string
	var currentIndex int

	// Read active mode context
	// err := ws.DB.Conn.QueryRow("SELECT session_mode, current_question_index FROM user_sessions WHERE user_id = ?", userId).Scan(&mode, &currentIndex)
	// if err != nil {
	// 	mode = "all" // Fail open gracefully to global run loop
	// }

	var q *Question
	infoPrintf("Mode = %s", category)
	if mode == "simulator" || mode == "mock" {
		if currentIndex >= 30 {
			JSONError(w, "Exam complete", http.StatusNotFound)
			return
		}

		// Fetch the explicit question tied to the sequence index position
		query := `
			SELECT q.id, q.text, q.image_url, q.ans1, q.ans2, q.ans3, q.ans4, q.correct_ans 
			FROM questions q 
			JOIN mock_exam_questions m ON q.id = m.question_id 
			WHERE m.user_id = ? AND m.sort_order = ? and q.license_type = ? COLLATE NOCASE`
		infoPrintf("DEBUG: Fetching mock exam question for user %s at index %d in category %s", userId, currentIndex, category)
		infoPrintf("DEBUG: Executing query: %s", query)
		q = &Question{}
		err = ws.DB.Conn.QueryRow(query, userId, currentIndex, category).Scan(
			&q.ID, &q.Text, &q.ImageURL, &q.Ans1, &q.Ans2, &q.Ans3, &q.Ans4, &q.CorrectAns,
		)
		if err != nil {
			JSONError(w, "Exam question track exhausted", http.StatusNotFound)
			return
		}

		// Progress index pointer to the next position setup
		ws.DB.Conn.Exec("UPDATE user_sessions SET current_question_index = current_question_index + 1 WHERE user_id = ?", userId)

	} else {
		// Default Mode: Run through random questions like before
		q, err = ws.DB.GetNextRandomQuestion(category)
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
	infoPrintf("🔌 RAW FRONTEND INPUT JSON: %s", string(bodyBytes))

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
		infoPrintf("❌ Failed to look up question truth: %v", err)
		JSONError(w, "Question not found", http.StatusNotFound)
		return
	}
	isCorrect := (payload.SelectedAnswer == correctIndex)
	infoPrintf("📥 ID: %s, Selected Answer: %d. Correct answer %d", payload.ID, payload.SelectedAnswer, correctIndex)

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

	if mode == "simulator"  {
		infoPrintf("Mock license = %s", ws.LicenseType)
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

// handleGetCategories returns a map of categories and whether they have data
func (ws *WebServer) handleGetCategories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	categories := []string{"B", "C1", "C", "D"}
	response := make(map[string]interface{})
	list := make([]map[string]interface{}, 0)

	for _, cat := range categories {
		isEmpty, err := ws.DB.IsDatabaseEmpty(cat)
		hasData := false
		if err == nil && !isEmpty {
			hasData = true
		}
		list = append(list, map[string]interface{}{
			"id":       cat,
			"hydrated": hasData,
			"active":   cat == ws.LicenseType,
		})
	}

	response["categories"] = list
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleSelectCategory changes the active memory state of the server
func (ws *WebServer) handleSelectCategory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		Category string `json:"category"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Trigger lazy hydration if the user picks a clean category
	isEmpty, err := ws.DB.IsDatabaseEmpty(payload.Category)
	if err == nil && isEmpty {
		fmt.Printf("Category %s requested but empty. Hydrating on demand...\n", payload.Category)
		// Run your crawling logic inside a helper:
		// go ws.hydrateCategoryOnDemand(payload.Category)
	}

	ws.LicenseType = payload.Category
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"success","active":"%s"}`, ws.LicenseType)
}

// handleReloadCategory safely clears and re-scrapes a distinct category
func (ws *WebServer) handleReloadCategory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		Category string `json:"category"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// 1. Clear existing items from DB for this category
	// Assuming your db.go exposes an explicit ClearCategory runtime method:
	// ws.DB.ClearCategory(payload.Category)

	// 2. Trigger your crawlIndexPage & parseQuestionPage routine right here synchronously
	// or via a channel status notifier.

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"reloaded","category":"%s"}`, payload.Category)
}

type LicenseRequest struct {
	License string `json:"license"`
}

