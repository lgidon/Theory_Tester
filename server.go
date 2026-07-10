package main

import (
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
)

// AnswerPayload captures user response checks from the frontend
type AnswerPayload struct {
	ID      string `json:"id"`
	Correct bool   `json:"correct"`
}

// MarkKnownPayload captures requests to filter a question out
type MarkKnownPayload struct {
	ID string `json:"id"`
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

	// 2. Dynamic Image Route (Serves physical file stream from data/images)
	imagesDir := "./data/images"
	mux.Handle("/images/", http.StripPrefix("/images/", http.FileServer(http.Dir(imagesDir))))

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

	return http.ListenAndServe(":"+port, mux)
}

func (ws *WebServer) handleGetQuestion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	q, err := ws.DB.GetNextRandomQuestion(ws.LicenseType)
	if err != nil {
		JSONError(w, "Database failure", http.StatusInternalServerError)
		return
	}

	if q == nil {
		JSONError(w, "No remaining questions", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(q)
}

func (ws *WebServer) handlePostAnswer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload AnswerPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		JSONError(w, "Bad request body", http.StatusBadRequest)
		return
	}

	if err := ws.DB.RecordAnswerResult(payload.ID, payload.Correct); err != nil {
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
