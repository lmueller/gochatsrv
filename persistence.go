// project: gochatsrv
// file: persistence.go
//
// # persistence functions for gochatsrv; Currently, sqlite3/local files
//
// Date: 2024-10-28
// Author: Lutz Mueller <lmuellerhome@gmail.com>
// License: proprietary. All rights reserved.
//
// Version: see github.com/lmueller/gochatsrv
package main

import (
	"database/sql"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"log"
	"os"

	_ "github.com/mattn/go-sqlite3" // Import the SQLite driver
)

var DBFileName = "./gochatsrv.db"

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func checkPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func initDB() (*sql.DB, error) {

	// Check if the database file exists
	_, err := os.Stat(DBFileName)
	if os.IsNotExist(err) {
		log.Println("Database file does not exist. Creating a new one.")
	} else if err != nil {
		log.Printf("Error checking database file: %v\n", err)
		return nil, err
	}

	// Open the database
	db, err := sql.Open("sqlite3", DBFileName)
	if err != nil {
		log.Printf("Error opening database: %v\n", err)
		return nil, err
	}

	// Ping the database to ensure it's reachable
	if err := db.Ping(); err != nil {
		log.Printf("Error pinging database: %v\n", err)
		return nil, err
	}

	// Create tables if they don't exist
	createTables := `
    CREATE TABLE IF NOT EXISTS users (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        username TEXT UNIQUE NOT NULL,
        password_hash TEXT NOT NULL,
        privilege INTEGER DEFAULT 0
    );
    CREATE TABLE IF NOT EXISTS messages (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        sender_id INTEGER,
        recipient_id INTEGER,
        content TEXT,
        timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY(sender_id) REFERENCES users(id),
        FOREIGN KEY(recipient_id) REFERENCES users(id)
    );
    CREATE TABLE IF NOT EXISTS logs (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        event_type TEXT,
        user_id INTEGER,
        details TEXT,
        timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY(user_id) REFERENCES users(id)
    );
    `
	_, err = db.Exec(createTables)
	if err != nil {
		log.Printf("Error creating tables: %v\n", err)
		return nil, err
	}

	// Check if an admin user exists
	var adminCount int
	err = db.QueryRow("SELECT COUNT(*) FROM users WHERE privilege = 1").Scan(&adminCount)
	if err != nil {
		log.Printf("Error checking for admin user: %v\n", err)
		return nil, err
	}

	// If no admin user exists, create one
	if adminCount == 0 {
		log.Println("No admin user found. Creating default admin user.")
		adminUsername := "admin"
		adminPassword := "admin123" // Replace with a secure password in production
		passwordHash, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("Error hashing admin password: %v\n", err)
			return nil, err
		}

		_, err = db.Exec("INSERT INTO users (username, password_hash, privilege) VALUES (?, ?, ?)", adminUsername, string(passwordHash), 1)
		if err != nil {
			log.Printf("Error creating admin user: %v\n", err)
			return nil, err
		}
	}

	log.Println("Database initialized successfully.")

	return db, nil
}

func createUser(db *sql.DB, username, passwordHash string, privilege int) error {
	_, err := db.Exec("INSERT INTO users (username, password_hash, privilege) VALUES (?, ?, ?)", username, passwordHash, privilege)
	return err
}

func authenticateUser(db *sql.DB, username, password string) (*User, error) {
	var user User
	var hashedPassword string
	err := db.QueryRow("SELECT id, username, password_hash, privilege FROM users WHERE username = ?", username).
		Scan(&user.id, &user.username, &hashedPassword, &user.privilege)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found")
		}
		return nil, err
	}

	// Verify the password
	err = bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	if err != nil {
		return nil, fmt.Errorf("invalid password")
	}

	return &user, nil
}

/*
func getUser(db *sql.DB, username string) (*User, error) {
	row := db.QueryRow("SELECT id, username, password_hash, privilege FROM users WHERE username = ?", username)
	user := &User{}
	err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Privilege)
	if err != nil {
		return nil, err
	}
	return user, nil
}
*/
