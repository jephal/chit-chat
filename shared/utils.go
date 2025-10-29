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

func InitializeSharedLogging() error {
	logMutex.Lock()
	defer logMutex.Unlock()

	if sharedLogger != nil {
		return nil // Already initialized
	}

	var err error

	//create if doesnt exist, write only, append to end of file
	logFile, err = os.OpenFile("../chitchat.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return fmt.Errorf("failed to open shared log file: %v", err)
	}

	// Write only to file
	sharedLogger = log.New(logFile, "", log.LstdFlags|log.Lmicroseconds)

	return nil
}

func CloseSharedLogging() {
	logMutex.Lock()
	defer logMutex.Unlock()

	if logFile != nil {
		logFile.Close()
		logFile = nil
		sharedLogger = nil
	}
}

func LogEvent(component, eventType, clientID, message string) {
	logMutex.Lock()
	defer logMutex.Unlock()

	if sharedLogger == nil {
		return // Logger not initialized
	}

	logMsg := fmt.Sprintf("%s | %s | ClientID: %s | %s",
		component, eventType, clientID, message)

	sharedLogger.Println(logMsg)
}

func Max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func ValidateMessage(content string) error {

	if !utf8.ValidString(content) {
		return fmt.Errorf("message contains invalid UTF-8 characters")
	}

	if len([]rune(content)) > 128 {
		return fmt.Errorf("message exceeds maximum length of 128 characters (current: %d)", len([]rune(content)))
	}

	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("message cannot be empty")
	}

	return nil
}
