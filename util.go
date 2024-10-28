// project: gochatsrv
// file: util.go
//
// # general utility functions
//
// Date: 2024-10-28
// Author: Lutz Mueller <lmuellerhome@gmail.com>
// License: proprietary. All rights reserved.
//
// Version: v0.1.0
package main

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
)

func sendMessageToConn(conn net.Conn, msgs ...string) error {
	w := bufio.NewWriter(conn)
	for _, msg := range msgs {
		if _, err := fmt.Fprintln(w, msg); err != nil {
			return err
		}
	}
	return w.Flush()
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

func sanitizeNickname(nickname string) string {
	// Remove unwanted characters. Used during login.
	nickname = strings.ReplaceAll(nickname, " ", "")
	nickname = strings.ReplaceAll(nickname, `\`, "")
	return strings.TrimRight(nickname, "\r\n")
}
