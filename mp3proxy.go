package main

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
 
	"github.com/teris-io/shortid"
	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

func main() {
	var err error
	db, err = sql.Open("sqlite3", "./urls.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	createTable()

	http.HandleFunc("/add", handleAdd)
	http.HandleFunc("/", handleStream)

	fmt.Println("Server started at :8080")
	log.Fatal(http.ListenAndServe(":8080", nil)) // обязательно HTTP
}

func createTable() {
	sqlStmt := `
	CREATE TABLE IF NOT EXISTS streams (
		id TEXT PRIMARY KEY,
		url TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err := db.Exec(sqlStmt)
	if err != nil {
		log.Fatalf("%q: %s\n", err, sqlStmt)
	}
}

func handleAdd(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
	if url == "" {
		http.Error(w, "url parameter is required", http.StatusBadRequest)
		return
	}

	id, err := shortid.Generate()
	if err != nil {
		http.Error(w, "failed to generate id", http.StatusInternalServerError)
		return
	}

	_, err = db.Exec("INSERT INTO streams(id, url) VALUES(?, ?)", id, url)
	if err != nil {
		http.Error(w, "failed to save url", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "http://%s/%s\n", r.Host, id)
}

func handleStream(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Path[1:] // Trim "/"
	if id == "" {
		http.NotFound(w, r)
		return
	}

	var streamURL string
	err := db.QueryRow("SELECT url FROM streams WHERE id = ?", id).Scan(&streamURL)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	resp, err := http.Get(streamURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		http.Error(w, "failed to fetch stream", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Set headers
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.WriteHeader(http.StatusOK)

	// Pipe the stream
	io.Copy(w, resp.Body)
}
