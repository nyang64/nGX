package server

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	authpkg "agentmail/pkg/auth"
	redispkg "agentmail/pkg/redis"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// CORS is handled by the chi cors middleware; allow all origins here.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Hub maintains the set of active WebSocket clients and fans out messages from Redis.
type Hub struct {
	mu         sync.RWMutex
	clients    map[string]map[*Client]struct{} // orgID → set of clients
	register   chan *Client
	unregister chan *Client
	redis      *redis.Client
}

// Client is a single WebSocket connection.
type Client struct {
	hub    *Hub
	conn   *websocket.Conn
	send   chan []byte
	orgID  string
	claims *authpkg.Claims
}

// NewHub creates a Hub using the provided Redis client for pub/sub.
func NewHub(redisClient *redis.Client) *Hub {
	return &Hub{
		clients:    make(map[string]map[*Client]struct{}),
		register:   make(chan *Client, 16),
		unregister: make(chan *Client, 16),
		redis:      redisClient,
	}
}

// Run processes register/unregister events until ctx is cancelled.
func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case client := <-h.register:
			h.mu.Lock()
			if h.clients[client.orgID] == nil {
				h.clients[client.orgID] = make(map[*Client]struct{})
				// Subscribe to Redis channel for this org the first time a client joins.
				go h.subscribeOrg(ctx, client.orgID)
			}
			h.clients[client.orgID][client] = struct{}{}
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.orgID]; ok {
				delete(h.clients[client.orgID], client)
				close(client.send)
				if len(h.clients[client.orgID]) == 0 {
					delete(h.clients, client.orgID)
				}
			}
			h.mu.Unlock()
		}
	}
}

// subscribeOrg subscribes to the Redis pub/sub channel for orgID and broadcasts
// incoming messages to all connected clients for that org.
func (h *Hub) subscribeOrg(ctx context.Context, orgID string) {
	channel := redispkg.WebSocketChannel(orgID)
	sub := h.redis.Subscribe(ctx, channel)
	defer sub.Close()

	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			h.broadcast(orgID, []byte(msg.Payload))
		}
	}
}

// broadcast fans out payload to all clients registered for orgID.
func (h *Hub) broadcast(orgID string, payload []byte) {
	h.mu.RLock()
	clients := h.clients[orgID]
	h.mu.RUnlock()
	for client := range clients {
		select {
		case client.send <- payload:
		default:
			// Slow client — drop the message rather than blocking.
		}
	}
}

// ServeWS upgrades an HTTP connection to WebSocket. It requires valid Claims in ctx.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	claims := authpkg.ClaimsFromCtx(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade", "error", err)
		return
	}

	client := &Client{
		hub:    h,
		conn:   conn,
		send:   make(chan []byte, 256),
		orgID:  claims.OrgID.String(),
		claims: claims,
	}

	h.register <- client

	go client.writePump()
	go client.readPump()
}

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512
)

// writePump pumps messages from the send channel to the WebSocket connection.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump reads from the WebSocket connection. The API gateway does not expect
// client messages; we only need to handle pong frames and detect disconnects.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}
