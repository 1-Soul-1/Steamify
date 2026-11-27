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
	Type     string `json:"type"`               // "chat" или "system"
	Username string `json:"username,omitempty"` // необязательное для системных
	Text     string `json:"text"`
	Time     string `json:"time"`
}

type Client struct {
	Conn     *websocket.Conn
	Send     chan OutgoingMessage // Изменено на структуру
	Username string
}

type Hub struct {
	Clients    map[*Client]bool
	Broadcast  chan OutgoingMessage // Изменено на структуру
	Register   chan *Client
	Unregister chan *Client
	History    []OutgoingMessage    // НОВОЕ: история сообщений
	mu         sync.Mutex
}

var hub = Hub{
	Broadcast:  make(chan OutgoingMessage),
	Register:   make(chan *Client),
	Unregister: make(chan *Client),
	Clients:    make(map[*Client]bool),
	History:    make([]OutgoingMessage, 0),
}

// Вспомогательная функция для добавления в историю (храним последние 100)
func (h *Hub) addToHistory(msg OutgoingMessage) {
	h.History = append(h.History, msg)
	if len(h.History) > 100 {
		h.History = h.History[1:]
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			h.mu.Lock()
			h.Clients[client] = true
			
			// 1. Отправляем новому пользователю историю сообщений
			for _, msg := range h.History {
				client.Send <- msg
			}

			// 2. Создаем системное сообщение о входе
			joinMsg := OutgoingMessage{
				Type: "system",
				Text: client.Username + " присоединился к чату",
				Time: time.Now().Format("15:04"),
			}

			// 3. Сохраняем в историю и рассылаем всем
			h.addToHistory(joinMsg)
			for c := range h.Clients {
				c.Send <- joinMsg
			}
			h.mu.Unlock()

		case client := <-h.Unregister:
			h.mu.Lock()
			if _, ok := h.Clients[client]; ok {
				delete(h.Clients, client)
				close(client.Send)
				
				// Системное сообщение о выходе
				leaveMsg := OutgoingMessage{
					Type: "system",
					Text: client.Username + " покинул чат",
					Time: time.Now().Format("15:04"),
				}
				h.addToHistory(leaveMsg)
				for c := range h.Clients {
					c.Send <- leaveMsg
				}
			}
			h.mu.Unlock()

		case message := <-h.Broadcast:
			h.mu.Lock()
			h.addToHistory(message)
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
	client := &Client{Conn: conn, Send: make(chan OutgoingMessage, 256), Username: username}
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
		outMsg := OutgoingMessage{
			Type:     "chat",
			Username: c.Username,
			Text:     msg.Text,
			Time:     time.Now().Format("15:04"),
		}
		hub.Broadcast <- outMsg
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
		c.Conn.WriteJSON(message) // Отправляем как JSON структуру
	}
}

// ===== СТРИМ (БЕЗ ИЗМЕНЕНИЙ) =====
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
		return
	}

	os.MkdirAll(hlsDir, os.ModePerm)
	files, _ := filepath.Glob(filepath.Join(hlsDir, "*"))
	for _, f := range files {
		os.Remove(f)
	}

	args := []string{
		"-i", "rtmp://127.0.0.1:1935/live/stream",
		"-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency",
		"-c:a", "aac",
		"-f", "hls", "-hls_time", "2", "-hls_list_size", "5", "-hls_flags", "delete_segments",
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

	go func() {
		ffmpegCmd.Wait()
		ffmpegMu.Lock()
		ffmpegAlive = false
		ffmpegMu.Unlock()
	}()
}

func startTestStream() error {
	testStreamMu.Lock()
	defer testStreamMu.Unlock()
	if testStreamCmd != nil {
		return nil
	}

	args := []string{
		"-re",
		"-f", "lavfi", "-i", "testsrc=size=1280x720:rate=30",
		"-f", "lavfi", "-i", "sine=frequency=1000",
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
	return nil
}

func stopTestStream() {
	testStreamMu.Lock()
	defer testStreamMu.Unlock()
	if testStreamCmd != nil && testStreamCmd.Process != nil {
		testStreamCmd.Process.Kill()
		testStreamCmd.Wait()
		testStreamCmd = nil
	}

	ffmpegMu.Lock()
	if ffmpegCmd != nil && ffmpegCmd.Process != nil {
		ffmpegCmd.Process.Kill()
		ffmpegCmd.Wait()
		ffmpegAlive = false
	}
	ffmpegMu.Unlock()
}

func handleStartStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == "OPTIONS" { return }
	if err := startTestStream(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write([]byte(`{"status":"started"}`))
}

func handleStopStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == "OPTIONS" { return }
	stopTestStream()
	w.Write([]byte(`{"status":"stopped"}`))
}

type rtmpHandler struct {
	rtmp.DefaultHandler
}

func (h *rtmpHandler) OnPublish(ctx *rtmp.StreamContext, timestamp uint32, cmd *message.NetStreamPublish) error {
	startFFmpeg()
	return nil
}

func startRTMP() {
	listener, err := net.Listen("tcp", ":1935")
	if err != nil { log.Fatal("RTMP listen:", err) }

	srv := rtmp.NewServer(&rtmp.ServerConfig{
		OnConnect: func(conn net.Conn) (io.ReadWriteCloser, *rtmp.ConnConfig) {
			return conn, &rtmp.ConnConfig{Handler: &rtmpHandler{}}
		},
	})

	if err := srv.Serve(listener); err != nil { log.Fatal("RTMP serve:", err) }
}

func main() {
	_ = godotenv.Load("../.env")
	secret := os.Getenv("JWT_SECRET_KEY")
	if secret == "" { log.Fatal("JWT_SECRET_KEY не найден в .env") }
	jwtSecret = []byte(secret)

	go startRTMP()
	go hub.Run()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", handleConnections)
	fs := http.FileServer(http.Dir(hlsDir))
	mux.Handle("/hls/", http.StripPrefix("/hls/", fs))
	mux.HandleFunc("/api/stream/start", handleStartStream)
	mux.HandleFunc("/api/stream/stop", handleStopStream)

	log.Println("HTTP-сервер на http://127.0.0.1:8080")
	if err := http.ListenAndServe("127.0.0.1:8080", mux); err != nil { log.Fatal("ListenAndServe: ", err) }
}
