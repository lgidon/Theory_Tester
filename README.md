# Israel Theory Exam Preparation App (Stage 1)
A high-performance, container-ready application built in Go to scrape, store, and study for the Israeli driving theory examination. Stage 1 focuses on the core data engine: index crawling, deep parsing, downloading static assets, and initializing a local persistence layer.

## 🚀 Completed Architecture
### Index Crawler: 
Parses category listings dynamically (currently configured for C1) to harvest question page routes.

### Deep Scraper: 
Explores question endpoints, handles modern Hebrew web DOM typography, and accurately identifies the correct answer using data element attributes.

### Asset Pipeline: 
Intercepts, maps, and downloads question images natively to local storage to eliminate live dependencies on external web servers.

### Storage Layer: 
Fully normalized multi-table schema using SQLite to manage static data blocks and user test state maps natively.

## 📁 Directory Structure
```text
israel-theory-scraper/
├── data/                    # Generated automatically
│   ├── theory.db            # SQLite persistent database file
│   └── images/              # Downloaded question images (.png, .jpg)
├── frontend/                # Prepared for Stage 2
│   └── dist/
│       └── index.html       # Responsive, RTL-styled study dashboard
├── db.go                    # Database connections, schemas, and transactions
├── scrapper.go              # Main execution entrypoint and network crawl code
├── scrapper_test.go         # Unit tests simulating page parsing variants
└── go.mod / go.sum          # Go dependency declarations
```

## 🛠️ Verification Commands
### Run Unit Tests
To confirm that parsing mechanisms, image extensions, and multi-choice indices map out correctly against mock HTML frames without hitting the live network:

```go
go test -v .
```

Run Ingestion
To execute the first-run scraper loop, populate your local files, and fetch question assets:

```go
go run scrapper.go db.go
```

Query SQLite Internally
To run interactive ad-hoc SQL assertions within your environment shell:


### 1. Install SQLite CLI dependencies
```bash
sudo apt-get update && sudo apt-get install -y sqlite3
```

### 2. Enter interactive database shell
```bash
sqlite3 ./data/theory.db
```
Inside the interactive prompt (sqlite>), run:
```
SQL
.mode table
.headers on
SELECT id, question_text, correct_answer FROM questions LIMIT 5;
.exit
```
