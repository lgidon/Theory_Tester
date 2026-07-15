package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"path/filepath"
	"io"
	"os"
	

	"github.com/PuerkitoBio/goquery"
	_ "modernc.org/sqlite"
)

// Question matches your SQLite schema structure exactly
type Question struct {
	ID          string
	Text        string
	ImageURL    string
	Ans1        string
	Ans2        string
	Ans3        string
	Ans4        string
	CorrectAns  int
	LicenseType string
	IsKnown     int
}

// Run executes the scraping process for a specific category.
// It detects the question limit, scrapes each, and records to the database.
func Run(category string, dbPath string) error {
	category = strings.ToLower(category)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	if err := initTable(db); err != nil {
		return fmt.Errorf("failed to initialize table: %w", err)
	}

	fmt.Printf("Determining maximum questions for category '%s'...\n", category)
	maxQuestions, err := getMaxQuestions(category)
	if err != nil {
		return fmt.Errorf("failed to determine total questions: %w", err)
	}
	fmt.Printf("Found %d questions to scrape for category %s.\n", maxQuestions, category)

	for i := 1; i <= maxQuestions; i++ {
	// for i := 1; i <= 5; i++ {
		url := fmt.Sprintf("https://teo.co.il/questions/%s/%d", category, i)
		fmt.Printf("[%d/%d] Scraping %s...\n", i, maxQuestions, url)

		q, err := scrapeQuestionPage(url, category, i)
		if err != nil {
			log.Printf("Warning: Failed to scrape page %d: %v. Skipping.", i, err)
			continue
		}

		if err := insertQuestion(db, q); err != nil {
			log.Printf("Warning: Database insert failed for %s: %v", q.ID, err)
		}
	}

	fmt.Println("Scraping completed successfully!")
	return nil
}

// getMaxQuestions looks at the parent page to find the total quantity of questions (X)
func getMaxQuestions(category string) (int, error) {
	url := fmt.Sprintf("https://teo.co.il/questions/%s", category)
	infoPrintf("DEBUG: Fetching parent page to determine max questions: %s", url)
	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("bad status code on parent page: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return 0, err
	}

	maxNum := 0
	
	// Create a regex to pull the numeric question ID from the end of the href path
	// Handles absolute URLs: "https://teo.co.il/questions/c1/1" 
	// And relative paths: "/questions/c1/1"
	re := regexp.MustCompile(fmt.Sprintf(`/questions/%s/(\d+)`, regexp.QuoteMeta(category)))

	// Target the specific list structure: ID "main-self", section, ordered list (ol), list item (li), anchor (a)
	doc.Find("#main-self section ol li a").Each(func(i int, s *goquery.Selection) {
		if href, exists := s.Attr("href"); exists {
			if matches := re.FindStringSubmatch(href); len(matches) == 2 {
				num, err := strconv.Atoi(matches[1])
				if err == nil && num > maxNum {
					maxNum = num
				}
			}
		}
	})

	if maxNum == 0 {
		return 0, fmt.Errorf("could not detect any question page counts on %s using selector '#main-self section ol li a'", url)
	}

	return maxNum, nil
}

// scrapeQuestionPage fetches and extracts details of a single question
func scrapeQuestionPage(url string, category string, index int) (*Question, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("non-200 status code: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	q := &Question{
		ID:          fmt.Sprintf("%s_%d", category, index),
		LicenseType: category,
		IsKnown:     0,
	}

	// 1. Get Question Text (targets headers or question blocks containing content)
	q.Text = strings.TrimSpace(doc.Find(".question-self").First().Text())

// 2. Locate and Download the Image (if it exists)
	var remoteImgURL string
	
	// Scan the main container for question images, ignoring navigation/avatar elements
	doc.Find("#main-self img").Each(func(i int, s *goquery.Selection) {
		if src, exists := s.Attr("src"); exists {
			// Filter to capture actual question diagrams/illustrations
			if strings.Contains(src, "/uploads/") || strings.Contains(src, "/questions/") {
				remoteImgURL = src
			}
		}
	})

	if remoteImgURL != "" {
		// Resolve relative paths to absolute URLs
		if strings.HasPrefix(remoteImgURL, "/") && !strings.HasPrefix(remoteImgURL, "//") {
			remoteImgURL = "https://teo.co.il" + remoteImgURL
		}

		// Detect file extension (defaulting to .png if none found)
		ext := filepath.Ext(remoteImgURL)
		if ext == "" || len(ext) > 5 {
			ext = ".png"
		}

		// Create local file name: e.g., "c1_1.png"
		localFileName := fmt.Sprintf("%s_%d%s", category, index, ext)
		localFolder := "./data/images"



		// Download and save the image
		err := downloadFile(remoteImgURL, filepath.Join(localFolder, localFileName))
		if err != nil {
			log.Printf("Warning: Failed to download image %s: %v", remoteImgURL, err)
		} else {
			// Record the file name to the DB struct
			q.ImageURL = localFileName
		}

	}

	// 3. Extract Answers & Verify Core Correct Answer ID
	answers := make([]string, 0, 4)
	var correctAnsIdx int

	doc.Find("input[type='radio']").Each(func(i int, s *goquery.Selection) {
		if len(answers) >= 4 {
			return // Strictly capture standard 4-option questions
		}

		// Look for standard data-correct properties
		if isCorrect, _ := s.Attr("data-correct"); isCorrect == "1" {
			correctAnsIdx = len(answers) + 1 // 1-based index (1-4)
		}

		var labelText string
		id, _ := s.Attr("id")

		if id != "" {
			labelText = strings.TrimSpace(doc.Find(fmt.Sprintf("label[for='%s']", id)).Text())
		} else {
			labelText = strings.TrimSpace(s.NextFiltered("label").Text())
		}

		if labelText != "" {
			answers = append(answers, labelText)
		}
	})

	// Safely map extracted answers to exact string properties
	if len(answers) > 0 { q.Ans1 = answers[0] }
	if len(answers) > 1 { q.Ans2 = answers[1] }
	if len(answers) > 2 { q.Ans3 = answers[2] }
	if len(answers) > 3 { q.Ans4 = answers[3] }

	q.CorrectAns = correctAnsIdx

	// Basic validation check
	if q.Text == "" || q.Ans1 == "" {
		return nil, fmt.Errorf("parsed question details are incomplete")
	}

	return q, nil
}

func initTable(db *sql.DB) error {
	query := `
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
	);`
	_, err := db.Exec(query)
	return err
}

func insertQuestion(db *sql.DB, q *Question) error {
	query := `
	INSERT INTO questions (id, text, image_url, ans1, ans2, ans3, ans4, correct_ans, license_type, is_known)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		text = excluded.text,
		image_url = excluded.image_url,
		ans1 = excluded.ans1,
		ans2 = excluded.ans2,
		ans3 = excluded.ans3,
		ans4 = excluded.ans4,
		correct_ans = excluded.correct_ans,
		license_type = excluded.license_type;`

	_, err := db.Exec(query,
		q.ID,
		q.Text,
		sql.NullString{String: q.ImageURL, Valid: q.ImageURL != ""},
		q.Ans1,
		q.Ans2,
		q.Ans3,
		q.Ans4,
		q.CorrectAns,
		q.LicenseType,
		q.IsKnown,
	)
	return err
}

// downloadFile streams a remote URL file directly to a local file path
func downloadFile(url, destPath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad response status: %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}