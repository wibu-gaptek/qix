package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/wibu-gaptek/qix"
)

// MySQLDB wraps sql.DB to implement qix.DB interface
type MySQLDB struct {
	*sql.DB
}

func main() {
	// Connect to MySQL
	db, err := sql.Open("mysql", "user:password@tcp(localhost:3306)/dbname?parseTime=true")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Create query builder instance
	qb := qix.New(&MySQLDB{db})

	// Example: Create users table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			email VARCHAR(255) UNIQUE NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Example: Insert with transaction
	err = qb.Transaction(ctx, func(tx *qix.Builder) error {
		// Insert a user
		id, err := tx.Table("users").InsertGetId(ctx, map[string]interface{}{
			"name":  "John Doe",
			"email": "john@example.com",
		})
		if err != nil {
			return err
		}
		fmt.Printf("Inserted user with ID: %d\n", id)

		// Update the user
		affected, err := tx.Table("users").
			Where("id", "=", id).
			UpdateWithContext(ctx, map[string]interface{}{
				"name": "John Smith",
			})
		if err != nil {
			return err
		}
		fmt.Printf("Updated %d rows\n", affected)

		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	// Example: Complex query
	rows, err := qb.Table("users").
		Select("id", "name", "email", "created_at").
		Where("created_at", ">=", time.Now().AddDate(0, -1, 0)). // Users created in last month
		WhereNotNull("email").
		OrderBy("created_at", "DESC").
		Limit(10).
		Get(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	// Process results
	for rows.Next() {
		var (
			id        int
			name      string
			email     string
			createdAt time.Time
		)
		if err := rows.Scan(&id, &name, &email, &createdAt); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("User: %d, %s, %s, %s\n", id, name, email, createdAt)
	}
	if err = rows.Err(); err != nil {
		log.Fatal(err)
	}
}
