package main

import (
	"bufio"
	chitchat "chit-chat/grpc"
	"chit-chat/shared"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type client struct {
	participantName string
	grpcClient      chitchat.ChitChatServiceClient
	ctx             context.Context
	logicalClock    int64
	mu              sync.Mutex
}

func (c *client) validateMessage(content string) error {
	return shared.ValidateMessage(content)
}

func setupLogging() {
	if err := shared.InitializeSharedLogging(); err != nil {
		log.Fatalf("Failed to initialize shared logging: %v", err)
	}
}

func (c *client) logEvent(eventType string, message string, additionalData ...interface{}) {
	shared.LogEvent("CLIENT", eventType, c.participantName, message, additionalData...)
}

func (c *client) updateLogicalClock(receivedTime int64) {
	c.mu.Lock()
	c.logicalClock = shared.Max(c.logicalClock, receivedTime) + 1
	c.mu.Unlock()
}

func (c *client) getNextTimestamp() int64 {
	c.mu.Lock()
	c.logicalClock++
	timestamp := c.logicalClock
	c.mu.Unlock()
	return timestamp
}

func connectToServer() (chitchat.ChitChatServiceClient, *grpc.ClientConn) {
	conn, err := grpc.NewClient("localhost:3000", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}

	client := chitchat.NewChitChatServiceClient(conn)
	return client, conn
}

// Listen for incoming broadcast messages - uses goroutine as required
func (c *client) listenForMessages(stream chitchat.ChitChatService_JoinChatClient) {
	for {
		msg, err := stream.Recv()
		if err != nil {
			return
		}

		c.updateLogicalClock(msg.LogicalTimestamp)

		c.logEvent("MESSAGE_RECEIVED", fmt.Sprintf("From %s at logical time %d: %s",
			msg.SenderName, msg.LogicalTimestamp, msg.Content))
		fmt.Printf("[%d] %s: %s\n", msg.LogicalTimestamp, msg.SenderName, msg.Content)
	}
}

func (c *client) handleQuit() {
	timestamp := c.getNextTimestamp()
	leaveReq := &chitchat.LeaveRequest{
		ParticipantName:  c.participantName,
		LogicalTimestamp: timestamp,
	}

	_, err := c.grpcClient.LeaveChat(c.ctx, leaveReq)
	if err != nil {
		c.logEvent("ERROR", fmt.Sprintf("Error leaving chat: %v", err))
	}

	c.logEvent("DISCONNECTION", "Leaving chat")
	fmt.Printf("[%d] You left the chat\n", timestamp)
	os.Exit(0)
}

// Read user input and send messages
func (c *client) handleUserInput() {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Type your messages (or /quit to leave):")

	for {
		fmt.Print("> ")
		input, err := reader.ReadString('\n')
		if err != nil {
			c.logEvent("ERROR", fmt.Sprintf("Error reading input: %v", err))
			continue
		}

		message := strings.TrimSpace(input)
		if message == "" {
			continue
		}

		if message == "/quit" {
			c.handleQuit()
		}

		// Validate message before sending
		if err := c.validateMessage(message); err != nil {
			fmt.Printf("Invalid message: %v\n", err)
			c.logEvent("VALIDATION_ERROR", fmt.Sprintf("Invalid message rejected: %v", err))
			continue
		}

		// Send regular message
		timestamp := c.getNextTimestamp()
		sendReq := &chitchat.SendMessageRequest{
			ParticipantName:  c.participantName,
			Content:          message,
			LogicalTimestamp: timestamp,
		}

		_, err = c.grpcClient.SendMessage(c.ctx, sendReq)
		if err != nil {
			c.logEvent("ERROR", fmt.Sprintf("Error sending message: %v", err))
		} else {
			// Only log the sent message, don't display it locally
			c.logEvent("MESSAGE_SENT", fmt.Sprintf("Sent message at logical time %d: %s", timestamp, message))
		}
	}
}

func main() {
	// Get participant name from command line arguments
	if len(os.Args) < 2 {
		log.Fatal("Usage: go run main.go <participant_name>")
	}
	participantName := os.Args[1]

	setupLogging()
	defer shared.CloseSharedLogging()

	grpcClient, conn := connectToServer()
	defer conn.Close()

	ctx := context.Background()

	// Create client instance
	client := &client{
		participantName: participantName,
		grpcClient:      grpcClient,
		ctx:             ctx,
	}

	client.logEvent("CONNECTION", "Joining chat")

	// Join the chat
	timestamp := client.getNextTimestamp()
	joinReq := &chitchat.JoinRequest{
		ParticipantName:  participantName,
		LogicalTimestamp: timestamp,
	}

	stream, err := grpcClient.JoinChat(ctx, joinReq)
	if err != nil {
		log.Fatalf("Failed to join chat: %v", err)
	}

	client.logEvent("CONNECTION", "Successfully joined chat")

	// Start goroutine to listen for incoming messages (requirement satisfied)
	go client.listenForMessages(stream)

	// Handle user input in main thread
	client.handleUserInput()
}
