package main

import (
	chitchat "chit-chat/grpc"
	"chit-chat/shared"
	"context"
	"fmt"
	"log"
	"net"
	"sync"

	"google.golang.org/grpc"
)

const (
	JOIN_OP       = 1
	LEAVE_OP      = 2
	MESSAGE_OP    = 3
	DISCONNECT_OP = 4
)

type Operation struct {
	Type            int
	ParticipantName string
	Content         string
	Timestamp       int64
	Stream          chitchat.ChitChatService_JoinChatServer
	ResponseChan    chan *chitchat.BroadcastMessage
}

type server struct {
	chitchat.UnimplementedChitChatServiceServer
	operationChan chan Operation
	logicalClock  int64
	activeClients map[string]chitchat.ChitChatService_JoinChatServer
	mu            sync.RWMutex
}

func (s *server) broadcastToAll(msg *chitchat.BroadcastMessage) {
	s.mu.RLock()

	for participantName, stream := range s.activeClients {

		go func() {

			err := stream.Send(msg)

			if err != nil {

				shared.LogEvent("SERVER", "ERROR", participantName, fmt.Sprintf("Failed to send message: %v", err))

				disconnectOp := Operation{
					Type:            DISCONNECT_OP,
					ParticipantName: participantName,
					ResponseChan:    make(chan *chitchat.BroadcastMessage),
				}
				s.operationChan <- disconnectOp
			}
		}()
	}
	s.mu.RUnlock()
}

func (s *server) JoinChat(req *chitchat.JoinRequest, stream chitchat.ChitChatService_JoinChatServer) error {
	joinOp := Operation{
		Type:            JOIN_OP,
		ParticipantName: req.ParticipantName,
		Stream:          stream,
		Timestamp:       req.LogicalTimestamp,
		ResponseChan:    make(chan *chitchat.BroadcastMessage, 1),
	}

	s.operationChan <- joinOp
	<-joinOp.ResponseChan

	<-stream.Context().Done()

	disconnectOp := Operation{
		Type:            DISCONNECT_OP,
		ParticipantName: req.ParticipantName,
		ResponseChan:    make(chan *chitchat.BroadcastMessage),
	}
	s.operationChan <- disconnectOp

	return nil
}

func (s *server) SendMessage(ctx context.Context, req *chitchat.SendMessageRequest) (*chitchat.BroadcastMessage, error) {
	msgOp := Operation{
		Type:            MESSAGE_OP,
		ParticipantName: req.ParticipantName,
		Content:         req.Content,
		Timestamp:       req.LogicalTimestamp,
		ResponseChan:    make(chan *chitchat.BroadcastMessage, 1),
	}

	s.operationChan <- msgOp
	response := <-msgOp.ResponseChan

	return response, nil
}

func (s *server) LeaveChat(ctx context.Context, req *chitchat.LeaveRequest) (*chitchat.BroadcastMessage, error) {
	leaveOp := Operation{
		Type:            LEAVE_OP,
		ParticipantName: req.ParticipantName,
		Timestamp:       req.LogicalTimestamp,
		ResponseChan:    make(chan *chitchat.BroadcastMessage, 1),
	}

	s.operationChan <- leaveOp
	response := <-leaveOp.ResponseChan

	return response, nil
}

func initializeAndStartServer() {
	lis, err := net.Listen("tcp", ":3000")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	chatServer := &server{
		operationChan: make(chan Operation, 10),
		activeClients: make(map[string]chitchat.ChitChatService_JoinChatServer),
	}

	go chatServer.coordinator()
	chitchat.RegisterChitChatServiceServer(grpcServer, chatServer)

	shared.LogEvent("SERVER", "STARTUP", "", "ChitChat server starting on port :50051")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

func (s *server) coordinator() {
	for op := range s.operationChan {

		switch op.Type {

		case JOIN_OP:
			s.mu.Lock()
			s.logicalClock = shared.Max(s.logicalClock, op.Timestamp) + 1
			currentTime := s.logicalClock
			s.activeClients[op.ParticipantName] = op.Stream
			clientCount := len(s.activeClients)
			s.mu.Unlock()

			msg := &chitchat.BroadcastMessage{
				SenderName:       op.ParticipantName,
				LogicalTimestamp: currentTime,
				Content:          fmt.Sprintf("Participant %s joined Chit Chat at logical time %d", op.ParticipantName, currentTime),
				MessageType:      chitchat.MessageType_JOIN,
			}

			s.broadcastToAll(msg)
			shared.LogEvent("SERVER", "CONNECTION", op.ParticipantName, fmt.Sprintf("Client connected at logical time %d", currentTime))
			shared.LogEvent("SERVER", "BROADCAST_JOIN", op.ParticipantName, fmt.Sprintf("Broadcasting join message to %d clients", clientCount))

			op.ResponseChan <- msg

		case MESSAGE_OP:
			s.mu.Lock()
			s.logicalClock = shared.Max(s.logicalClock, op.Timestamp) + 1
			currentTime := s.logicalClock
			clientCount := len(s.activeClients)
			s.mu.Unlock()

			msg := &chitchat.BroadcastMessage{
				SenderName:       op.ParticipantName,
				LogicalTimestamp: currentTime,
				Content:          op.Content,
				MessageType:      chitchat.MessageType_CHAT,
			}

			s.broadcastToAll(msg)
			shared.LogEvent("SERVER", "MESSAGE_RECEIVED", op.ParticipantName, fmt.Sprintf("Valid message at logical time %d: %s", currentTime, op.Content))
			shared.LogEvent("SERVER", "MESSAGE_DELIVERY", op.ParticipantName, fmt.Sprintf("Delivered message to %d clients at logical time %d", clientCount, currentTime))

			op.ResponseChan <- msg

		case LEAVE_OP:
			s.mu.Lock()
			s.logicalClock = shared.Max(s.logicalClock, op.Timestamp) + 1
			currentTime := s.logicalClock
			delete(s.activeClients, op.ParticipantName)
			s.mu.Unlock()

			msg := &chitchat.BroadcastMessage{
				SenderName:       op.ParticipantName,
				LogicalTimestamp: currentTime,
				Content:          fmt.Sprintf("Participant %s left Chit Chat at logical time %d", op.ParticipantName, currentTime),
				MessageType:      chitchat.MessageType_LEAVE,
			}

			s.broadcastToAll(msg)
			shared.LogEvent("SERVER", "DISCONNECTION", op.ParticipantName, fmt.Sprintf("Client explicitly left at logical time %d", currentTime))

			op.ResponseChan <- msg

		case DISCONNECT_OP:
			s.mu.Lock()
			delete(s.activeClients, op.ParticipantName)
			s.mu.Unlock()
			shared.LogEvent("SERVER", "DISCONNECTION", op.ParticipantName, "Client stream closed")
			close(op.ResponseChan)
		}
	}
}

func main() {
	err := shared.InitializeSharedLogging()
	if err != nil {
		log.Fatalf("Failed to initialize shared logging: %v", err)
	}
	defer shared.CloseSharedLogging()

	shared.LogEvent("SERVER", "STARTUP", "", "ChitChat server starting")

	initializeAndStartServer()
}
