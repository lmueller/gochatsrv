// project: gochatsrv
// file: serveraux.go
//
// # Auxiliary functions for server such as watchdogs/event management
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
	"strings"
	"time"
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

// Channels for communicating results back to the main function
var timedLoginNickChan = make(chan string, 1)
var timedLoginErrorChan = make(chan error, 1)

func countdownWarnings(userManager *UserManager, remainingTime int) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	const Minute = 60

	for remainingTime > 0 {
		select {
		case <-ticker.C:
			remainingTime--

			if remainingTime > 15*Minute { // More than 15 minutes
				if remainingTime%(15*Minute) == 0 {
					minutes := remainingTime / Minute
					userManager.broadcastMessage(fmt.Sprintf("System Notice: SHUTDOWN in %d minutes", minutes))
				}
			} else if remainingTime <= 15*Minute && remainingTime > 1*Minute { // Between 1 and 15 minutes
				if remainingTime%60 == 0 {
					minutes := remainingTime / 60
					userManager.broadcastMessage(fmt.Sprintf("System Notice: SHUTDOWN in %d minutes. Please log out now.", minutes))
				}
			} else if remainingTime <= 1*Minute { // Less than 1 minute
				if remainingTime%10 == 0 && remainingTime > 0 {
					userManager.broadcastMessage(fmt.Sprintf("System Notice: SHUTDOWN IMMINENT in %d seconds. LOG OUT NOW!", remainingTime))
				}
			}
		}
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
		return "", fmt.Errorf("timeout")
	}
}

func resetTimedLoginChannels() {
	for len(timedLoginNickChan) > 0 {
		<-timedLoginNickChan
	}
	for len(timedLoginErrorChan) > 0 {
		<-timedLoginErrorChan
	}
}

func logEvent(event string) {
	log.Println(event)
}
