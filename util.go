// project: gochatsrv
// file: util.go
//
// # general utility functions
//
// Date: 2024-10-28
// Author: Lutz Mueller <lmuellerhome@gmail.com>
// License: proprietary. All rights reserved.
//
// Version: see github.com/lmueller/gochatsrv

package main

import (
	"errors"
	"log"
	"regexp"
	"strings"
)

func logEvent(event string) {
	log.Println(event)
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
