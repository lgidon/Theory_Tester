package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"os/exec"
	"runtime"
	"github.com/PuerkitoBio/goquery"
	"flag"
)

// Question represents our normalized internal data structure
type Question struct {
	ID            string
	Text          string
	ImageURL      string
	Answers       [4]string
	CorrectAnswer int // 1, 2, 3, or 4
}

const (
	BaseURL     = "https://teo.co.il"
	CategoryURL = BaseURL + "/questions/c1" // Temporary hardcoded fallback or initial entrypoint
)

func main() {
	dbPath := "./data/theory.db"
	licenseType := "C1"

	// 1. Define command-line flags
	port := flag.String("port", "8080", "Port to run the server on")
	localMode := flag.Bool("local", true, "Run locally (binds to localhost, auto-opens browser)")
	flag.Parse()

	// 2. Initialize DB Client
	db, err := InitDB(dbPath)
	if err != nil {
		log.Fatalf("Database initialization failed: %v", err)
	}
	defer db.Conn.Close()

	// 3. Check if first-run scrape is necessary
	needsScrape, err := db.IsDatabaseEmpty(licenseType)
	if err != nil {
		log.Fatalf("Failed to verify database state: %v", err)
	}

	// 3. Perform the scrape only if database is clean
	if needsScrape {
		fmt.Printf("Database empty for %s. Commencing index crawl...\n", licenseType)
		questionURLs, err := crawlIndexPage(CategoryURL)
		if err != nil {
			log.Fatalf("Error crawling index page: %v", err)
		}

		fmt.Printf("Found %d questions. Starting processing run...\n", len(questionURLs))
		count := 0
		for i, url := range questionURLs {
			if count >= 15 {
				break
			}
			count++
			fmt.Printf("[%d/%d] Scraping: %s\n", i+1, len(questionURLs), url)

			q, err := parseQuestionPage(url)
			if err != nil {
				fmt.Printf("⚠️ Error parsing %s: %v\n", url, err)
				continue
			}

			if q.ImageURL != "" {
				imagesDir := "./data/images"
				fmt.Printf("   💾 Downloading image for %s...\n", q.ID)

				localFilename, err := downloadImage(q.ImageURL, q.ID, imagesDir)
				if err != nil {
					fmt.Printf("   ⚠️ Failed to download image for %s: %v\n", q.ID, err)
					q.ImageURL = ""
				} else {
					q.ImageURL = localFilename
				}
			}

			if err := db.SaveQuestion(q, licenseType); err != nil {
				fmt.Printf("⚠️ Error saving question %s to DB: %v\n", q.ID, err)
				continue
			}

			time.Sleep(400 * time.Millisecond)
		}
		fmt.Println("\n🎉 Success! Question database hydrated completely.")
	} else {
		fmt.Printf("Database already contains questions for %s. Skipping scrape phase.\n", licenseType)
	}

	// 4. Determine host binding based on the execution mode
	host := "0.0.0.0" // Binds to all interfaces (headless webserver mode)
	if *localMode {
		host = "127.0.0.1" // Binds only to loopback (local client mode)
	}
	addr := fmt.Sprintf("%s:%s", host, *port)

	// 5. Launch Web Server (Reachable by both code execution branches)
	server := NewWebServer(db, licenseType)

	// 6. If running in local mode, open the browser asynchronously 
	if *localMode {
		go func() {
			url := fmt.Sprintf("http://localhost:%s", *port)
			fmt.Printf("🚀 Local mode active. Launching browser to %s\n", url)
			
			// Small delay to let the server startup and listen
			time.Sleep(200 * time.Millisecond)
			if err := openBrowser(url); err != nil {
				log.Printf("⚠️ Could not automatically open browser: %v", err)
				log.Printf("Please open your browser manually and navigate to: %s", url)
			}
		}()
	} else {
		fmt.Printf("🚀 Server mode active. Listening on http://%s\n", addr)
	}

	// 7. Start the server with the dynamic address
	if err := server.Start(addr); err != nil {
		log.Fatalf("Server shutdown unexpectedly: %v", err)
	}
}


func openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin": // macOS
		cmd = "open"
		args = []string{url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return exec.Command(cmd, args...).Start()
}

// crawlIndexPage parses the exact DOM structure provided in your screenshot
func crawlIndexPage(targetURL string) ([]string, error) {
	res, err := http.Get(targetURL)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("status code error: %d %s", res.StatusCode, res.Status)
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return nil, err
	}

	var urls []string
	// CSS selector directly targeting your DOM screenshot context
	doc.Find("#main-self ol li a").Each(func(i int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if exists {
			// Handle both absolute and relative URLs gracefully
			if !strings.HasPrefix(href, "http") {
				href = BaseURL + href
			}
			urls = append(urls, href)
		}
	})

	return urls, nil
}

// parseQuestionPage dives into a single question and extracts its details based on exact DOM attributes
func parseQuestionPage(pageURL string) (Question, error) {
	var q Question

	// Extract an ID from the trailing URL path (e.g., ".../questions/c1/3" -> "c1_3")
	parts := strings.Split(strings.TrimRight(pageURL, "/"), "/")
	if len(parts) >= 2 {
		q.ID = fmt.Sprintf("%s_%s", parts[len(parts)-2], parts[len(parts)-1])
	} else {
		q.ID = pageURL
	}

	res, err := http.Get(pageURL)
	if err != nil {
		return q, err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return q, fmt.Errorf("status code error: %d", res.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return q, err
	}

	// 1. Extract Question Text
	q.Text = strings.TrimSpace(doc.Find("#questions h3 span.question-self").First().Text())

	// 2. Extract Optional Image Layout
	// Look for an image asset inside the questions section block
	imgSrc, imgExists := doc.Find("#questions img").Attr("src")
	if imgExists {
		if !strings.HasPrefix(imgSrc, "http") {
			q.ImageURL = BaseURL + imgSrc
		} else {
			q.ImageURL = imgSrc
		}
	}

	// 3. Extract the 4 Answers and find the correct index based on data-correct="1"
	doc.Find("#questions ul li").Each(func(i int, s *goquery.Selection) {
		if i < 4 {
			// Extract answer text from the <label>
			answerText := strings.TrimSpace(s.Find("label").Text())
			q.Answers[i] = answerText

			// Check the companion <input> element for the correct flag attribute
			input := s.Find("input")
			if input.AttrOr("data-correct", "0") == "1" {
				q.CorrectAnswer = i + 1 // 1-indexed (1, 2, 3, or 4)
			}
		}
	})

	return q, nil
}

// downloadImage pulls the remote asset down and writes it to the local data directory
func downloadImage(url, questionID, outputDir string) (string, error) {
	// Ensure the directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", err
	}

	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download image, status: %s", resp.Status)
	}

	// Determine file extension (fallback to .png if none found)
	ext := filepath.Ext(url)
	if ext == "" {
		ext = ".png"
	}

	// Create a predictable filename using the question ID (e.g., c1_3.png)
	filename := questionID + ext
	finalPath := filepath.Join(outputDir, filename)

	out, err := os.Create(finalPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return "", err
	}

	return filename, nil
}
