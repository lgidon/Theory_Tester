package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCrawlIndexPage verifies that our selector accurately captures question URLs from an index page
func TestCrawlIndexPage(t *testing.T) {
	// Mock HTML matching your exact index page DOM structure
	mockHTML := `
	<div>
		<section id="main-self">
			<section>
				<ol>
					<li><a href="/questions/c1/3">היכן תעצור...</a></li>
					<li><a href="/questions/c1/4">איזה תמרור...</a></li>
				</ol>
			</section>
		</section>
	</div>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, mockHTML)
	}))
	defer server.Close()

	urls, err := crawlIndexPage(server.URL)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(urls) != 2 {
		t.Errorf("Expected 2 URLs, got %d", len(urls))
	}

	expectedURL1 := BaseURL + "/questions/c1/3"
	if urls[0] != expectedURL1 {
		t.Errorf("Expected URL %s, got %s", expectedURL1, urls[0])
	}
}

// TestParseQuestionPage verifies that question text, images, and correct answer indices parse reliably
func TestParseQuestionPage(t *testing.T) {
	// Mock HTML matching your exact question page DOM structure
	mockHTML := `
	<div id="main">
		<section id="main-self">
			<section id="questions">
				<h3><span class="question-self">האם מותר לנהוג ברכב כבד?</span></h3>
				<img src="/assets/sign_123.png" />
				<ul>
					<li>
						<input type="radio" data-correct="0" id="q1"/>
						<label for="q1">מותר, אם ברור לחלוטין שאין דליפה.</label>
					</li>
					<li>
						<input type="radio" data-correct="1" id="q2"/>
						<label style="color:green;" for="q2">אסור.</label>
					</li>
				</ul>
			</section>
		</section>
	</div>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, mockHTML)
	}))
	defer server.Close()

	// Pass the mock server URL as our target question URL
	q, err := parseQuestionPage(server.URL)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify text parsing
	if q.Text != "האם מותר לנהוג ברכב כבד?" {
		t.Errorf("Unexpected question text: %s", q.Text)
	}

	// Verify image resolution logic
	expectedImg := BaseURL + "/assets/sign_123.png"
	if q.ImageURL != expectedImg {
		t.Errorf("Expected ImageURL %s, got %s", expectedImg, q.ImageURL)
	}

	// Verify choice extraction and index mapping
	if q.Answers[0] != "מותר, אם ברור לחלוטין שאין דליפה." {
		t.Errorf("Unexpected answer 1 text: %s", q.Answers[0])
	}

	// Verify data-correct="1" is mapped to index 2 (1-indexed)
	if q.CorrectAnswer != 2 {
		t.Errorf("Expected CorrectAnswer to be 2, got %d", q.CorrectAnswer)
	}
}