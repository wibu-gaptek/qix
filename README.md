# Qix - SQL Query Builder for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/wibu-gaptek/qix.svg)](https://pkg.go.dev/github.com/wibu-gaptek/qix)
[![Go Report Card](https://goreportcard.com/badge/github.com/wibu-gaptek/qix)](https://goreportcard.com/report/github.com/wibu-gaptek/qix)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

## Summary
Qix is a fluent SQL query builder for Go that helps you build complex database queries with an elegant API. It provides a clean and intuitive way to construct SQL queries while maintaining type safety and preventing SQL injection.

## Features

### Core Features
- 🔄 Fluent chainable interface
- 🛡️ SQL injection prevention
- 📦 Transaction support
- 🔍 Query debugging
- 🚀 High performance
- 🧩 ORM for Go structs

### Query Building
- SELECT, INSERT, UPDATE, DELETE operations
- Complex WHERE clauses (IN, NOT IN, NULL, BETWEEN)
- JOIN operations (INNER, LEFT, RIGHT, CROSS)
- GROUP BY and HAVING
- ORDER BY and LIMIT
- Subqueries and nested conditions
- Raw queries when needed

### Advanced Features
- Batch operations (bulk insert/update)
- Date/Time helpers (WhereDate, WhereMonth, etc)
- Column comparisons
- Aggregate functions
- Query events/logging
- Custom function support

### ORM Features
- Struct to database table mapping
- Tag-based column definitions
- CRUD operations on struct models
- Relationship management (hasOne, hasMany, belongsTo, manyToMany)
- Eager loading with With() and WithQuery()
- Manual preloading of relationships
- Nested transactions with savepoints
- Pagination with struct models
- Transaction support with models

## Installation

```bash
go get github.com/wibu-gaptek/qix
```

## Quick Example

```go
package main

import (
    "context"
    _ "github.com/go-sql-driver/mysql"
    "github.com/wibu-gaptek/qix"
)

func main() {
    // Example: Connect to MySQL database
    db, err := sql.Open("mysql", "user:password@tcp(localhost:3306)/dbname")
    if err != nil {
        panic(err)
    }
    defer db.Close()
    qb := qix.New(db)

    // Simple query
    rows, err := qb.Table("users").
        Select("id", "name", "email").
        Where("age", ">", 18).
        OrderBy("name", "ASC").
        Get(context.Background())
}
```

## Core Concepts

### Builder Pattern
Qix uses the builder pattern to construct queries:
```go
qb.Table("users").
   Select("id", "name").
   Where("active", "=", true)
```

### Transaction Support
```go
err := qb.Transaction(ctx, func(tx *qix.Builder) error {
    // Multiple operations in transaction
    return nil
})
```

### Query Debugging
```go
sql := qb.Table("users").ToSQL() // Get generated SQL
```

## API Reference

### Basic Operations
- `Table(name string)` - Set table name
- `Select(columns ...string)` - Select columns
- `Where(column, operator, value)` - Add WHERE clause
- `Join(table, condition)` - Add JOIN clause
- `GroupBy(columns ...string)` - Add GROUP BY
- `OrderBy(column, direction)` - Add ORDER BY
- `Limit(limit int)` - Set LIMIT
- `Offset(offset int)` - Set OFFSET

### Advanced Queries
- `WhereIn(column, values)` - WHERE IN clause
- `WhereNotIn(column, values)` - WHERE NOT IN clause
- `WhereBetween(column, start, end)` - WHERE BETWEEN
- `WhereNull(column)` - WHERE IS NULL
- `WhereNotNull(column)` - WHERE IS NOT NULL
- `WhereExists(subQuery)` - WHERE EXISTS
- `WhereRaw(sql, bindings)` - Raw WHERE clause

### Date Operations
- `WhereDate(column, operator, value)`
- `WhereMonth(column, operator, value)`
- `WhereDay(column, operator, value)`
- `WhereYear(column, operator, value)`

### Batch Operations
- `BatchInsert(data []map[string]interface{})`
- `BulkUpdate(data []map[string]interface{}, key string)`

## Using ORM Tags

Qix ORM uses struct tags to map Go structs to database tables:

```go
type User struct {
    ID        int       `db:"id,pk,auto"`     // Primary key, auto-increment
    Name      string    `db:"name"`           // Basic column mapping
    Email     string    `db:"email"`          // Basic column mapping
    Age       int       `db:"age,omitempty"`  // Skip zero values
    CreatedAt time.Time `db:"created_at"`     // Time mapping
    Password  string    `db:"password,omit"`  // Never send to/from DB
    Temp      string    `db:"-"`              // Ignore this field
    Posts     []Post    `rel:"hasMany,foreignKey:user_id"` // One-to-many relationship
    Profile   Profile   `rel:"hasOne,foreignKey:user_id"`  // One-to-one relationship
}

type Post struct {
    ID     int    `db:"id,pk,auto"`
    UserID int    `db:"user_id"`
    Title  string `db:"title"`
    User   User   `rel:"belongsTo,foreignKey:user_id"` // Belongs-to relationship
    Tags   []Tag  `rel:"manyToMany,pivot:post_tags,pivotFk:post_id,pivotRfk:tag_id"` // Many-to-many
}
```

### Database Column Tags
Available `db` tag options:
- `pk` - Mark as primary key
- `auto` - Auto-increment field
- `omitempty` - Skip zero values on insert/update
- `omit` - Never include in database operations
- `-` - Ignore field entirely

### Relationship Tags
Available `rel` tag options:
- `hasOne` - One-to-one relationship where the related model contains foreign key
- `hasMany` - One-to-many relationship where related models contain foreign key
- `belongsTo` - Inverse of hasOne or hasMany (model contains the foreign key)
- `manyToMany` - Many-to-many relationship using a pivot table

Relationship parameters:
- `foreignKey` - Specifies the foreign key column name (default: parent_table_id)
- `localKey` - Specifies the local key column (default: id)
- `table` - Override the related model's table name
- `pivot` - For manyToMany, specifies the pivot table name
- `pivotFk` - For manyToMany, specifies the pivot table foreign key column for this model
- `pivotRfk` - For manyToMany, specifies the pivot table foreign key column for related model

## Eager Loading

Qix ORM supports eager loading relationships:

```go
// Eager load basic relationships
users, err := userModel.With("Posts", "Profile").All(ctx)

// Eager load with custom constraints
posts, err := postModel.WithQuery("Comments", func(q *qix.Builder) *qix.Builder {
    return q.Where("approved", "=", true)
}).Find(ctx, postID)

// Eager load nested relationships
users, err := userModel.With("Posts.Comments", "Profile").All(ctx)
```

## Relationships API

Qix ORM provides methods for working with relationships:

```go
// Define and query relationships
postsQuery, err := userModel.HasMany(ctx, Post{}, "user_id", "id")
postsRows, err := postsQuery.Where("published", "=", true).Get(ctx)

profileQuery, err := userModel.HasOne(ctx, Profile{}, "user_id", "id")
profileRow, err := profileQuery.First(ctx)

userQuery, err := postModel.BelongsTo(ctx, User{}, "user_id", "id")
userRow, err := userQuery.Get(ctx)

tagsQuery, err := postModel.BelongsToMany(ctx, Tag{}, "post_tags", "post_id", "tag_id")
tagsRows, err := tagsQuery.OrderBy("name", "ASC").Get(ctx)
```

## Nested Transactions

Qix ORM supports nested transactions using database savepoints:

```go
err := userModel.Transaction(ctx, func(outerTx *qix.Model) error {
    // Outer transaction operations...
    
    // Nested transaction with automatic savepoint management
    return outerTx.Transaction(ctx, func(innerTx *qix.Model) error {
        // Inner transaction operations...
        return nil
    })
})
```

## Issues

Please report any issues on our [GitHub Issues](https://github.com/wibu-gaptek/qix/issues) page.

Common issues:
1. Connection handling
2. Transaction management
3. Query optimization
4. Type conversion

## Contributing

We welcome contributions! Please follow these steps:

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

### Development Setup
```bash
git clone https://github.com/wibu-gaptek/qix.git
cd qix
go test ./...
```

## Contact

- GitHub: [@wibu-gaptek](https://github.com/wibu-gaptek)
- Twitter: [@wibu_gaptek](https://twitter.com/wibu_gaptek)

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
