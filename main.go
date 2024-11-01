// project: gochatsrv
// file: main.go
//
// # Main file stub
//
// Date: 2024-10-28
// Author: Lutz Mueller <lmuellerhome@gmail.com>
// License: proprietary. All rights reserved.
//
// Version: see github.com/lmueller/gochatsrv

package main

import (
	"database/sql"
	"log"
	"os"
	"os/signal"
	"syscall"
)

var (
	DB *sql.DB
)

func closeDB(db *sql.DB) {
	log.Println("Closing database connection")
	err := db.Close()
	if err != nil {
		log.Fatalf("Failed to close database: %v", err)
	}
}

func main() {
	// Initialize the database
	var err error
	DB, err = initDB()
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer closeDB(DB) // Ensure the database is closed on normal exit

	// Create and run the chat server
	server := &ChatServer{
		userManager: &UserManager{
			users: make(map[string]*User),
		},
		commands: make(chan ServerCommand),
	}

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("SIGINT/SIGTERM received, shutting down server...")
		server.commands <- ServerCommand{
			Command: "shutdown",
			User:    nil, // No user associated with this command
			Args:    []string{"0"},
		}
		closeDB(DB) // Ensure the database is closed on signal
		os.Exit(0)
	}()

	// Run the server
	server.Run()
}
