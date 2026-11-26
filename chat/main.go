package main

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
	"github.com/yutopp/go-rtmp"
	"github.com/yutopp/go-rtmp/message"
)

var jwtSecret []byte

// ===== ЧАТ =====
type IncomingMessage struct {
	Text string `json:"text"`
}

type OutgoingMessage struct {
	Username string `json:"username"`
	Text     string `json:"text"`
	Time     string `json:"time"`
}

type Client struct {
	Conn     *websocket.Conn
	Send     chan []byte
	Username string
}

type Hub struct {
	Clients    map[*Client]bool
	Broadcast  chan []byte
	Register   chan *Client
	Unregister chan *Client
	mu         sync.Mutex
}

var hub = Hub{
	Broadcast:  make(chan []byte),
	Register:   make(chan *Client),
	Unregister: make(chan *Client),
	Clients:    make(map[*Client]bool),
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			h.mu.Lock()
			h.Clients[client] = true
			h.mu.Unlock()
		case client := <-h.Unregister:
			h.mu.Lock()
			if _, ok := h.Clients[client]; ok {
				delete(h.Clients, client)
				close(client.Send)
			}
			h.mu.Unlock()
		case message := <-h.Broadcast:
			h.mu.Lock()
			for client := range h.Clients {
				select {
				case client.Send <- message:
				default:
					close(client.Send)
					delete(h.Clients, client)
				}
			}
			h.mu.Unlock()
		}
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	tokenString := r.URL.Query().Get("token")
	if tokenString == "" {
		http.Error(w, "Токен не предоставлен", http.StatusUnauthorized)
		return
	}
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	})
	if err != nil || !token.Valid {
		http.Error(w, "Недействительный токен", http.StatusUnauthorized)
		return
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		http.Error(w, "Ошибка чтения токена", http.StatusUnauthorized)
		return
	}
	email, _ := claims["email"].(string)
	username := strings.Split(email, "@")[0]

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Ошибка Upgrade:", err)
		return
	}
	client := &Client{Conn: conn, Send: make(chan []byte, 256), Username: username}
	hub.Register <- client
	go client.writePump()
	go client.readPump()
}

func (c *Client) readPump() {
	defer func() {
		hub.Unregister <- c
		c.Conn.Close()
	}()
	for {
		var msg IncomingMessage
		if err := c.Conn.ReadJSON(&msg); err != nil {
			break
		}
		outMsg := OutgoingMessage{Username: c.Username, Text: msg.Text, Time: time.Now().Format("15:04")}
		bytes, _ := json.Marshal(outMsg)
		hub.Broadcast <- bytes
	}
}

func (c *Client) writePump() {
	defer c.Conn.Close()
	for {
		message, ok := <-c.Send
		if !ok {
			c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
		}
		c.Conn.WriteMessage(websocket.TextMessage, message)
	}
}

// ===== СТРИМ =====
var (
	hlsDir      = "./hls"
	ffmpegCmd   *exec.Cmd
	ffmpegMu    sync.Mutex
	ffmpegAlive bool

	testStreamCmd *exec.Cmd
	testStreamMu  sync.Mutex
)

func startFFmpeg() {
	ffmpegMu.Lock()
	defer ffmpegMu.Unlock()
	if ffmpegAlive {
		log.Println("HLS-конвертер уже запущен")
		return
	}

	os.MkdirAll(hlsDir, os.ModePerm)
	files, _ := filepath.Glob(filepath.Join(hlsDir, "*"))
	for _, f := range files {
		os.Remove(f)
	}

	args := []string{
		"-i", "rtmp://127.0.0.1:1935/live/stream",
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-tune", "zerolatency",
		"-c:a", "aac",
		"-f", "hls",
		"-hls_time", "2",
		"-hls_list_size", "5",
		"-hls_flags", "delete_segments",
		filepath.Join(hlsDir, "stream.m3u8"),
	}
	ffmpegCmd = exec.Command("ffmpeg", args...)
	ffmpegCmd.Stdout = os.Stdout
	ffmpegCmd.Stderr = os.Stderr

	if err := ffmpegCmd.Start(); err != nil {
		log.Println("Ошибка запуска HLS-конвертера:", err)
		return
	}
	ffmpegAlive = true
	log.Println("HLS-конвертер запущен, ждём RTMP поток...")

	go func() {
		ffmpegCmd.Wait()
		ffmpegMu.Lock()
		ffmpegAlive = false
		ffmpegMu.Unlock()
		log.Println("HLS-конвертер завершился")
	}()
}

