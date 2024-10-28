// project: gochatsrv
// file: server.go
//
// # server globals, definition and management functions except user management
//
// Date: 2024-10-28
// Author: Lutz Mueller <lmuellerhome@gmail.com>
// License: proprietary. All rights reserved.
//
// Version: v0.1.0
package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	serverPort       = 8080
	maxLoginAttempts = 3
	loginTimeout     = 10 * time.Second

	ErrPrivilege           = "this is a system command, must be admin user to execute"
	ErrIllegalNickname     = "illegal nickname"
	ErrInvalidShutdownTime = "invalid shutdown time, specify <#seconds>"
)

type ServerCommand struct {
	Command string
	User    *User
	Args    []string
}

type Server struct {
	listener        net.Listener
	userManager     *UserManager
	commands        chan ServerCommand
	shutdownOngoing bool
}

func (s *Server) handleUserInput(user *User, reader *bufio.Reader) {
	for {
		msg, err := reader.ReadString('\n')
		if err != nil {
			// Log the error and remove the user if the read operation fails
			logEvent(fmt.Sprintf("Error reading input from user %s: %v", user.nickname, err))
			s.userManager.removeUser(user.nickname)
			return
		}

		msg = strings.TrimSpace(msg) // Trim whitespace here
		if msg == "" {
			continue
		}

		if msg[0] == '/' { // Command handling
			cmd := s.parseCommand(user, msg) // Use parseCommand
			s.commands <- cmd
		} else {
			// Broadcast the message to all users
			s.userManager.broadcastMessage(fmt.Sprintf("%s: %s", user.nickname, msg))
		}
	}
}

func (s *Server) handleWhoAmI(user *User) {
	if user.privilege == 1 {
		_ = s.userManager.sendMessageToUser(user, fmt.Sprintf("You are: %s (admin) (%s)\n", user.nickname, user.conn.RemoteAddr().String()))
	} else {
		_ = s.userManager.sendMessageToUser(user, fmt.Sprintf("You are: %s (user)\n", user.nickname))
	}
}

func (s *Server) terminateServer(seconds int) {
	if seconds > 0 {
		msg := fmt.Sprintf("Server shutdown has been initiated; server will shut down in %d minutes, %d seconds.", seconds/60, seconds%60)
		log.Println(msg)
		s.userManager.broadcastMessage(msg)

		// Start the warning goroutine
		go countdownWarnings(s.userManager, seconds)

		// This goroutine will handle the actual shutdown after the countdown
		go func() {
			time.Sleep(time.Duration(seconds) * time.Second)
			s.terminateServerNow()
		}()
	} else {
		// If no countdown is needed, shut down immediately
		s.terminateServerNow()
	}
}

func (s *Server) terminateServerNow() {
	// Notify users that the server is shutting down immediately
	s.shutdownOngoing = true
	msg := "System Notice: Server is shutting down NOW. Please reconnect later."
	s.userManager.broadcastMessage(msg)
	logEvent(msg)

	// Close the listener
	if err := s.listener.Close(); err != nil {
		logEvent(fmt.Sprintf("Error closing listener: %v", err))
	}
	close(s.commands)

	// Gracefully close all user connections
	s.userManager.mu.Lock()
	defer s.userManager.mu.Unlock()

	var wg sync.WaitGroup
	for _, user := range s.userManager.users {
		wg.Add(1)
		go func(u *User) {
			defer wg.Done()
			if tcpConn, ok := u.conn.(*net.TCPConn); ok {
				err := tcpConn.SetLinger(0)
				if err != nil {
					logEvent(fmt.Sprintf("Error setting linger on connection for user %s: %v", u.nickname, err))
					return
				}
			}
			err := u.conn.Close()
			if err != nil {
				logEvent(fmt.Sprintf("Error closing connection for user %s: %v", u.nickname, err))
				return
			}
		}(user)
	}

	select {
	case <-time.After(5 * time.Second):
		logEvent("Timeout waiting for connections to close, forcing shutdown.")
	case <-time.After(s.waitForConnectionsClosed(&wg)):
		logEvent("All connections closed gracefully.")
	}
}

func (s *Server) waitForConnectionsClosed(wg *sync.WaitGroup) time.Duration {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return 0 // No need to wait
	case <-time.After(1 * time.Millisecond):
		return 5 * time.Second // Wait up to 5 seconds if not done instantly
	}
}

func (s *Server) parseCommand(user *User, input string) ServerCommand {
	input = strings.TrimSpace(input) // No need to re-trim since it's already done in handleUserInput

	// Remove the leading '/' to get the command
	cmdStr := strings.TrimPrefix(input, "/")

	// Split the input into command and arguments
	parts := strings.Fields(cmdStr) // Directly use Fields to split into words
	if len(parts) == 0 {
		return ServerCommand{User: user} // Return empty command if input was just '/'
	}

	return ServerCommand{
		Command: parts[0],
		User:    user,
		Args:    parts[1:],
	}
}

func (s *Server) Run() {
	var err error
	s.listener, err = net.Listen("tcp", fmt.Sprintf(":%d", serverPort))
	if err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
	defer func(listener net.Listener) {
		err := listener.Close()
		if err != nil {
			log.Printf("Error closing listener: %v", err)
		}
	}(s.listener)

	s.commands = make(chan ServerCommand)
	s.userManager = &UserManager{
		users: make(map[string]*User),
	}

	// Start up goroutines for command handling
	go s.commandDispatcher()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		// Here we manually send a shutdown command with a 10-second delay
		s.commands <- ServerCommand{
			Command: "shutdown",
			User:    nil, // No user associated with this command
			Args:    []string{"10"},
		}
	}()

	logEvent("Server running on :8080")

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if err.Error() == "use of closed network connection" {
				logEvent(fmt.Sprintf("Server stopped accepting new connections."))
				return
			}
			if !s.shutdownOngoing {
				logEvent(fmt.Sprintf("Error accepting connection: %v", err))
			}
			return
		}

		go s.handleNewClient(conn)
	}
}

