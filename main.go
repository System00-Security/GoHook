package main

import (
	"fmt"
	"html/template"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Connection struct {
	Timestamp time.Time
	IP        string
	Method    string
	Params    map[string]string
}

type WebhookServer struct {
	mux         sync.Mutex
	Connections []Connection
}

func GenerateRandomPath() string {
	rand.Seed(time.Now().UnixNano())
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	path := make([]byte, 10)
	for i := range path {
		path[i] = chars[rand.Intn(len(chars))]
	}
	return string(path)
}

func (wh *WebhookServer) LogConnection(r *http.Request) {
	wh.mux.Lock()
	defer wh.mux.Unlock()

	ip := r.Header.Get("X-Real-IP")
	if ip == "" {
		ip = r.RemoteAddr
	}

	params := make(map[string]string)
	for key, values := range r.URL.Query() {
		params[key] = strings.Join(values, ", ")
	}

	connection := Connection{
		Timestamp: time.Now(),
		IP:        ip,
		Method:    r.Method,
		Params:    params,
	}
	wh.Connections = append(wh.Connections, connection)

	for client := range clients {
		client.sendUpdate(connection)
	}
}

func (wh *WebhookServer) HomeHandler(w http.ResponseWriter, r *http.Request) {
	webhookURL := fmt.Sprintf("http://%s%s", r.Host, "/webhook")

	data := struct {
		WebhookURL   string
		Connections  []Connection
	}{
		WebhookURL:   webhookURL,
		Connections:  wh.Connections,
	}

	tmpl, err := template.ParseFiles("index.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	err = tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type Client struct {
	conn *websocket.Conn
}

func (c *Client) sendUpdate(connection Connection) {
	err := c.conn.WriteJSON(connection)
	if err != nil {
		log.Println("Error sending WebSocket update:", err)
	}
}

var clients = make(map[*Client]bool)
var clientsMutex sync.Mutex

func WebSocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Error upgrading WebSocket connection:", err)
		return
	}
	defer conn.Close()

	client := &Client{conn: conn}
	clientsMutex.Lock()
	clients[client] = true
	clientsMutex.Unlock()

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}

	clientsMutex.Lock()
	delete(clients, client)
	clientsMutex.Unlock()
}

func (wh *WebhookServer) WebhookHandler(w http.ResponseWriter, r *http.Request) {
	wh.LogConnection(r)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Request received successfully"))
}

func main() {
	wh := &WebhookServer{}

	http.HandleFunc("/", wh.HomeHandler)
	http.HandleFunc("/webhook", wh.WebhookHandler)
	http.HandleFunc("/ws", WebSocketHandler)

	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	log.Println("Webhook server started on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
