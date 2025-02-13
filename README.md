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