func (s *Server) commandDispatcher() {
	for cmd := range s.commands {
		switch cmd.Command {
		case "whoami":
			s.handleWhoAmI(cmd.User)
		case "kick":
			if cmd.User.privilege == 0 {
				_ = s.userManager.sendMessageToUser(cmd.User, ErrPrivilege)
			} else {
				if len(cmd.Args) == 1 {
					err := s.userManager.handleKick(cmd.Args[0], "", cmd.User)
					if err != nil {
						logEvent(fmt.Sprintf("Error in kick command: %v", err))
						_ = s.userManager.sendMessageToUser(cmd.User, "Error executing kick command. "+err.Error())
					}
				} else {
					msg := strings.Join(cmd.Args[1:], " ")
					err := s.userManager.handleKick(cmd.Args[0], msg, cmd.User)
					if err != nil {
						logEvent(fmt.Sprintf("Error in kick command: %v", err))
						_ = s.userManager.sendMessageToUser(cmd.User, "Error executing kick command. "+err.Error())
					}
				}
			}
		case "msg", "w", "whisper":
			if len(cmd.Args) >= 2 {
				targetNickname := cmd.Args[0]
				msg := strings.Join(cmd.Args[1:], " ")
				if strings.Trim(msg, " ") == "" {
					_ = s.userManager.sendMessageToUser(cmd.User, "Message what?")
				} else {
					s.userManager.handlePrivateMessage(cmd.User, targetNickname, msg)
				}
			} else {
				_ = s.userManager.sendMessageToUser(cmd.User, "Invalid message format. Use: /msg <nickname> <message>")
			}
		case "bye", "logout":
			s.userManager.handleUserLogout(cmd.User)
		case "who":
			s.userManager.handleWho(cmd.User)
		case "nick":
			if len(cmd.Args) > 0 {
				newNickname := cmd.Args[0]
				s.userManager.handleNewNick(cmd.User, newNickname)
			} else {
				_ = s.userManager.sendMessageToUser(cmd.User, "Please provide a new nickname. Use: /nick <newNickname>")
			}
		case "shutdown":
			var cts int = 0
			var err error
			if len(cmd.Args) > 0 {
				cts, err = strconv.Atoi(cmd.Args[0])
				if err != nil || cts < 0 {
					_ = s.userManager.sendMessageToUser(cmd.User, ErrInvalidShutdownTime)
					continue
				}
				s.terminateServer(cts)
			}
			return // Exit commandDispatcher to stop processing commands
		default:
			logEvent(fmt.Sprintf("Unknown command received from user %s: %s", cmd.User.nickname, cmd.Command))
			_ = s.userManager.sendMessageToUser(cmd.User, "Unknown command.")
		}
	}
}

func (s *Server) handleNewClient(conn net.Conn) {
	defer func() {
		err := conn.Close()
		if err != nil {
			logEvent(fmt.Sprintf("Error closing connection: %v", err))
		}
	}()

	reader := bufio.NewReader(conn)
	timeoutChan := time.After(loginTimeout)

	for attempts := 0; attempts < maxLoginAttempts; attempts++ {
		resetTimedLoginChannels()
		select {
		case <-timeoutChan:
			msg := "Login period exceeded, connection closed."
			logEvent(msg)
			_ = sendMessageToConn(conn, msg)
			return
		default:
			nickname, err := queryNicknameWithTimeout(reader, timeoutChan)
			if err != nil {
				if err.Error() == "timeout" {
					msg := "Login period exceeded, connection closed."
					logEvent(msg)
					_ = sendMessageToConn(conn, msg)
					return
				}
				logEvent(fmt.Sprintf("Error querying nickname: %v", err))
				return
			}

			// Check if the nickname already exists, case-insensitive
			if existingUser := s.userManager.FindUser(nickname); existingUser != nil {
				if sendErr := sendMessageToConn(conn, "Nickname already in use. Please try another."); sendErr != nil {
					logEvent(fmt.Sprintf("Error sending message to client: %v", sendErr))
					return
				}
				continue // Try again
			}

			user := &User{
				conn:      conn,
				nickname:  nickname,
				privilege: 0,
			}

			if s.userManager.addUser(user) {
				// Success, handle user setup here
				if strings.EqualFold(nickname, "admin") {
					ip, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
					userIP := net.ParseIP(ip)
					if userIP != nil && isLocalIP(userIP) {
						s.userManager.assignPrivilege(nickname, 1)
					}
				}
				s.userManager.broadcastMessage(fmt.Sprintf("%s has joined the chat", nickname))
				s.handleUserInput(user, reader)
				return
			} else {
				// This should not happen given the above check, but included for completeness
				if sendErr := sendMessageToConn(conn, "Nickname already in use. Please try another."); sendErr != nil {
					logEvent(fmt.Sprintf("Error sending message to client: %v", sendErr))
					return
				}
			}
		}
	}

	// If we reach here, user failed to choose a valid nickname
	msg := "Login attempts exhausted, closing connection."
	logEvent(msg)
	_ = sendMessageToConn(conn, msg)
}
