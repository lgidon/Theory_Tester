package main


import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"
	"io"
	"os/signal"
	"syscall"
	"context"
)

// Global or package-level verbosity tracker
var verbose bool

// infoPrintf only prints to stdout if verbose mode is enabled
func infoPrintf(format string, a ...interface{}) {
	if verbose {
		fmt.Printf(format, a...)
	}
}

// infoPrintln only prints to stdout if verbose mode is enabled
func infoPrintln(a ...interface{}) {
	if verbose {
		fmt.Println(a...)
	}
}

func main() {
	dbPath := "./data/theory.db"
	licenseType := "C1"

	// 1. Define command-line flags
	port := flag.String("port", "8080", "Port to run the server on")
	localMode := flag.Bool("local", true, "Run locally (binds to localhost, auto-opens browser)")
	verboseFlag := flag.Bool("verbose", false, "Enable detailed debug and scraping logs")
	flag.Parse()

	verbose = *verboseFlag

	// 2. Control log package output
	if !verbose {
		log.SetOutput(io.Discard) 
	}

	// 3. Initialize DB Client
	db, err := InitDB(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Database initialization failed: %v\n", err)
		os.Exit(1)
	}

	// 4. Check & run first-run scrape (Hydration)
	needsScrape, err := db.IsDatabaseEmpty(licenseType)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to verify database state: %v\n", err)
		db.Conn.Close()
		os.Exit(1)
	}

	if needsScrape {
		fmt.Printf("Preparing your database for %s (this only happens once)... ", licenseType)
		// ... (Scraper loop execution logic) ...
		fmt.Println("Done! 🎉")
	}

	// 5. Configure server address
	host := "0.0.0.0"
	if *localMode {
		host = "127.0.0.1"
	}
	addr := fmt.Sprintf("%s:%s", host, *port)
	server := NewWebServer(db, licenseType)

	// 6. Set up a channel to listen for OS shutdown signals
	shutdownSig := make(chan os.Signal, 1)
	signal.Notify(shutdownSig, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	// 7. Start the server in a goroutine so it doesn't block main
	serverError := make(chan error, 1)
	go func() {
		if *localMode {
			go func() {
				url := fmt.Sprintf("http://localhost:%s", *port)
				fmt.Printf("🚀 Launching local application at %s\n", url)
				time.Sleep(200 * time.Millisecond)
				_ = openBrowser(url) // Ignore error on headless environments
			}()
		} else {
			fmt.Printf("🚀 Server mode active. Listening on http://%s\n", addr)
		}
		
		serverError <- server.Start(addr)
	}()

	// 8. Block and wait for either a server crash or a shutdown signal
	select {
	case err := <-serverError:
		fmt.Fprintf(os.Stderr, "Server shutdown unexpectedly: %v\n", err)
		db.Conn.Close()
		os.Exit(1)

	case sig := <-shutdownSig:
		fmt.Printf("\nReceived signal (%s). Gracefully shutting down...\n", sig)

		// Create a timeout context for the shutdown sequence (e.g., 5 seconds)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Tell the server to stop accepting new requests and finish existing ones
		// (Assuming your server struct exposes a Shutdown method wrapping http.Server.Shutdown)
		if err := server.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Server forced to shutdown: %v\n", err)
		}

		// Close database connection cleanly
		if err := db.Conn.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing database: %v\n", err)
		}

		fmt.Println("Goodbye! 👋")
	}
}