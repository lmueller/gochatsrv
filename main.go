/* Simple chat server program written in golang. */
package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	serverPort       = 8080
	maxLoginAttempts = 3
	loginTimeout     = 10 * time.Second

	ErrPrivilege       = "This is a system command, must be admin user to execute"
	ErrIllegalNickname = "Illegal nickname"
)

var (
	localIPRANGES = []string{
		"127.0.0.1/24",
		"192.168.0.0/16",
		"10.0.0.0/8",
		"::1/128",
		"fc00::/7",  // Unique local addresses (ULA) for IPv6
		"fe80::/10", // Link-local addresses for IPv6
	}
)

type User struct {
	conn      net.Conn
	nickname  string
	privilege int // 0 for regular users, 1 for admin
}

type UserManager struct {
	users map[string]*User
	mu    sync.Mutex
}

type Message struct {
	Sender  string
	Target  string
	Content string
}

type ServerCommand struct {
	Command string
	User    *User
	Args    []string
}

// Channels for communicating results back to the main function
var timedLoginNickChan = make(chan string, 1)
var timedLoginErrorChan = make(chan error, 1)

func logEvent(event string) {
	log.Println(event)
}

func sendMessageToConn(conn net.Conn, msgs ...string) error {
	w := bufio.NewWriter(conn)
	for _, msg := range msgs {
		if _, err := fmt.Fprintln(w, msg); err != nil {
			return err
		}
	}
	return w.Flush()
}

func (um *UserManager) unsafeSendMessageToUser(user *User, msgs ...string) error {
	err := sendMessageToConn(user.conn, msgs...)
	if err != nil {
		count := len(msgs)
		logEvent(fmt.Sprintf("Error sending message to user %s: %v. %d messages not sent.", user.nickname, err, count))
		// Consider removing the user if the connection is broken
		delete(um.users, user.nickname)
		return err // Return the error
	}
	return nil // All messages sent successfully
}

func (um *UserManager) sendMessageToUser(user *User, msgs ...string) error {
	um.mu.Lock()
	defer um.mu.Unlock()
	return um.unsafeSendMessageToUser(user, msgs...)
}

func (um *UserManager) broadcastMessage(msgs ...string) {
	um.mu.Lock()
	defer um.mu.Unlock()
	for _, user := range um.users {
		_ = um.unsafeSendMessageToUser(user, msgs...)
	}
}

func (um *UserManager) broadcastMessageExcept(excludedUserNickname string, msgs ...string) {
	um.mu.Lock()
	defer um.mu.Unlock()
	for _, user := range um.users {
		if strings.EqualFold(user.nickname, excludedUserNickname) {
			continue
		}
		_ = um.unsafeSendMessageToUser(user, msgs...)
	}
}

func (um *UserManager) addUser(user *User) bool {
	um.mu.Lock()
	defer um.mu.Unlock()
	if _, exists := um.users[user.nickname]; exists {
		return false
	}
	um.users[user.nickname] = user
	return true
}

func (um *UserManager) removeUser(nickname string) {
	um.mu.Lock()
	defer um.mu.Unlock()
	delete(um.users, nickname)
}

func (um *UserManager) FindUser(nickname string) *User {
	um.mu.Lock()
	defer um.mu.Unlock()

	for _, user := range um.users {
		if strings.EqualFold(user.nickname, nickname) {
			return user
		}
	}
	return nil
}

// AssignPrivilege assigns a new privilege level to a user by nickname.
// The search for the user is case-insensitive. Returns the user pointer if the user was found
// and the privilege was changed, otherwise nil.
func (um *UserManager) assignPrivilege(nickname string, newPrivilege int) *User {
	um.mu.Lock()
	defer um.mu.Unlock()

	// Case-insensitive search for the user
	for key, user := range um.users {
		if strings.EqualFold(key, nickname) {
			if user.privilege != newPrivilege {
				user.privilege = newPrivilege

				// Log the privilege change for auditing purposes
				logEvent(fmt.Sprintf("User privilege changed: %s to %d", user.nickname, newPrivilege))

				return user // Return the updated user
			}
			return user // User found but privilege already matches newPrivilege
		}
	}

	return nil // User not found
}

