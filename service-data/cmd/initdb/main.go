package main

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"log"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	// 1. Connect to MySQL without database first to create the DB
	dsnNoDb := "penny@unix(/home/penny/mariadb-tmp/mysql.sock)/?charset=utf8mb4"
	db, err := sql.Open("mysql", dsnNoDb)
	if err != nil {
		log.Fatalf("Failed to open connection: %v", err)
	}
	
	fmt.Println("Creating database 'wordbot' if not exists...")
	_, err = db.Exec("CREATE DATABASE IF NOT EXISTS wordbot CHARACTER SET utf8mb4;")
	if err != nil {
		log.Fatalf("Failed to create database: %v", err)
	}

	fmt.Println("Configuring database user permissions...")
	_, _ = db.Exec("ALTER USER 'penny'@'localhost' IDENTIFIED VIA mysql_native_password USING '';")
	_, _ = db.Exec("CREATE USER IF NOT EXISTS 'penny'@'127.0.0.1' IDENTIFIED VIA mysql_native_password USING '';")
	_, _ = db.Exec("GRANT ALL PRIVILEGES ON wordbot.* TO 'penny'@'localhost';")
	_, _ = db.Exec("GRANT ALL PRIVILEGES ON wordbot.* TO 'penny'@'127.0.0.1';")
	_, _ = db.Exec("FLUSH PRIVILEGES;")

	db.Close()

	// 2. Connect to the 'wordbot' database with multiStatements support
	dsnDb := "penny@unix(/home/penny/mariadb-tmp/mysql.sock)/wordbot?multiStatements=true&charset=utf8mb4"
	db, err = sql.Open("mysql", dsnDb)
	if err != nil {
		log.Fatalf("Failed to open connection to wordbot: %v", err)
	}
	defer db.Close()

	// 3. Read schema.sql
	schemaPath := "../migrations/schema.sql"
	schemaBytes, err := ioutil.ReadFile(schemaPath)
	if err != nil {
		// Try absolute/relative to working dir
		schemaPath = "migrations/schema.sql"
		schemaBytes, err = ioutil.ReadFile(schemaPath)
		if err != nil {
			log.Fatalf("Failed to read schema.sql: %v", err)
		}
	}

	fmt.Println("Applying schema.sql...")
	_, err = db.Exec(string(schemaBytes))
	if err != nil {
		log.Fatalf("Failed to apply schema: %v", err)
	}

	fmt.Println("Database successfully initialized!")
}
