// project: gochatsrv
// file: usermgr.go
//
// # user management structures, communications, and UserManager object. Implementation of many
// # server commands called by the server command dispatcher function.
//
// Date: 2024-10-28
// Author: Lutz Mueller <lmuellerhome@gmail.com>
// License: proprietary. All rights reserved.
//
// Version: see github.com/lmueller/gochatsrv
package main

import (
	"errors"
	"fmt"
	"github.com/lmueller/termcolor"
	"golang.org/x/crypto/bcrypt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// User represents a user in the chat system.
type User struct {
	id          int      // Unique identifier for the user
	username    string   // persisted Username of the user
	privilege   int      // Privilege level (e.g., 0 for regular user, 1 for admin)
	nickname    string   // Existing field for the user's nickname
	conn        net.Conn // Connection object for the user
	lastMsgFrom string   // Existing field for tracking the last message sender
}

// UserManager manages the users in the chat server.
type UserManager struct {
	users map[string]*User
	mu    sync.Mutex
}

func (um *UserManager) handleWhoAmI(user *User) {
	if user.privilege == 1 {
		_ = um.sendMessageToUser(user, fmt.Sprintf("You are: %s (admin) (%s)\n", user.nickname, user.conn.RemoteAddr().String()))
	} else {
		_ = um.sendMessageToUser(user, fmt.Sprintf("You are: %s (user)\n", user.nickname))
	}
}

// FindUser looks up a user by their nickname.
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

// addUser adds a new user to the manager.
func (um *UserManager) addUser(user *User) bool {
	um.mu.Lock()
	defer um.mu.Unlock()
	if _, exists := um.users[user.nickname]; exists {
		return false
	}
	um.users[user.nickname] = user
	return true
}

// removeUser removes a user from the manager.
func (um *UserManager) removeUser(nickname string) {
	um.mu.Lock()
	defer um.mu.Unlock()
	delete(um.users, strings.ToLower(nickname))
}

// assignPrivilege changes the privilege level of a user.
func (um *UserManager) assignPrivilege(nickname string, newPrivilege int) *User {
	um.mu.Lock()
	defer um.mu.Unlock()

	// Case-insensitive search for the user
	for k, user := range um.users {
		if strings.EqualFold(k, nickname) {
			if user.privilege != newPrivilege {
				user.privilege = newPrivilege
				logEvent(fmt.Sprintf("User privilege changed: %s to %d", user.nickname, newPrivilege))
			}
			return user
		}
	}
	return nil
}

// generateUserList returns a string containing a formatted list of users.
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
			userList = append(userList, fmt.Sprintf("%s (%s) (%s)", user.nickname, privilege, user.conn.RemoteAddr().String()))
		} else {
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

// sendMessageToUser sends a message to a specific user.
func (um *UserManager) sendMessageToUser(user *User, msgs ...string) error {
	um.mu.Lock()
	defer um.mu.Unlock()
	return um.unsafeSendMessageToUser(user, msgs...)
}

// unsafeSendMessageToUser sends messages to a user without locking.
// This method should only be called within a locked context.
func (um *UserManager) unsafeSendMessageToUser(user *User, msgs ...string) error {
	err := sendMessageToConn(user.conn, msgs...)
	if err != nil {
		count := len(msgs)
		logEvent(fmt.Sprintf("Error sending message to user %s: %v. %d messages not sent.", user.nickname, err, count))
		delete(um.users, user.nickname)
		return err
	}
	return nil
}

func (um *UserManager) unsafeSendSysMessageToUser(user *User, msgs ...string) error {
	err := sendSysMessageToConn(user.conn, msgs...)
	if err != nil {
		count := len(msgs)
		logEvent(fmt.Sprintf("Error sending message to user %s: %v. %d messages not sent.", user.nickname, err, count))
		delete(um.users, user.nickname)
		return err
	}
	return nil
}

// broadcastMessage sends a message to all users.
func (um *UserManager) broadcastMessage(msgs ...string) {
	um.mu.Lock()
	defer um.mu.Unlock()
	for _, user := range um.users {
		_ = um.unsafeSendMessageToUser(user, msgs...)
	}
}

// broadcastMessage sends a message to all users.
func (um *UserManager) broadcastSysMessage(msgs ...string) {
	um.mu.Lock()
	defer um.mu.Unlock()
	for _, user := range um.users {
		_ = um.unsafeSendSysMessageToUser(user, msgs...)
	}
}

// broadcastMessageExcept sends a message to all users except the specified user.
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

// handlePrivateMessage handles the sending of a private message, compare /msg,/whisper or similar
func (um *UserManager) handlePrivateMessage(sender *User, isReply bool, targetNickname, message string) {
	targetUser := um.FindUser(targetNickname)
	if targetUser == nil {
		logEvent(fmt.Sprintf("Private message failed: user %s not found", targetNickname))
		_ = um.sendMessageToUser(sender, "User not found.")
		return
	}

	if sender == targetUser {
		// Case when sender messages themselves
		_ = um.sendMessageToUser(sender, termcolor.EncodeHTMLToTerm(tcServerTags, fmt.Sprintf("<w>You whisper to yourself: %s</w>", message)))
	} else {
		// Case when sender messages another user
		msgtype := "whisper whispers"
		if isReply {
			msgtype = "reply replies"
		}
		_ = um.sendMessageToUser(sender, termcolor.EncodeHTMLToTerm(tcServerTags, fmt.Sprintf("<w>You %s to %s: %s</w>", strings.Fields(msgtype)[0], targetUser.nickname, message)))
		if sender.privilege == 1 {
			_ = um.sendMessageToUser(targetUser, termcolor.EncodeHTMLToTerm(tcServerTags, fmt.Sprintf("<aw>%s (admin) %s: %s</ww>", sender.nickname, strings.Fields(msgtype)[1], message)))
		} else {
			_ = um.sendMessageToUser(targetUser, termcolor.EncodeHTMLToTerm(tcServerTags, fmt.Sprintf("<w>%s %s: %s</w>", sender.nickname, strings.Fields(msgtype)[1], message)))
		}
		// Update the lastPrivateMessageFrom field
		targetUser.lastMsgFrom = sender.nickname
	}
}

// handleUserLogout logs out a user from the chat server.
func (um *UserManager) handleUserLogout(user *User) {
	logEvent(fmt.Sprintf("User %s has quit", user.nickname))
	um.broadcastMessage(fmt.Sprintf("%s has left the chat", user.nickname))
	if err := user.conn.Close(); err != nil {
		logEvent(fmt.Sprintf("Error closing connection for user %s: %v", user.nickname, err))
	}
	um.removeUser(user.nickname)
}

// handleWho sends the user a list of all users currently online.
func (um *UserManager) handleWho(user *User) {
	userList := um.generateUserList(user)
	_ = um.sendMessageToUser(user, "Current Users:")
	_ = um.sendMessageToUser(user, userList)
}

// handleNewNick changes the nickname of a user.
func (um *UserManager) handleNewNick(user *User, newNickname string) {
	if err := validateNickname(newNickname); err != nil {
		_ = um.sendMessageToUser(user, err.Error())
		return
	}

	if existingUser := um.FindUser(newNickname); existingUser != nil {
		_ = um.sendMessageToUser(user, "That nickname is already in use. Please choose another.")
		return
	}

	oldNickname := user.nickname
	logEvent(fmt.Sprintf("User %s changed nickname to %s", oldNickname, newNickname))
	um.removeUser(oldNickname)
	user.nickname = newNickname
	if !um.addUser(user) {
		_ = um.sendMessageToUser(user, "Failed to update nickname. Please try again.")
		return
	}
	um.broadcastMessage(fmt.Sprintf("%s is now: %s", oldNickname, user.nickname))
}

// handleKick removes a user from the chat server, kicking them off.
// It returns an error if the user to be kicked is not found.
func (um *UserManager) handleKick(targetNickname, kickMessage string, admin *User) error {
	if strings.EqualFold(targetNickname, admin.nickname) {
		// Check if the admin is trying to kick themselves
		logEvent(fmt.Sprintf("%s tried to kick themselves", admin.nickname))
		return errors.New("cannot kick yourself")
	}

	targetUser := um.FindUser(targetNickname)
	if targetUser == nil {
		logEvent(fmt.Sprintf("Kick attempt failed: user %s not found", targetNickname))
		return errors.New("user not found")
	}

	nomsg := strings.Trim(kickMessage, " ") == ""
	// Notify the kicked user
	var kickMsg string
	if nomsg {
		kickMsg = fmt.Sprintf("You have been kicked by %s.", admin.nickname)
	} else {
		kickMsg = fmt.Sprintf("You have been kicked by %s: %s", admin.nickname, kickMessage)
	}
	if err := um.sendMessageToUser(targetUser, kickMsg); err != nil {
		logEvent(fmt.Sprintf("Error sending message to user %s: %v", targetUser.nickname, err))
	}

	// Close the connection
	if err := targetUser.conn.Close(); err != nil {
		logEvent(fmt.Sprintf("Error closing connection for kicked user %s: %v", targetUser.nickname, err))
		return err
	}

	// Remove the user from the user manager
	um.removeUser(targetUser.nickname)

	// Log and notify others
	var broadcastMessage string
	if nomsg {
		broadcastMessage = fmt.Sprintf("%s has been kicked.", targetUser.nickname)
	} else {
		broadcastMessage = fmt.Sprintf("%s has been kicked by %s: %s", targetUser.nickname, admin.nickname, kickMessage)
	}
	logEvent(broadcastMessage)
	um.broadcastMessageExcept(targetUser.nickname, broadcastMessage)

	// Inform the admin
	if err := um.sendMessageToUser(admin, fmt.Sprintf("User %s has been kicked.", targetUser.nickname)); err != nil {
		logEvent(fmt.Sprintf("Error sending kick confirmation to admin %s: %v", admin.nickname, err))
	}

	return nil // Kick successful
}

func changePasswordForUser(username, newPassword string) error {
	// Hash the new password
	newPasswordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("error hashing new password: %v", err)
	}

	// Update the password in the database
	return updatePassword(DB, username, string(newPasswordHash))
}