func isLocalIP(ip net.IP) bool {
	for _, network := range localIPRANGES {
		_, ipnetw, err := net.ParseCIDR(network)
		if err != nil {
			continue // Skip invalid CIDR
		}
		if ipnetw.Contains(ip) {
			return true
		}
	}
	return false
}

func validateNickname(nickname string) error {
	nicknameRegex := regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{2,19}$`)
	if !nicknameRegex.MatchString(nickname) {
		return errors.New(ErrIllegalNickname)
	}
	return nil
}

// Reset channels for the next attempt
func resetChannels() {
	for len(timedLoginNickChan) > 0 {
		<-timedLoginNickChan
	}
	for len(timedLoginErrorChan) > 0 {
		<-timedLoginErrorChan
	}
}

func queryNicknameWithTimeout(reader *bufio.Reader, timeoutChan <-chan time.Time) (string, error) {
	// Initiate a non-blocking read to check for input
	go func() {
		line, err := reader.ReadString('\n')
		if err == nil {
			// If we read a complete line, send it back to the main function
			select {
			case <-timeoutChan:
				// If timeout has already occurred, do nothing
			default:
				// Send the nickname back to the caller
				nick := strings.TrimRight(line, " \r\n")
				timedLoginNickChan <- nick
			}
		} else {
			// Handle errors from reading
			timedLoginErrorChan <- err
		}
	}()

	select {
	case nickname := <-timedLoginNickChan:
		nickname = sanitizeNickname(nickname)
		if err := validateNickname(nickname); err != nil {
			return "", err
		}
		return nickname, nil
	case err := <-timedLoginErrorChan:
		return "", err
	case <-timeoutChan:
		return "", fmt.Errorf("Timeout")
	}
}

func sanitizeNickname(nickname string) string {
	// Remove unwanted characters
	nickname = strings.ReplaceAll(nickname, " ", "")
	nickname = strings.ReplaceAll(nickname, `\`, "")
	return strings.TrimRight(nickname, "\r\n")
}

func queryNickname(conn net.Conn, reader *bufio.Reader) (string, error) {
	// non-timed version, may be outdated
	for {
		// Send prompt to the client
		if err := sendMessageToConn(conn, "Please enter your nickname:"); err != nil {
			return "", err
		}
		// Read the nickname from the client
		nickname, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		nickname = sanitizeNickname(nickname)

		// Validate the nickname
		if err := validateNickname(nickname); err != nil {
			if sendErr := sendMessageToConn(conn, "Illegal nickname, refer to naming rules."); sendErr != nil {
				logEvent(fmt.Sprintf("Error sending message to client: %v", sendErr))
			}
			continue
		}

		return nickname, nil
	}
}

func (um *UserManager) generateUserList(requestingUser *User) string {
	um.mu.Lock()
	defer um.mu.Unlock()

	var userList []string
	for _, user := range um.users {
		privilege := "user"
		if user.privilege == 1 {
			privilege = "admin"
		}
		if requestingUser.privilege == 1 {
			// Admins see the IP addresses of users
			userList = append(userList, fmt.Sprintf("%s (%s) (%s)", user.nickname, privilege, user.conn.RemoteAddr().String()))
		} else {
			// Non-admins see only nickname and privilege
			userList = append(userList, fmt.Sprintf("%s (%s)", user.nickname, privilege))
		}
	}

	// Sort the list, with admins appearing first
	sort.Slice(userList, func(i, j int) bool {
		if strings.Contains(userList[i], "admin") && !strings.Contains(userList[j], "admin") {
			return true
		}
		return userList[i] < userList[j]
	})

	return strings.Join(userList, "\n") + "\n(end of list)\n"
}

func handlePrivateMessage(sender *User, targetNickname, message string, userManager *UserManager) {
	targetUser := userManager.FindUser(targetNickname)
	if targetUser == nil {
		logEvent(fmt.Sprintf("Private message failed: user %s not found", targetNickname))
		_ = userManager.sendMessageToUser(sender, "User not found.")
		return
	}

	if sender == targetUser {
		// Case when sender messages themselves
		_ = userManager.sendMessageToUser(sender, fmt.Sprintf("You message yourself: %s", message))
	} else {
		// Case when sender messages another user
		_ = userManager.sendMessageToUser(sender, fmt.Sprintf("You message %s: %s", targetUser.nickname, message))
		_ = userManager.sendMessageToUser(targetUser, fmt.Sprintf("%s messages: %s", sender.nickname, message))
	}
}

func handleUserLogout(user *User, userManager *UserManager) {
	// Log the user quitting
	logEvent(fmt.Sprintf("User %s has quit", user.nickname))

	// Broadcast the user's departure
	userManager.broadcastMessage(fmt.Sprintf("%s has left the chat", user.nickname))

	// Close the user's connection
	if err := user.conn.Close(); err != nil {
		logEvent(fmt.Sprintf("Error closing connection for user %s: %v", user.nickname, err))
	}

	// Remove the user from the userManager
	userManager.removeUser(user.nickname)
}

func handleKick(targetNickname, kickMessage string, admin *User, userManager *UserManager) {
	if strings.EqualFold(targetNickname, admin.nickname) {
		// Check if the admin is trying to kick themselves
		logEvent(fmt.Sprintf("%s tried to kick themselves", admin.nickname))
		_ = userManager.sendMessageToUser(admin, "You cannot kick yourself (on this server)")
		return
	}
	// Find the target user
	targetUser := userManager.FindUser(targetNickname)
	if targetUser == nil {
		logEvent(fmt.Sprintf("Kick attempt failed: user %s not found", targetNickname))
		_ = userManager.sendMessageToUser(admin, "User not found.")
		return
	}
	nomsg := strings.Trim(kickMessage, " ") == ""
	// Notify the kicked user
	if nomsg {
		if err := userManager.sendMessageToUser(targetUser, fmt.Sprintf("You have been kicked by %s.", admin.nickname)); err != nil {
			logEvent(fmt.Sprintf("Error sending message to user %s: %v", admin.nickname, err))
		}
	} else {
		if err := userManager.sendMessageToUser(targetUser, fmt.Sprintf("You have been kicked by %s: %s", admin.nickname, kickMessage)); err != nil {
			logEvent(fmt.Sprintf("Error sending message to user %s: %v", admin.nickname, err))
		}
	}

	// Close the connection
	err := targetUser.conn.Close()
	if err != nil {
		logEvent(fmt.Sprintf("Error closing connection for kicked user %s: %v", targetUser.nickname, err))
	}

	// Remove the user from the user manager
	userManager.removeUser(targetUser.nickname)

	// Log and notify others
	var broadcastMessage string
	if nomsg {
		broadcastMessage = fmt.Sprintf("%s has been kicked.", targetUser.nickname)
	} else {
		broadcastMessage = fmt.Sprintf("%s has been kicked by %s: %s", targetUser.nickname, admin.nickname, kickMessage)
	}
	logEvent(broadcastMessage)
	userManager.broadcastMessageExcept(targetUser.nickname, broadcastMessage)

	// After kicking the user, inform the admin
	if err := userManager.sendMessageToUser(admin, fmt.Sprintf("User %s has been kicked.", targetUser.nickname)); err != nil {
		logEvent(fmt.Sprintf("Error sending kick confirmation to admin %s: %v", admin.nickname, err))
	}
}

func parseCommand(user *User, input string) ServerCommand {
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

func handleMessages(messages <-chan Message, userManager *UserManager) {
	for message := range messages {
		// Find the target user
		targetUser := userManager.FindUser(message.Target)
		if targetUser == nil {
			logEvent(fmt.Sprintf("Failed to deliver message: user %s not found", message.Target))
			continue
		}

		// Send the message to the target user
		_ = userManager.sendMessageToUser(targetUser, fmt.Sprintf("%s: %s", message.Sender, message.Content))
	}
}

func terminateServer(ln net.Listener, userManager *UserManager, commands chan<- ServerCommand) {
	// Close the listener to prevent new connections
	err := ln.Close()
	if err != nil {
		log.Fatalf("Error terminating server: %v", err)
	}

	// Signal to stop processing commands by closing the channel
	close(commands)

	// Lock the UserManager to ensure thread safety
	userManager.mu.Lock()
	defer userManager.mu.Unlock()

	// Close all user connections
	for _, user := range userManager.users {
		if err := user.conn.Close(); err != nil {
			logEvent(fmt.Sprintf("Error closing connection for user %s: %v", user.nickname, err))
		}
	}

	// Log server termination
	logEvent("Server terminated. Closing all connections.")
}

func handleWhoAmI(user *User, um *UserManager) {
	if user.privilege == 1 {
		_ = um.sendMessageToUser(user, fmt.Sprintf("You are: %s (admin) (%s)\n", user.nickname, user.conn.RemoteAddr().String()))
	} else {
		_ = um.sendMessageToUser(user, fmt.Sprintf("You are: %s (user)\n", user.nickname))
	}
}

func handleWho(user *User, um *UserManager) {
	if user.privilege == 2 {
		// Admin user branch
		_ = um.sendMessageToUser(user, "Admin user list will be displayed here.")
	} else {
		// Regular user branch
		// Get the list of users, formatted for regular users
		userList := um.generateUserList(user)
		// Send the user list to the user
		_ = um.sendMessageToUser(user, "Current Users:")
		_ = um.sendMessageToUser(user, userList)
	}
}

func handleNewNick(user *User, newNickname string, userManager *UserManager) {
	// Validate the new nickname
	if err := validateNickname(newNickname); err != nil {
		_ = userManager.sendMessageToUser(user, err.Error())
		return
	}

	// Check if the new nickname is already in use
	if existingUser := userManager.FindUser(newNickname); existingUser != nil {
		_ = userManager.sendMessageToUser(user, "That nickname is already in use. Please choose another.")
		return
	}

	// Log the nickname change
	logEvent(fmt.Sprintf("User %s changed nickname to %s", user.nickname, newNickname))

	// Remove the old user
	userManager.removeUser(user.nickname)

	// Update the user's nickname
	user.nickname = newNickname

	// Add the user back with the new nickname
	if !userManager.addUser(user) {
		// If somehow we couldn't add the user back, this shouldn't happen given the checks,
		// but we'll handle it gracefully
		_ = userManager.sendMessageToUser(user, "Failed to update nickname. Please try again.")
		return
	}

	// Notify everyone about the change
	userManager.broadcastMessage(fmt.Sprintf("%s is now: %s", user.nickname, newNickname))
}

func commandDispatcher(userManager *UserManager, commands <-chan ServerCommand) {
	for cmd := range commands {
		switch cmd.Command {
		case "whoami":
			handleWhoAmI(cmd.User, userManager)
			continue
		case "kick":
			if cmd.User.privilege == 0 {
				_ = userManager.sendMessageToUser(cmd.User, ErrPrivilege)
			} else {
				if len(cmd.Args) == 1 {
					handleKick(cmd.Args[0], "", cmd.User, userManager)
				} else {
					msg := strings.Join(cmd.Args[1:], " ")
					handleKick(cmd.Args[0], msg, cmd.User, userManager)
				}
			}
			continue
		case "msg", "w", "whisper":
			if len(cmd.Args) >= 2 {
				targetNickname := cmd.Args[0]
				msg := strings.Join(cmd.Args[1:], " ")
				if strings.Trim(msg, " ") == "" {
					_ = userManager.sendMessageToUser(cmd.User, "Message what?")
				} else {
					handlePrivateMessage(cmd.User, targetNickname, msg, userManager)
				}
			} else {
				_ = userManager.sendMessageToUser(cmd.User, "Invalid message format. Use: /msg <nickname> <message>")
			}
			continue
		case "bye", "logout":
			handleUserLogout(cmd.User, userManager)
			continue
		case "who":
			handleWho(cmd.User, userManager)
			continue
		case "nick":
			if len(cmd.Args) > 0 {
				newNickname := cmd.Args[0]
				handleNewNick(cmd.User, newNickname, userManager)
			} else {
				_ = userManager.sendMessageToUser(cmd.User, "Please provide a new nickname. Use: /nick <newNickname>")
			}
			continue
		default:
			logEvent(fmt.Sprintf("Unknown command received from user %s: %s", cmd.User.nickname, cmd.Command))
			_ = userManager.sendMessageToUser(cmd.User, "Unknown command.")
		}
	}
}

func handleUserInput(user *User, reader *bufio.Reader, commands chan<- ServerCommand, userManager *UserManager) {
	for {
		msg, err := reader.ReadString('\n')
		if err != nil {
			// Log the error and remove the user if the read operation fails
			logEvent(fmt.Sprintf("Error reading input from user %s: %v", user.nickname, err))
			userManager.removeUser(user.nickname)
			return
		}

		msg = strings.TrimSpace(msg) // Trim whitespace here
		if msg == "" {
			continue
		}

		if msg[0] == '/' { // Command handling
			cmd := parseCommand(user, msg) // Use parseCommand
			commands <- cmd
		} else {
			// Broadcast the message to all users
			userManager.broadcastMessage(fmt.Sprintf("%s: %s", user.nickname, msg))
		}
	}
}

func handleNewClient(conn net.Conn, userManager *UserManager, commands chan<- ServerCommand) {
	defer func() {
		err := conn.Close()
		if err != nil {
			logEvent(fmt.Sprintf("Error closing connection: %v", err))
		}
	}()

	reader := bufio.NewReader(conn)
	timeoutChan := time.After(loginTimeout)

	for attempts := 0; attempts < maxLoginAttempts; attempts++ {
		resetChannels()
		select {
		case <-timeoutChan:
			msg := "Login period exceeded, connection closed."
			logEvent(msg)
			_ = sendMessageToConn(conn, msg)
			return
		default:
			nickname, err := queryNicknameWithTimeout(reader, timeoutChan)
			if err != nil {
				if err.Error() == "Timeout" {
					msg := "Login period exceeded, connection closed."
					logEvent(msg)
					_ = sendMessageToConn(conn, msg)
					return
				}
				logEvent(fmt.Sprintf("Error querying nickname: %v", err))
				return
			}

			// Check if the nickname already exists, case-insensitive
			if existingUser := userManager.FindUser(nickname); existingUser != nil {
				if sendErr := sendMessageToConn(conn, "Nickname already in use. Please try another."); sendErr != nil {
					logEvent(fmt.Sprintf("Error sending message to client: %v", sendErr))
					return
				}
				continue // Try again
			}

			user := &User{
				nickname:  nickname,
				conn:      conn,
				privilege: 0,
			}

			if userManager.addUser(user) {
				// Success, handle user setup here
				if strings.EqualFold(nickname, "admin") {
					ip, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
					userIP := net.ParseIP(ip)
					if userIP != nil && isLocalIP(userIP) {
						userManager.assignPrivilege(nickname, 1)
					}
				}
				userManager.broadcastMessage(fmt.Sprintf("%s has joined the chat", nickname))
				handleUserInput(user, reader, commands, userManager)
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

func main() {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", serverPort))
	if err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
	defer func() {
		err := listener.Close()
		if err != nil {
			logEvent(fmt.Sprintf("Error closing listener: %v", err))
		}
	}()
	userManager := &UserManager{
		users: make(map[string]*User),
	}
	commands := make(chan ServerCommand)
	messages := make(chan Message)

	// Start up goroutines for command and message handling
	go commandDispatcher(userManager, commands)
	go handleMessages(messages, userManager)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		terminateServer(listener, userManager, commands)
		os.Exit(0)
	}()

	logEvent("Server running on :8080")

	for {
		conn, err := listener.Accept()
		if err != nil {
			if err.Error() == "use of closed network connection" {
				logEvent(fmt.Sprintf("Server stopped accepting new connections."))
				return
			}
			logEvent(fmt.Sprintf("Error accepting connection: %v", err))
			continue
		}

		go handleNewClient(conn, userManager, commands)
	}
}

/*end of file*/
