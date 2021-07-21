package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/go-redis/redis/v8"
	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
)

// ChatMessage structure
type ChatMessage struct {
	Username string `json:"username"`
	Text     string `json:"text"`
}

// Redis Client
var (
	rdb *redis.Client
)

// All active clients
var clients = make(map[*websocket.Conn]bool)

// Channel for sending and recieving messages
var broadcaster = make(chan ChatMessage)

// Websocket upgrade via Gorilla
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	socket, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatal(err)
	}

	// close connection when function returns
	defer socket.Close()
	clients[socket] = true

	for {
		var msg ChatMessage

		// Read the message
		err := socket.ReadJSON(&msg)
		if err != nil {
			delete(clients, socket)
			break
		}

		// See SO link: this is the channel assignment syntax.
		// Assign (send) new message to channel
		broadcaster <- msg
	}
}

// Send any new messages to every connected client
func handleMessages() {
	for {
		msg := <-broadcaster

		storeInRedis(msg)
		messageClients(msg)
	}
}

func storeInRedis(msg ChatMessage) {
	json, err := json.Marshal(msg)
	if err != nil {
		panic(err)
	}

	if err := rdb.RPush(context.TODO(), "chat_messages", json).Err(); err != nil {
		panic(err)
	}
}

func messageClients(msg ChatMessage) {
	// send to every client currently connected
	for client := range clients {
		messageClientHelper(client, msg)
	}
}

func messageClientHelper(client *websocket.Conn, msg ChatMessage) {
	err := client.WriteJSON(msg)
	if err != nil && unsafeError(err) {
		log.Print("error: %v", err)
		client.Close()
		delete(clients, client)
	}
}

// If a message is sent while a client is closing, ignore the error
func unsafeError(err error) bool {
	return !websocket.IsCloseError(err, websocket.CloseGoingAway) && err != io.EOF
}

func main() {

	err := godotenv.Load()

	if err != nil {
		log.Fatal("Error loading .env file")
	}

	port := os.Getenv("PORT")
	redisUrl := os.Getenv("REDIS_URL")

	if err != nil {
		panic(err)
	}

	rdb = redis.NewClient(&redis.Options{
		Addr:     redisUrl,
		Password: "",
		DB:       0,
	})

	http.Handle("/", http.FileServer(http.Dir("./public")))
	http.HandleFunc("/websocket", handleConnections)
	go handleMessages()

	log.Print("Server running on localhost:" + port)

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
