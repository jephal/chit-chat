package shared

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"unicode/utf8"
)

var (
	sharedLogger *log.Logger
	logFile      *os.File
	logMutex     sync.Mutex
)

// InitializeSharedLogging sets up a shared log file for both client and server
func InitializeSharedLogging() error {
	logMutex.Lock()
	defer logMutex.Unlock()

	if sharedLogger != nil {
		return nil // Already initialized
	}

	var err error
	// Use absolute path to create shared log file in the project root
	logFile, err = os.OpenFile("/home/jeppe/itu/ds/chit-chat/chitchat.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return fmt.Errorf("failed to open shared log file: %v", err)
	}

	// Write only to file, not console
	sharedLogger = log.New(logFile, "", log.LstdFlags|log.Lmicroseconds)

	return nil
}

// CloseSharedLogging closes the shared log file
func CloseSharedLogging() {
	logMutex.Lock()
	defer logMutex.Unlock()

	if logFile != nil {
		logFile.Close()
		logFile = nil
		sharedLogger = nil
	}
}

// LogEvent logs an event using the shared logger
func LogEvent(component, eventType, clientID, message string, additionalData ...interface{}) {
	logMutex.Lock()
	defer logMutex.Unlock()

	if sharedLogger == nil {
		return // Logger not initialized
	}

	logMsg := fmt.Sprintf("%s | %s | ClientID: %s | %s",
		component, eventType, clientID, message)

	if len(additionalData) > 0 {
		logMsg += fmt.Sprintf(" | Data: %v", additionalData)
	}

	sharedLogger.Println(logMsg)
}

// Max returns the maximum of two int64 values
func Max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// ValidateMessage validates chat message according to S3 requirements
func ValidateMessage(content string) error {
	// Check if string is valid UTF-8
	if !utf8.ValidString(content) {
		return fmt.Errorf("message contains invalid UTF-8 characters")
	}

	// Check maximum length of 128 characters
	if len([]rune(content)) > 128 {
		return fmt.Errorf("message exceeds maximum length of 128 characters (current: %d)", len([]rune(content)))
	}

	// Check for empty messages
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("message cannot be empty")
	}

	return nil
}
