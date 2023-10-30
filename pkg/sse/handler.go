package sse

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type SSEHandler struct {
	clients        map[chan string]bool
	newClients     chan chan string
	defunctClients chan chan string
	messages       chan string
}

func NewSSEHandler() *SSEHandler {
	return &SSEHandler{
		clients:        make(map[chan string]bool),
		newClients:     make(chan chan string),
		defunctClients: make(chan chan string),
		messages:       make(chan string, 10),
	}
}

func (b *SSEHandler) HandleEvents() {
	go func() {
		for {
			select {
			case s := <-b.newClients:
				b.clients[s] = true
			case s := <-b.defunctClients:
				delete(b.clients, s)
				close(s)
			case msg := <-b.messages:
				for s := range b.clients {
					s <- msg
				}
			}
		}
	}()
}

func (b *SSEHandler) SendString(msg string) {
	b.messages <- msg
}

func (b *SSEHandler) SendJSON(obj interface{}) {
	tmp, err := json.Marshal(obj)
	if err != nil {
		log.Panic("Error while sending JSON object:", err)
	}
	b.messages <- string(tmp)
}

func (b *SSEHandler) Subscribe(c *gin.Context) {
	w := c.Writer
	ctx := c.Request.Context()
	f, ok := w.(http.Flusher)
	if !ok {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("Streaming unsupported"))
		return
	}
	messageChan := make(chan string)
	b.newClients <- messageChan

	go func() {
		<-ctx.Done()
		b.defunctClients <- messageChan
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	done := true
	for done {
		delay := time.NewTimer(30 * time.Second)
		select {
		case msg, open := <-messageChan:
			if !delay.Stop() {
				<-delay.C
			}
			if !open {
				done = false
				// If our messageChan was closed, this means that
				// the client has disconnected.
				break
			}
			fmt.Fprintf(w, "event: message\n")
			fmt.Fprintf(w, "data: %s\n\n", msg)
		case <-delay.C:
			fmt.Fprintf(w, ": nothing to sent\n\n")
		}

		// Flush the response. This is only possible if the response
		// supports streaming.
		f.Flush()
	}

	c.AbortWithStatus(http.StatusOK)
}
