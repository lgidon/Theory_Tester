package main

import (
	"fmt"
	// "io"
	// "net/http"
	// "os"
	"os/exec"
	// "path/filepath"
	"runtime"
	// "strings"
	// "github.com/PuerkitoBio/goquery"
)

// // Question represents our normalized internal data structure
// type Question struct {
// 	ID            string
// 	Text          string
// 	ImageURL      string
// 	Answers       [4]string
// 	CorrectAnswer int // 1, 2, 3, or 4
// }

// const (
// 	BaseURL     = "https://teo.co.il"
// 	CategoryURL = BaseURL + "/questions/c1" // Temporary hardcoded fallback or initial entrypoint
// )




// openBrowser launches the OS default browser to the target URL
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

	// Safety Check: Verify if the executable actually exists in the environment's $PATH
	if _, err := exec.LookPath(cmd); err != nil {
		// Instead of returning a scary raw exec error, we return a clean explanation
		return fmt.Errorf("no default desktop browser tool (%s) found in this environment", cmd)
	}

	return exec.Command(cmd, args...).Start()
}

// // crawlIndexPage parses the exact DOM structure
// func crawlIndexPage(targetURL string) ([]string, error) {
// 	res, err := http.Get(targetURL)
// 	if err != nil {
// 		return nil, err
// 	}
// 	defer res.Body.Close()

// 	if res.StatusCode != 200 {
// 		return nil, fmt.Errorf("status code error: %d %s", res.StatusCode, res.Status)
// 	}

// 	doc, err := goquery.NewDocumentFromReader(res.Body)
// 	if err != nil {
// 		return nil, err
// 	}

// 	var urls []string
// 	// CSS selector directly targeting the DOM structure of the index page, specifically the list of question links
// 	doc.Find("#main-self ol li a").Each(func(i int, s *goquery.Selection) {
// 		href, exists := s.Attr("href")
// 		if exists {
// 			// Handle both absolute and relative URLs gracefully
// 			if !strings.HasPrefix(href, "http") {
// 				href = BaseURL + href
// 			}
// 			urls = append(urls, href)
// 		}
// 	})

// 	return urls, nil
// }

// // parseQuestionPage dives into a single question and extracts its details based on exact DOM attributes
// func parseQuestionPage(pageURL string) (Question, error) {
// 	var q Question

// 	// Extract an ID from the trailing URL path (e.g., ".../questions/c1/3" -> "c1_3")
// 	parts := strings.Split(strings.TrimRight(pageURL, "/"), "/")
// 	if len(parts) >= 2 {
// 		q.ID = fmt.Sprintf("%s_%s", parts[len(parts)-2], parts[len(parts)-1])
// 	} else {
// 		q.ID = pageURL
// 	}

// 	res, err := http.Get(pageURL)
// 	if err != nil {
// 		return q, err
// 	}
// 	defer res.Body.Close()

// 	if res.StatusCode != 200 {
// 		return q, fmt.Errorf("status code error: %d", res.StatusCode)
// 	}

// 	doc, err := goquery.NewDocumentFromReader(res.Body)
// 	if err != nil {
// 		return q, err
// 	}

// 	// 1. Extract Question Text
// 	q.Text = strings.TrimSpace(doc.Find("#questions h3 span.question-self").First().Text())

// 	// 2. Extract Optional Image Layout
// 	// Look for an image asset inside the questions section block
// 	imgSrc, imgExists := doc.Find("#questions img").Attr("src")
// 	if imgExists {
// 		if !strings.HasPrefix(imgSrc, "http") {
// 			q.ImageURL = BaseURL + imgSrc
// 		} else {
// 			q.ImageURL = imgSrc
// 		}
// 	}

// 	// 3. Extract the 4 Answers and find the correct index based on data-correct="1"
// 	doc.Find("#questions ul li").Each(func(i int, s *goquery.Selection) {
// 		if i < 4 {
// 			// Extract answer text from the <label>
// 			answerText := strings.TrimSpace(s.Find("label").Text())
// 			q.Answers[i] = answerText

// 			// Check the companion <input> element for the correct flag attribute
// 			input := s.Find("input")
// 			if input.AttrOr("data-correct", "0") == "1" {
// 				q.CorrectAnswer = i + 1 // 1-indexed (1, 2, 3, or 4)
// 			}
// 		}
// 	})

// 	return q, nil
// }

// // downloadImage pulls the remote asset down and writes it to the local data directory
// func downloadImage(url, questionID, outputDir string) (string, error) {
// 	// Ensure the directory exists
// 	if err := os.MkdirAll(outputDir, 0755); err != nil {
// 		return "", err
// 	}

// 	resp, err := http.Get(url)
// 	if err != nil {
// 		return "", err
// 	}
// 	defer resp.Body.Close()

// 	if resp.StatusCode != http.StatusOK {
// 		return "", fmt.Errorf("failed to download image, status: %s", resp.Status)
// 	}

// 	// Determine file extension (fallback to .png if none found)
// 	ext := filepath.Ext(url)
// 	if ext == "" {
// 		ext = ".png"
// 	}

// 	// Create a predictable filename using the question ID (e.g., c1_3.png)
// 	filename := questionID + ext
// 	finalPath := filepath.Join(outputDir, filename)

// 	out, err := os.Create(finalPath)
// 	if err != nil {
// 		return "", err
// 	}
// 	defer out.Close()

// 	_, err = io.Copy(out, resp.Body)
// 	if err != nil {
// 		return "", err
// 	}

// 	return filename, nil
// }
