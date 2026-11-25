package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
)

var jwtSecret []byte

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
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
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
		err := c.Conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Ошибка чтения: %v", err)
			}
			break
		}

		outMsg := OutgoingMessage{
			Username: c.Username,
			Text:     msg.Text,
			Time:     time.Now().Format("15:04"),
		}
		
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

func main() {
	_ = godotenv.Load("../.env") 
	secret := os.Getenv("JWT_SECRET_KEY")
	if secret == "" {
		log.Fatal("JWT_SECRET_KEY не найден в .env")
	}
	jwtSecret = []byte(secret)

	go hub.Run()

	http.HandleFunc("/ws", handleConnections)
	
	log.Println("Чат-сервер запущен на ws://127.0.0.1:8080/ws")
	if err := http.ListenAndServe("127.0.0.1:8080", nil); err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}