package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/cloudronix/agent/internal/config"
	"github.com/gorilla/websocket"
)

// JobNotification is received when a new job is available
type JobNotification struct {
	Type         string `json:"type"`
	JobID        string `json:"job_id"`
	PlaybookName string `json:"playbook_name"`
}

// WebSocketClient manages the WebSocket connection to the server
type WebSocketClient struct {
	cfg        *config.Config
	conn       *websocket.Conn
	jobChannel chan JobNotification
	done       chan struct{}
}

// NewWebSocketClient creates a new WebSocket client
func NewWebSocketClient(cfg *config.Config) *WebSocketClient {
	return &WebSocketClient{
		cfg:        cfg,
		jobChannel: make(chan JobNotification, 100),
		done:       make(chan struct{}),
	}
}

// Connect establishes the WebSocket connection
func (c *WebSocketClient) Connect(ctx context.Context) error {
	// Reset done channel for reconnection
	c.done = make(chan struct{})

	// Convert http:// to ws:// or https:// to wss://
	wsURL := c.cfg.AgentURL
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)

	u, err := url.Parse(wsURL + "/agent/ws")
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	fmt.Printf("Connecting to WebSocket: %s\n", u.String())

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	c.conn = conn

	// Send device ID to authenticate
	if err := conn.WriteMessage(websocket.TextMessage, []byte(c.cfg.DeviceID)); err != nil {
		conn.Close()
		return fmt.Errorf("failed to send device ID: %w", err)
	}

	// Wait for confirmation
	_, msg, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to read confirmation: %w", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(msg, &resp); err != nil {
		conn.Close()
		return fmt.Errorf("invalid confirmation: %w", err)
	}

	if _, ok := resp["connected"]; !ok {
		conn.Close()
		return fmt.Errorf("connection rejected: %s", string(msg))
	}

	fmt.Println("WebSocket connected - real-time job notifications enabled")

	// Start reading messages
	go c.readMessages()

	return nil
}

// readMessages reads incoming WebSocket messages
func (c *WebSocketClient) readMessages() {
	defer close(c.done)

	for {
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				fmt.Printf("WebSocket error: %v\n", err)
			}
			return
		}

		var notification JobNotification
		if err := json.Unmarshal(msg, &notification); err != nil {
			continue
		}

		if notification.Type == "new_job" {
			fmt.Printf(">>> NEW JOB: %s (%s)\n", notification.PlaybookName, notification.JobID[:8])
			select {
			case c.jobChannel <- notification:
			default:
				// Channel full, job will be picked up by polling
			}
		}
	}
}

// JobChannel returns the channel for job notifications
func (c *WebSocketClient) JobChannel() <-chan JobNotification {
	return c.jobChannel
}

// Close closes the WebSocket connection
func (c *WebSocketClient) Close() error {
	if c.conn != nil {
		// Send close message
		c.conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		time.Sleep(100 * time.Millisecond)
		return c.conn.Close()
	}
	return nil
}

// Done returns a channel that's closed when the connection is lost
func (c *WebSocketClient) Done() <-chan struct{} {
	return c.done
}