// Запуск тестового стрима (генерация видео и отправка его на RTMP)
func startTestStream() error {
	testStreamMu.Lock()
	defer testStreamMu.Unlock()
	if testStreamCmd != nil {
		return nil // Уже запущен
	}

	args := []string{
		"-re", // Читаем в реальном времени
		"-f", "lavfi", "-i", "testsrc=size=1280x720:rate=30", // Тестовая картинка
		"-f", "lavfi", "-i", "sine=frequency=1000",           // Тестовый звук
		"-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency",
		"-c:a", "aac",
		"-f", "flv", "rtmp://127.0.0.1:1935/live/stream",
	}
	testStreamCmd = exec.Command("ffmpeg", args...)
	testStreamCmd.Stdout = os.Stdout
	testStreamCmd.Stderr = os.Stderr

	if err := testStreamCmd.Start(); err != nil {
		testStreamCmd = nil
		return err
	}
	log.Println("Тестовый стрим запущен")
	return nil
}

// Остановка тестового стрима
func stopTestStream() {
	testStreamMu.Lock()
	defer testStreamMu.Unlock()
	if testStreamCmd != nil && testStreamCmd.Process != nil {
		testStreamCmd.Process.Kill()
		testStreamCmd.Wait()
		testStreamCmd = nil
		log.Println("Тестовый стрим остановлен")
	}

	// Также убиваем HLS-конвертер, чтобы он не висел
	ffmpegMu.Lock()
	if ffmpegCmd != nil && ffmpegCmd.Process != nil {
		ffmpegCmd.Process.Kill()
		ffmpegCmd.Wait()
		ffmpegAlive = false
	}
	ffmpegMu.Unlock()
}

// Обработчики HTTP API для управления тестовым стримом
func handleStartStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	if err := startTestStream(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write([]byte(`{"status":"started"}`))
}

func handleStopStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	stopTestStream()
	w.Write([]byte(`{"status":"stopped"}`))
}

// ===== RTMP =====
type rtmpHandler struct {
	rtmp.DefaultHandler
}

func (h *rtmpHandler) OnPublish(ctx *rtmp.StreamContext, timestamp uint32, cmd *message.NetStreamPublish) error {
	log.Println("Входящий RTMP поток от OBS или теста")
	startFFmpeg()
	return nil
}

func startRTMP() {
	listener, err := net.Listen("tcp", ":1935")
	if err != nil {
		log.Fatal("RTMP listen:", err)
	}

	srv := rtmp.NewServer(&rtmp.ServerConfig{
		OnConnect: func(conn net.Conn) (io.ReadWriteCloser, *rtmp.ConnConfig) {
			return conn, &rtmp.ConnConfig{
				Handler: &rtmpHandler{},
			}
		},
	})

	log.Println("RTMP-сервер на rtmp://127.0.0.1:1935/live")
	if err := srv.Serve(listener); err != nil {
		log.Fatal("RTMP serve:", err)
	}
}

// ===== MAIN =====
func main() {
	_ = godotenv.Load("../.env")
	secret := os.Getenv("JWT_SECRET_KEY")
	if secret == "" {
		log.Fatal("JWT_SECRET_KEY не найден в .env")
	}
	jwtSecret = []byte(secret)

	go startRTMP()
	go hub.Run()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", handleConnections)

	// Раздача HLS
	fs := http.FileServer(http.Dir(hlsDir))
	mux.Handle("/hls/", http.StripPrefix("/hls/", fs))

	// Управление стримом
	mux.HandleFunc("/api/stream/start", handleStartStream)
	mux.HandleFunc("/api/stream/stop", handleStopStream)

	log.Println("HTTP-сервер на http://127.0.0.1:8080")
	if err := http.ListenAndServe("127.0.0.1:8080", mux); err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}