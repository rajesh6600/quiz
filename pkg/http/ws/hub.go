package ws

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

// Hub manages WebSocket connections and broadcasts messages to match participants.
type Hub struct {
	mu          sync.RWMutex
	connections map[uuid.UUID]*Connection // user_id -> connection
	matches     map[uuid.UUID][]uuid.UUID // match_id -> []user_id
	logger      zerolog.Logger
}

// NewHub creates a new WebSocket hub.
func NewHub(logger zerolog.Logger) *Hub {
	return &Hub{
		connections: make(map[uuid.UUID]*Connection),
		matches:     make(map[uuid.UUID][]uuid.UUID),
		logger:      logger,
	}
}

// RegisterConnection adds a connection for a user.
func (h *Hub) RegisterConnection(userID uuid.UUID, conn *Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Close existing connection if any
	if old, exists := h.connections[userID]; exists {
		old.Close()
	}

	h.connections[userID] = conn
	h.logger.Info().Str("user_id", userID.String()).Msg("connection registered")
}

// UnregisterConnection removes a connection.
func (h *Hub) UnregisterConnection(userID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if conn, exists := h.connections[userID]; exists {
		conn.Close()
		delete(h.connections, userID)
		h.logger.Info().Str("user_id", userID.String()).Msg("connection unregistered")
	}

	// Remove from all matches
	for matchID, users := range h.matches {
		for i, uid := range users {
			if uid == userID {
				h.matches[matchID] = append(users[:i], users[i+1:]...)
				break
			}
		}
	}
}

// JoinMatch associates a user with a match for targeted broadcasts.
func (h *Hub) JoinMatch(matchID, userID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()

	users := h.matches[matchID]
	for _, uid := range users {
		if uid == userID {
			return // already joined
		}
	}
	h.matches[matchID] = append(users, userID)
}

// LeaveMatch removes a user from a match.
func (h *Hub) LeaveMatch(matchID, userID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()

	users := h.matches[matchID]
	for i, uid := range users {
		if uid == userID {
			h.matches[matchID] = append(users[:i], users[i+1:]...)
			break
		}
	}
}

// BroadcastToMatch sends a message to all players in a match.
func (h *Hub) BroadcastToMatch(matchID uuid.UUID, msg Message) error {
	h.mu.RLock()
	users := h.matches[matchID]
	h.mu.RUnlock()

	var errors []error
	for _, userID := range users {
		if err := h.SendToUser(userID, msg); err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		return errors[0] // return first error
	}
	return nil
}

// BroadcastAll sends a message to every connected user.
func (h *Hub) BroadcastAll(msg Message) error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var firstErr error
	for userID, conn := range h.connections {
		if err := conn.Send(msg); err != nil && firstErr == nil {
			firstErr = err
			h.logger.Warn().Err(err).Str("user_id", userID.String()).Msg("broadcast_all_send_failed")
		}
	}
	return firstErr
}

// SendToUser delivers a message to a specific user.
func (h *Hub) SendToUser(userID uuid.UUID, msg Message) error {
	h.mu.RLock()
	conn, exists := h.connections[userID]
	h.mu.RUnlock()

	if !exists {
		return ErrConnectionNotFound
	}

	return conn.Send(msg)
}

// GetConnection retrieves a connection for a user.
func (h *Hub) GetConnection(userID uuid.UUID) (*Connection, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	conn, exists := h.connections[userID]
	return conn, exists
}

// Connection represents a WebSocket connection with send queue.
type Connection struct {
	conn   *websocket.Conn
	sendCh chan Message
	mu     sync.Mutex
	closed bool
	logger zerolog.Logger
}

// NewConnection wraps a WebSocket connection.
func NewConnection(conn *websocket.Conn, logger zerolog.Logger) *Connection {
	return &Connection{
		conn:   conn,
		sendCh: make(chan Message, 256),
		logger: logger,
	}
}

// Send queues a message for delivery.
func (c *Connection) Send(msg Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return ErrConnectionClosed
	}

	select {
	case c.sendCh <- msg:
		return nil
	default:
		return ErrSendQueueFull
	}
}

// Close shuts down the connection.
func (c *Connection) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}

	c.closed = true
	close(c.sendCh)
	c.conn.Close()
}

// WritePump sends messages from the send queue.
func (c *Connection) WritePump() {
	defer c.conn.Close()

	for {
		select {
		case msg, ok := <-c.sendCh:
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteJSON(msg); err != nil {
				c.logger.Warn().Err(err).Msg("write error")
				return
			}
		}
	}
}

// ReadPump receives messages and calls the handler.
func (c *Connection) ReadPump(handler func(Message) error) {
	defer c.conn.Close()

	// Set read deadline to 60 seconds, extend on pong
	readDeadline := time.Now().Add(60 * time.Second)
	c.conn.SetReadDeadline(readDeadline)
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		var msg Message
		if err := c.conn.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.logger.Warn().Err(err).Msg("read error")
			}
			break
		}

		if err := handler(msg); err != nil {
			c.logger.Warn().Err(err).Msg("message handler error")
		}
	}
}

var (
	ErrConnectionNotFound = &Error{Code: "connection_not_found", Message: "User connection not found"}
	ErrConnectionClosed   = &Error{Code: "connection_closed", Message: "Connection is closed"}
	ErrSendQueueFull      = &Error{Code: "send_queue_full", Message: "Send queue is full"}
)

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string {
	return e.Message
}
