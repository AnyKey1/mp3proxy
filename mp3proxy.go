package main

import (
	"bytes"
	"database/sql"
	_ "embed"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/teris-io/shortid"
)

var (
	db             *sql.DB
	streamBuffers  = make(map[string]*StreamBuffer)
	streamBufMutex = sync.Mutex{}
)

// --- StreamBuffer: Буферизация MP3 ---
type StreamBuffer struct {
	url string
	buf *bytes.Buffer
	mu  sync.Mutex
}

func NewStreamBuffer(url string) *StreamBuffer {
	sb := &StreamBuffer{
		url: url,
		buf: bytes.NewBuffer(nil),
	}
	go sb.fillBuffer()
	return sb
}

func (s *StreamBuffer) fillBuffer() {
	for {
		resp, err := http.Get(s.url)
		if err != nil {
			log.Println("Ошибка подключения к потоку:", err)
			time.Sleep(5 * time.Second)
			continue
		}
		defer resp.Body.Close()

		log.Println("Буферизация начата:", s.url)
		buf := make([]byte, 32*1024)

		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				s.mu.Lock()
				s.buf.Write(buf[:n])
				s.mu.Unlock()
			}
			if err != nil {
				log.Println("Остановка буферизации:", err)
				break
			}
		}
	}
}

func (s *StreamBuffer) ServeToClient(w http.ResponseWriter, r *http.Request, streamID string) {
	ip := getRealIP(r)
	ua := r.UserAgent()

	log.Printf("Новое подключение: IP=%s, User-Agent=%q, Stream=%s\n", ip, ua, s.url)
	logConnection(streamID, ip, ua)

	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	time.Sleep(2 * time.Second) // Ожидание начальной буферизации

	for {
		s.mu.Lock()
		data := s.buf.Next(32 * 1024)
		s.mu.Unlock()

		if len(data) > 0 {
			_, err := w.Write(data)
			if err != nil {
				log.Printf("Отключение клиента: IP=%s, причина=%v\n", ip, err)
				return
			}
		} else {
			time.Sleep(200 * time.Millisecond)
		}
	}
}

// --- БД: потоки и подключения ---
func initDB() {
	var err error
	db, err = sql.Open("sqlite3", "./urls.db")
	if err != nil {
		log.Fatal("Не удалось открыть базу данных:", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS streams (
			id TEXT PRIMARY KEY,
			url TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		log.Fatal("Ошибка создания таблицы streams:", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS connections (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			stream_id TEXT,
			ip TEXT,
			user_agent TEXT,
			connected_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		log.Fatal("Ошибка создания таблицы connections:", err)
	}
}

func logConnection(streamID, ip, userAgent string) {
	_, err := db.Exec(
		`INSERT INTO connections (stream_id, ip, user_agent) VALUES (?, ?, ?)`,
		streamID, ip, userAgent,
	)
	if err != nil {
		log.Println("Ошибка записи подключения:", err)
	}
}

// --- HTTP ---
func streamHandler(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/")

	var url string
	err := db.QueryRow("SELECT url FROM streams WHERE id = ?", id).Scan(&url)
	if err != nil {
		http.Error(w, "Stream not found", http.StatusNotFound)
		return
	}

	buf := getOrCreateBuffer(id, url)
	buf.ServeToClient(w, r, id)
}

func addStreamHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	url := r.FormValue("url")
	if url == "" {
		http.Error(w, "Missing URL", http.StatusBadRequest)
		return
	}

	id := shortid.MustGenerate()
	_, err := db.Exec("INSERT INTO streams (id, url) VALUES (?, ?)", id, url)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		log.Println("Ошибка записи потока:", err)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("http://" + r.Host + "/" + id + "\n"))
}

func getOrCreateBuffer(id string, url string) *StreamBuffer {
	streamBufMutex.Lock()
	defer streamBufMutex.Unlock()

	if buf, ok := streamBuffers[id]; ok {
		return buf
	}
	buf := NewStreamBuffer(url)
	streamBuffers[id] = buf
	return buf
}

func getRealIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		return strings.Split(forwarded, ",")[0]
	}
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func main() {
	initDB()

	http.HandleFunc("/", streamHandler)
	http.HandleFunc("/add", addStreamHandler)

	log.Println("Сервер запущен на :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}