func (um *UserManager) handleChangePassword(user *User, args []string) {
	if len(args) != 2 {
		_ = um.sendMessageToUser(user, "Usage: /passwd <username> <newPassword>")
		return
	}

	targetUsername := args[0]
	newPassword := args[1]

	// Check if the user is an admin
	if user.privilege == 1 {
		// Admin can change any user's password
		err := changePasswordForUser(targetUsername, newPassword)
		if err != nil {
			_ = um.sendMessageToUser(user, fmt.Sprintf("Error changing password for %s: %v", targetUsername, err))
		} else {
			_ = um.sendMessageToUser(user, fmt.Sprintf("Password for %s updated successfully.", targetUsername))
		}
	} else {
		// Regular users can only change their own password
		if targetUsername != user.username {
			_ = um.sendMessageToUser(user, "You can only change your own password.")
			return
		}

		err := changePasswordForUser(user.username, newPassword)
		if err != nil {
			_ = um.sendMessageToUser(user, "Error updating your password. Please try again later.")
		} else {
			_ = um.sendMessageToUser(user, "Your password has been updated successfully.")
		}
	}
}

func (um *UserManager) handleCreateUser(user *User, args []string) {
	if user.privilege < 1 {
		_ = um.sendMessageToUser(user, ErrPrivilege)
		return
	}
	if len(args) != 2 {
		_ = um.sendMessageToUser(user, "Usage: /createuser <username> <password>")
		return
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(args[1]), bcrypt.DefaultCost)
	if err != nil {
		_ = um.sendMessageToUser(user, "Error hashing password. Please try again later.")
		return
	}
	err = createUser(DB, args[0], string(passwordHash), 0)
	if err != nil {
		_ = um.sendMessageToUser(user, fmt.Sprintf("Error creating user: %v", err))
		return
	} else {
		_ = um.sendMessageToUser(user, fmt.Sprintf("User %s created successfully.", args[0]))
	}
}

