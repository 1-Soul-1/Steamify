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

// ===== ЧАТ (КОМНАТЫ) =====
type IncomingMessage struct {
	Text string `json:"text"`
}

type OutgoingMessage struct {
	Type     string `json:"type"`
	Username string `json:"username,omitempty"`
	Text     string `json:"text"`
	Time     string `json:"time"`
}

type Client struct {
	Conn     *websocket.Conn
	Send     chan OutgoingMessage
	Username string
}

type Room struct {
	Name       string
	Clients    map[*Client]bool
	Broadcast  chan OutgoingMessage
	Register   chan *Client
	Unregister chan *Client
	History    []OutgoingMessage
	mu         sync.Mutex
}

var (
	rooms   = make(map[string]*Room)
	roomsMu sync.Mutex
)

func getOrCreateRoom(name string) *Room {
	roomsMu.Lock()
	defer roomsMu.Unlock()

	if r, ok := rooms[name]; ok {
		return r
	}

	r := &Room{
		Name:       name,
		Broadcast:  make(chan OutgoingMessage),
		Register:   make(chan *Client),
		Unregister: make(chan *Client),
		Clients:    make(map[*Client]bool),
		History:    make([]OutgoingMessage, 0),
	}
	rooms[name] = r
	go r.Run()
	return r
}

func (h *Room) addToHistory(msg OutgoingMessage) {
	h.History = append(h.History, msg)
	if len(h.History) > 100 {
		h.History = h.History[1:]
	}
}

func (h *Room) Run() {
	for {
		select {
		case client := <-h.Register:
			h.mu.Lock()
			h.Clients[client] = true
			
			// Отправляем историю
			for _, msg := range h.History {
				client.Send <- msg
			}

			joinMsg := OutgoingMessage{
				Type: "system",
				Text: client.Username + " присоединился к чату",
				Time: time.Now().Format("15:04"),
			}

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
	roomName := r.URL.Query().Get("room")
	if tokenString == "" || roomName == "" {
		http.Error(w, "Токен или комната не предоставлены", http.StatusUnauthorized)
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
		return
	}
	
	room := getOrCreateRoom(roomName)
	client := &Client{Conn: conn, Send: make(chan OutgoingMessage, 256), Username: username}
	room.Register <- client
	go client.writePump(room)
	go client.readPump(room)
}

func (c *Client) readPump(room *Room) {
	defer func() {
		room.Unregister <- c
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
		room.Broadcast <- outMsg
	}
}

func (c *Client) writePump(room *Room) {
	defer c.Conn.Close()
	for {
		message, ok := <-c.Send
		if !ok {
			c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
		}
		c.Conn.WriteJSON(message)
	}
}

// ===== СТРИМ (КОМНАТЫ) =====
var (
	hlsDir         = "./hls"
	ffmpegCmds     = make(map[string]*exec.Cmd)
	testStreamCmds = make(map[string]*exec.Cmd)
	ffmpegMu       sync.Mutex
)

func startFFmpeg(room string) {
	ffmpegMu.Lock()
	defer ffmpegMu.Unlock()
	
	if _, ok := ffmpegCmds[room]; ok {
		return // Уже запущен
	}

	roomDir := filepath.Join(hlsDir, room)
	os.MkdirAll(roomDir, os.ModePerm)
	
	files, _ := filepath.Glob(filepath.Join(roomDir, "*"))
	for _, f := range files {
		os.Remove(f)
	}

	args := []string{
		"-i", "rtmp://127.0.0.1:1935/live/" + room,
		"-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency",
		"-c:a", "aac",
		"-f", "hls", "-hls_time", "2", "-hls_list_size", "5", "-hls_flags", "delete_segments",
		filepath.Join(roomDir, "stream.m3u8"),
	}
	cmd := exec.Command("ffmpeg", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log.Println("Ошибка запуска HLS-конвертера:", err)
		return
	}
	ffmpegCmds[room] = cmd

	go func() {
		cmd.Wait()
		ffmpegMu.Lock()
		delete(ffmpegCmds, room)
		ffmpegMu.Unlock()
	}()
}

func startTestStream(room string) error {
	ffmpegMu.Lock()
	defer ffmpegMu.Unlock()
	
	if _, ok := testStreamCmds[room]; ok {
		return nil
	}

	args := []string{
		"-re",
		"-f", "lavfi", "-i", "testsrc=size=1280x720:rate=30",
		"-f", "lavfi", "-i", "sine=frequency=1000",
		"-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency",
		"-c:a", "aac",
		"-f", "flv", "rtmp://127.0.0.1:1935/live/" + room,
	}
	cmd := exec.Command("ffmpeg", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}
	testStreamCmds[room] = cmd
	return nil
}

func stopTestStream(room string) {
	ffmpegMu.Lock()
	defer ffmpegMu.Unlock()

	if cmd, ok := testStreamCmds[room]; ok && cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
		cmd.Wait()
		delete(testStreamCmds, room)
	}

	if cmd, ok := ffmpegCmds[room]; ok && cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
		cmd.Wait()
		delete(ffmpegCmds, room)
	}
}

func handleGetStreams(w http.ResponseWriter, r *http.Request) {
	ffmpegMu.Lock()
	defer ffmpegMu.Unlock()
	
	var active []string
	for room := range ffmpegCmds {
		active = append(active, room)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(active)
}

func handleStartStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == "OPTIONS" { return }
	
	room := r.URL.Query().Get("room")
	if room == "" { room = "default" }

	if err := startTestStream(room); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write([]byte(`{"status":"started"}`))
}

func handleStopStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == "OPTIONS" { return }
	
	room := r.URL.Query().Get("room")
	if room == "" { room = "default" }
	
	stopTestStream(room)
	w.Write([]byte(`{"status":"stopped"}`))
}

type rtmpHandler struct {
	rtmp.DefaultHandler
}

func (h *rtmpHandler) OnPublish(ctx *rtmp.StreamContext, timestamp uint32, cmd *message.NetStreamPublish) error {
	room := cmd.PublishingName
	if room == "" { room = "default" }
	log.Printf("Начата трансляция в комнате: %s", room)
	startFFmpeg(room)
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

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", handleConnections)
	mux.HandleFunc("/api/streams", handleGetStreams)
	mux.HandleFunc("/api/stream/start", handleStartStream)
	mux.HandleFunc("/api/stream/stop", handleStopStream)
	
	fs := http.FileServer(http.Dir(hlsDir))
	mux.Handle("/hls/", http.StripPrefix("/hls/", fs))

	log.Println("HTTP-сервер на http://127.0.0.1:8080")
	if err := http.ListenAndServe("127.0.0.1:8080", mux); err != nil { log.Fatal("ListenAndServe: ", err) }
}