func (um *UserManager) handleChangePrivilege(user *User, args []string) {
	syntax := "Usage: /priv <username> <privilege> (0: user, 1: admin)"
	if user.privilege < 1 {
		_ = um.sendMessageToUser(user, ErrPrivilege)
		return
	}
	if len(args) != 2 {
		_ = um.sendMessageToUser(user, syntax)
		return
	}
	if strings.EqualFold(args[1], "admin") {
		_ = um.sendMessageToUser(user, "Privilege level change for user 'admin' is not allowed.")
		return
	}
	newPrivilege, err := strconv.Atoi(args[1])
	if err != nil {
		_ = um.sendMessageToUser(user, syntax)
		return
	}
	err = updatePrivilege(DB, args[0], newPrivilege)
	if err != nil {
		_ = um.sendMessageToUser(user, fmt.Sprintf("Error changing privilege for user %s: %v", args[0], err))
		return
	}
	_ = um.sendMessageToUser(user, fmt.Sprintf("Privilege for user %s updated successfully. User must re-login.", args[0]))
}

func (um *UserManager) handleDeleteUser(user *User, args []string) {
	syntax := "Usage: /deleteuser <username>"
	if user.privilege < 1 {
		_ = um.sendMessageToUser(user, ErrPrivilege)
		return
	}
	if len(args) != 1 {
		_ = um.sendMessageToUser(user, syntax)
		return
	}
	targetUsername := args[0]
	if strings.EqualFold(targetUsername, user.username) {
		_ = um.sendMessageToUser(user, "Deleting current user is not allowed.")
		return
	}

	// Check if the user is currently logged in
	targetUser := um.FindUser(targetUsername)
	if targetUser != nil {
		// Kick the user if they are logged in
		err := um.handleKick(targetUsername, "User deleted by administrator", user)
		if err != nil {
			_ = um.sendMessageToUser(user, fmt.Sprintf("Error kicking user %s: %v", targetUsername, err))
			return
		}
	}

	// Proceed to delete the user from the database
	err := deleteUser(DB, targetUsername)
	if err != nil {
		_ = um.sendMessageToUser(user, fmt.Sprintf("Error deleting user %s: %v", targetUsername, err))
		return
	}
	_ = um.sendMessageToUser(user, fmt.Sprintf("User %s deleted successfully.", targetUsername))
}

func (um *UserManager) handleEnumUsers(user *User) {
	if user.privilege < 1 {
		_ = um.sendMessageToUser(user, ErrPrivilege)
		return
	}
	dbusers, err := enumUsers(DB)
	if err != nil {
		_ = um.sendMessageToUser(user, fmt.Sprintf("Error enumerating users: %v", err))
		return
	}
	msg := strings.Join(dbusers, "\n")
	_ = um.sendMessageToUser(user, msg+"\n(End of list)")
}
