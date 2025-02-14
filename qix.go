package qix

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"
)

// DB interface untuk memudahkan testing dan mendukung berbagai database driver
type DB interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// TxDB extends DB interface to support transaction operations
type TxDB interface {
	DB
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// Builder represents the main query builder struct
type Builder struct {
	table               string
	columns             []string
	wheres              []where
	joins               []join
	groups              []string
	havings             []having
	orders              []order
	limit               *int
	offset              *int
	bindings            []interface{}
	db                  DB // tambahkan field db
	unions              []union
	beforeQueryHandlers []QueryEventHandler
	afterQueryHandlers  []QueryEventHandler
}

// where represents a where clause condition
type where struct {
	column   string
	operator string
	value    interface{}
	boolean  string
	isColumn bool
}

type join struct {
	table     string
	condition string
	joinType  string
}

type having struct {
	column   string
	operator string
	value    interface{}
	boolean  string
}

type order struct {
	column    string
	direction string
}

// New creates a new instance of query builder with database connection
func New(db DB) *Builder {
	return &Builder{
		columns:  make([]string, 0),
		wheres:   make([]where, 0),
		joins:    make([]join, 0),
		groups:   make([]string, 0),
		havings:  make([]having, 0),
		orders:   make([]order, 0),
		bindings: make([]interface{}, 0),
		db:       db,
	}
}

// Table sets the table name for the query
func (b *Builder) Table(name string) *Builder {
	b.table = name
	return b
}

// Select adds columns to be selected
func (b *Builder) Select(columns ...string) *Builder {
	b.columns = append(b.columns, columns...)
	return b
}

// Where adds a where clause to the query
func (b *Builder) Where(column string, operator string, value interface{}) *Builder {
	b.wheres = append(b.wheres, where{
		column:   column,
		operator: operator,
		value:    value,
		boolean:  "AND",
	})
	b.bindings = append(b.bindings, value)
	return b
}

// Join adds a JOIN clause to the query
func (b *Builder) Join(table string, condition string) *Builder {
	b.joins = append(b.joins, join{
		table:     table,
		condition: condition,
		joinType:  "INNER",
	})
	return b
}

// LeftJoin adds a LEFT JOIN clause to the query
func (b *Builder) LeftJoin(table string, condition string) *Builder {
	b.joins = append(b.joins, join{
		table:     table,
		condition: condition,
		joinType:  "LEFT",
	})
	return b
}

// GroupBy adds GROUP BY clause to the query
func (b *Builder) GroupBy(columns ...string) *Builder {
	b.groups = append(b.groups, columns...)
	return b
}

// Having adds HAVING clause to the query
func (b *Builder) Having(column string, operator string, value interface{}) *Builder {
	b.havings = append(b.havings, having{
		column:   column,
		operator: operator,
		value:    value,
		boolean:  "AND",
	})
	b.bindings = append(b.bindings, value)
	return b
}

// OrderBy adds ORDER BY clause to the query
func (b *Builder) OrderBy(column string, direction string) *Builder {
	b.orders = append(b.orders, order{
		column:    column,
		direction: direction,
	})
	return b
}

// Limit sets the LIMIT clause
func (b *Builder) Limit(limit int) *Builder {
	b.limit = &limit
	return b
}

// Offset sets the OFFSET clause
func (b *Builder) Offset(offset int) *Builder {
	b.offset = &offset
	return b
}

// Aggregate functions
func (b *Builder) Count(column string) *Builder {
	return b.Select("COUNT(" + column + ")")
}

func (b *Builder) Max(column string) *Builder {
	return b.Select("MAX(" + column + ")")
}

func (b *Builder) Min(column string) *Builder {
	return b.Select("MIN(" + column + ")")
}

func (b *Builder) Avg(column string) *Builder {
	return b.Select("AVG(" + column + ")")
}

func (b *Builder) Sum(column string) *Builder {
	return b.Select("SUM(" + column + ")")
}

// Insert operation
func (b *Builder) Insert(data map[string]interface{}) *Builder {
	columns := make([]string, 0, len(data))

	for column, value := range data {
		columns = append(columns, column)
		b.bindings = append(b.bindings, value)
	}

	b.columns = columns
	return b
}

// Update operation
func (b *Builder) Update(data map[string]interface{}) *Builder {
	for column, value := range data {
		b.columns = append(b.columns, column)
		b.bindings = append(b.bindings, value)
	}
	return b
}

// Delete operation
func (b *Builder) Delete() *Builder {
	return b
}

// SubSelect adds a subquery
func (b *Builder) SubSelect(subQuery *Builder, alias string) *Builder {
	// Implementation for subquery will need more complex logic
	// This is a basic implementation
	return b.Select("(" + subQuery.ToSQL() + ") as " + alias)
}

// ToSQL converts the query builder to SQL string
func (b *Builder) ToSQL() string {
	var query strings.Builder

	// Build base query
	query.WriteString(b.buildBaseQuery())

	// Add UNION clauses
	for _, union := range b.unions {
		if union.typ == UnionAll {
			query.WriteString(" UNION ALL ")
		} else {
			query.WriteString(" UNION ")
		}
		query.WriteString(union.query.buildBaseQuery())
		b.bindings = append(b.bindings, union.query.bindings...)
	}

	return query.String()
}

// buildBaseQuery builds the base SELECT query without UNIONs
func (b *Builder) buildBaseQuery() string {
	var query strings.Builder

	// Build SELECT clause
	if len(b.columns) > 0 {
		query.WriteString("SELECT ")
		query.WriteString(strings.Join(b.columns, ", "))
	} else {
		query.WriteString("SELECT *")
	}

	// Add FROM clause
	if b.table != "" {
		query.WriteString(" FROM ")
		query.WriteString(b.table)
	}

	// Add JOINs
	for _, join := range b.joins {
		query.WriteString(" ")
		query.WriteString(join.joinType)
		query.WriteString(" JOIN ")
		query.WriteString(join.table)
		if join.condition != "" {
			query.WriteString(" ON ")
			query.WriteString(join.condition)
		}
	}

	// Add WHERE clauses
	if len(b.wheres) > 0 {
		query.WriteString(" WHERE ")
		query.WriteString(b.whereSQL())
	}

	// Add GROUP BY
	if len(b.groups) > 0 {
		query.WriteString(" GROUP BY ")
		query.WriteString(strings.Join(b.groups, ", "))
	}

	// Add HAVING
	if len(b.havings) > 0 {
		query.WriteString(" HAVING ")
		for i, having := range b.havings {
			if i > 0 {
				query.WriteString(" ")
				query.WriteString(having.boolean)
				query.WriteString(" ")
			}
			query.WriteString(having.column)
			query.WriteString(" ")
			query.WriteString(having.operator)
			query.WriteString(" ?")
		}
	}

	// Add ORDER BY
	if len(b.orders) > 0 {
		query.WriteString(" ORDER BY ")
		orderClauses := make([]string, len(b.orders))
		for i, order := range b.orders {
			orderClauses[i] = order.column + " " + order.direction
		}
		query.WriteString(strings.Join(orderClauses, ", "))
	}

	// Add LIMIT and OFFSET
	if b.limit != nil {
		query.WriteString(" LIMIT ?")
		b.bindings = append(b.bindings, *b.limit)
	}
	if b.offset != nil {
		query.WriteString(" OFFSET ?")
		b.bindings = append(b.bindings, *b.offset)
	}

	return query.String()
}

// WhereIn adds a WHERE IN clause to the query
func (b *Builder) WhereIn(column string, values ...interface{}) *Builder {
	if len(values) == 0 {
		return b
	}

	// Create placeholders array
	placeholders := make([]string, len(values))
	for i := range values {
		placeholders[i] = "?"
		b.bindings = append(b.bindings, values[i])
	}

	// Create the where clause with the correct parentheses treatment
	whereClause := where{
		column:   column,
		operator: "IN",
		value:    strings.Join(placeholders, ", "), // Remove parentheses here
		boolean:  "AND",
	}

	// Add to wheres slice
	b.wheres = append(b.wheres, whereClause)
	return b
}

// WhereNotIn adds a WHERE NOT IN clause to the query
func (b *Builder) WhereNotIn(column string, values ...interface{}) *Builder {
	if len(values) == 0 {
		return b
	}

	// Handle array/slice value
	if len(values) == 1 {
		if arr, ok := values[0].([]interface{}); ok {
			values = arr
		}
	}

	placeholders := make([]string, len(values))
	for i := range values {
		placeholders[i] = "?"
		b.bindings = append(b.bindings, values[i])
	}

	b.wheres = append(b.wheres, where{
		column:   column,
		operator: "NOT IN",
		value:    strings.Join(placeholders, ", "),
		boolean:  "AND",
	})
	return b
}

// WhereNull adds a WHERE IS NULL clause to the query
func (b *Builder) WhereNull(column string) *Builder {
	b.wheres = append(b.wheres, where{
		column:   column,
		operator: "IS",
		value:    "NULL",
		boolean:  "AND",
	})
	return b
}

// WhereNotNull adds a WHERE IS NOT NULL clause to the query
func (b *Builder) WhereNotNull(column string) *Builder {
	b.wheres = append(b.wheres, where{
		column:   column,
		operator: "IS NOT",
		value:    "NULL",
		boolean:  "AND",
	})
	return b
}

// WhereBetween adds a WHERE BETWEEN clause to the query
func (b *Builder) WhereBetween(column string, start, end interface{}) *Builder {
	b.wheres = append(b.wheres, where{
		column:   column,
		operator: "BETWEEN",
		value:    "? AND ?",
		boolean:  "AND",
	})
	b.bindings = append(b.bindings, start, end)
	return b
}

// OrWhere adds an OR WHERE clause to the query
func (b *Builder) OrWhere(column string, operator string, value interface{}) *Builder {
	b.wheres = append(b.wheres, where{
		column:   column,
		operator: operator,
		value:    value,
		boolean:  "OR",
	})
	b.bindings = append(b.bindings, value)
	return b
}

// WhereDate adds a WHERE DATE clause to the query
func (b *Builder) WhereDate(column string, operator string, value interface{}) *Builder {
	b.wheres = append(b.wheres, where{
		column:   "DATE(" + column + ")",
		operator: operator,
		value:    value,
		boolean:  "AND",
	})
	b.bindings = append(b.bindings, value)
	return b
}

// WhereYear adds a WHERE YEAR clause to the query
func (b *Builder) WhereYear(column string, operator string, value interface{}) *Builder {
	b.wheres = append(b.wheres, where{
		column:   "YEAR(" + column + ")",
		operator: operator,
		value:    value,
		boolean:  "AND",
	})
	b.bindings = append(b.bindings, value)
	return b
}

// createPlaceholders generates SQL placeholders and bindings for values
func createPlaceholders(values []interface{}) (string, []interface{}) {
	placeholders := make([]string, len(values))
	bindings := make([]interface{}, len(values))
	for i, val := range values {
		placeholders[i] = "?"
		bindings[i] = val
	}
	return strings.Join(placeholders, ", "), bindings
}

// WhereMonth adds a WHERE MONTH clause to the query
func (b *Builder) WhereMonth(column string, operator string, values ...interface{}) *Builder {
	if len(values) == 0 {
		return b
	}

	var valueStr string
	var bindings []interface{}

	if len(values) == 1 {
		if v, ok := values[0].([]int); ok {
			valueStr, bindings = createPlaceholders(sliceToInterface(v))
		} else {
			valueStr = "?"
			bindings = values[:1]
		}
	} else {
		valueStr, bindings = createPlaceholders(values)
	}

	// if operator == "IN" {
	// 	valueStr = "(" + valueStr + ")"
	// }

	b.wheres = append(b.wheres, where{
		column:   "MONTH(" + column + ")",
		operator: operator,
		value:    valueStr,
		boolean:  "AND",
	})
	b.bindings = append(b.bindings, bindings...)
	return b
}

// sliceToInterface converts []int to []interface{}
func sliceToInterface(v []int) []interface{} {
	result := make([]interface{}, len(v))
	for i, val := range v {
		result[i] = val
	}
	return result
}

// WhereDay adds a WHERE DAY clause to the query
func (b *Builder) WhereDay(column string, operator string, value interface{}) *Builder {
	b.wheres = append(b.wheres, where{
		column:   "DAY(" + column + ")",
		operator: operator,
		value:    value,
		boolean:  "AND",
	})
	b.bindings = append(b.bindings, value)
	return b
}

// WhereColumn adds a WHERE clause comparing two columns
func (b *Builder) WhereColumn(column1 string, operator string, column2 string) *Builder {
	b.wheres = append(b.wheres, where{
		column:   column1,
		operator: operator,
		value:    column2,
		boolean:  "AND",
		isColumn: true,
	})
	return b
}

// OrWhereColumn adds an OR WHERE clause comparing two columns
func (b *Builder) OrWhereColumn(column1 string, operator string, column2 string) *Builder {
	b.wheres = append(b.wheres, where{
		column:   column1,
		operator: operator,
		value:    column2,
		boolean:  "OR",
		isColumn: true,
	})
	return b
}

// Get executes the SELECT query and returns the rows
func (b *Builder) Get(ctx context.Context) (*sql.Rows, error) {
	query := b.ToSQL()
	return b.db.QueryContext(ctx, query, b.bindings...)
}

// First executes the SELECT query and returns the first row
func (b *Builder) First(ctx context.Context) (*sql.Rows, error) {
	b.Limit(1)
	query := b.ToSQL()
	return b.db.QueryContext(ctx, query, b.bindings...)
}

// InsertGetId executes the INSERT query and returns the last inserted ID
func (b *Builder) InsertGetId(ctx context.Context, data map[string]interface{}) (int64, error) {
	b.Insert(data)

	columns := make([]string, 0, len(data))
	placeholders := make([]string, 0, len(data))

	for column := range data {
		columns = append(columns, column)
		placeholders = append(placeholders, "?")
	}

	query := "INSERT INTO " + b.table + " (" + strings.Join(columns, ", ") + ") VALUES (" + strings.Join(placeholders, ", ") + ")"

	result, err := b.db.ExecContext(ctx, query, b.bindings...)
	if err != nil {
		return 0, err
	}

	return result.LastInsertId()
}

// UpdateWithContext executes the UPDATE query with context
func (b *Builder) UpdateWithContext(ctx context.Context, data map[string]interface{}) (int64, error) {
	b.Update(data)

	sets := make([]string, 0, len(data))
	for column := range data {
		sets = append(sets, column+" = ?")
	}

	query := "UPDATE " + b.table + " SET " + strings.Join(sets, ", ")

	if len(b.wheres) > 0 {
		query += " WHERE " + b.whereSQL()
	}

	result, err := b.db.ExecContext(ctx, query, b.bindings...)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// DeleteWithContext executes the DELETE query with context
func (b *Builder) DeleteWithContext(ctx context.Context) (int64, error) {
	query := "DELETE FROM " + b.table

	if len(b.wheres) > 0 {
		query += " WHERE " + b.whereSQL()
	}

	result, err := b.db.ExecContext(ctx, query, b.bindings...)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// whereSQL generates the WHERE clause SQL
func (b *Builder) whereSQL() string {
	var whereClauses []string
	for i, where := range b.wheres {
		if i > 0 {
			whereClauses = append(whereClauses, where.boolean)
		}

		switch {
		case where.operator == "" && where.value == "":
			// For raw or nested conditions
			whereClauses = append(whereClauses, where.column)

		case where.value == "NULL":
			// For IS NULL conditions
			whereClauses = append(whereClauses, fmt.Sprintf("%v %v %v", where.column, where.operator, where.value))

		case where.isColumn:
			// For column comparisons
			whereClauses = append(whereClauses, fmt.Sprintf("%v %v %v", where.column, where.operator, where.value))

		case where.operator == "IN" || where.operator == "NOT IN" || where.operator == "EXISTS":
			// Special handling for IN operator
			whereClauses = append(whereClauses, fmt.Sprintf("%v %v (%v)", where.column, where.operator, where.value))

		case where.operator == "BETWEEN":
			// Special handling for BETWEEN operator
			whereClauses = append(whereClauses, fmt.Sprintf("%v %v %v", where.column, where.operator, where.value))

		default:
			// For normal conditions
			whereClauses = append(whereClauses, where.column+" "+where.operator+" ?")
		}
	}
	return strings.Join(whereClauses, " ")
}

// Transaction executes a function within a transaction
func (b *Builder) Transaction(ctx context.Context, fn func(*Builder) error) error {
	txDB, ok := b.db.(TxDB)
	if !ok {
		return fmt.Errorf("database does not support transactions")
	}

	tx, err := txDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	// Create a new builder with the transaction
	txBuilder := &Builder{
		table:    b.table,
		columns:  b.columns,
		wheres:   b.wheres,
		joins:    b.joins,
		groups:   b.groups,
		havings:  b.havings,
		orders:   b.orders,
		limit:    b.limit,
		offset:   b.offset,
		bindings: b.bindings,
		db:       tx,
	}

	if err := fn(txBuilder); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("error rolling back: %v (original error: %v)", rbErr, err)
		}
		return err
	}

	return tx.Commit()
}

// BatchInsert executes multiple INSERT in a single query
func (b *Builder) BatchInsert(ctx context.Context, data []map[string]interface{}) error {
	if len(data) == 0 {
		return nil
	}

	// Get columns from first row
	columns := make([]string, 0)
	for column := range data[0] {
		columns = append(columns, column)
	}

	// Build placeholders and collect values
	var placeholders []string
	for _, row := range data {
		rowPlaceholders := make([]string, len(columns))
		for i, col := range columns {
			rowPlaceholders[i] = "?"
			b.bindings = append(b.bindings, row[col])
		}
		placeholders = append(placeholders, "("+strings.Join(rowPlaceholders, ", ")+")")
	}

	query := "INSERT INTO " + b.table +
		" (" + strings.Join(columns, ", ") + ") VALUES " +
		strings.Join(placeholders, ", ")

	_, err := b.db.ExecContext(ctx, query, b.bindings...)
	return err
}

// BulkUpdate executes multiple UPDATE in a single query
func (b *Builder) BulkUpdate(ctx context.Context, data []map[string]interface{}, key string) error {
	if len(data) == 0 {
		return nil
	}

	// Build CASE statements for each column
	var sets []string
	for column := range data[0] {
		if column == key {
			continue
		}
		caseStmt := column + " = CASE " + key
		for _, row := range data {
			caseStmt += fmt.Sprintf(" WHEN ? THEN ?")
			b.bindings = append(b.bindings, row[key], row[column])
		}
		caseStmt += " END"
		sets = append(sets, caseStmt)
	}

	// Collect all keys for WHERE IN clause
	keys := make([]interface{}, len(data))
	for i, row := range data {
		keys[i] = row[key]
		b.bindings = append(b.bindings, row[key])
	}

	query := "UPDATE " + b.table + " SET " + strings.Join(sets, ", ") +
		" WHERE " + key + " IN (" + strings.Repeat("?,", len(keys)-1) + "?)"

	_, err := b.db.ExecContext(ctx, query, b.bindings...)
	return err
}

// RightJoin adds a RIGHT JOIN clause
func (b *Builder) RightJoin(table string, condition string) *Builder {
	b.joins = append(b.joins, join{
		table:     table,
		condition: condition,
		joinType:  "RIGHT",
	})
	return b
}

// CrossJoin adds a CROSS JOIN clause
func (b *Builder) CrossJoin(table string) *Builder {
	b.joins = append(b.joins, join{
		table:    table,
		joinType: "CROSS",
	})
	return b
}

// JoinSub adds a subquery JOIN
func (b *Builder) JoinSub(subQuery *Builder, as string, condition string) *Builder {
	b.joins = append(b.joins, join{
		table:     "(" + subQuery.ToSQL() + ") AS " + as,
		condition: condition,
		joinType:  "INNER",
	})
	b.bindings = append(b.bindings, subQuery.bindings...)
	return b
}

// WhereExists adds WHERE EXISTS clause
func (b *Builder) WhereExists(subQuery *Builder) *Builder {
	b.wheres = append(b.wheres, where{
		column:   "EXISTS",
		operator: "",
		value:    "(" + subQuery.ToSQL() + ")",
		boolean:  "AND",
	})
	b.bindings = append(b.bindings, subQuery.bindings...)
	return b
}

// WhereLike adds WHERE LIKE clause
func (b *Builder) WhereLike(column string, pattern string) *Builder {
	b.wheres = append(b.wheres, where{
		column:   column,
		operator: "LIKE",
		value:    pattern,
		boolean:  "AND",
	})
	b.bindings = append(b.bindings, pattern)
	return b
}

// WhereRaw adds raw WHERE condition
func (b *Builder) WhereRaw(sql string, bindings ...interface{}) *Builder {
	b.wheres = append(b.wheres, where{
		column:   sql,
		operator: "",
		value:    "",
		boolean:  "AND",
	})
	b.bindings = append(b.bindings, bindings...)
	return b
}

// QueryFunc represents a function that modifies the query builder
type QueryFunc func(*Builder)

// WhereFunc adds a WHERE clause using a callback function
func (b *Builder) WhereFunc(fn QueryFunc) *Builder {
	subBuilder := New(b.db)
	fn(subBuilder)

	// Merge conditions from subBuilder
	b.wheres = append(b.wheres, subBuilder.wheres...)
	b.bindings = append(b.bindings, subBuilder.bindings...)
	return b
}

// OrWhereFunc adds an OR WHERE clause using a callback function
func (b *Builder) OrWhereFunc(fn QueryFunc) *Builder {
	subBuilder := New(b.db)
	fn(subBuilder)

	// Convert first condition to OR
	if len(subBuilder.wheres) > 0 {
		subBuilder.wheres[0].boolean = "OR"
	}

	b.wheres = append(b.wheres, subBuilder.wheres...)
	b.bindings = append(b.bindings, subBuilder.bindings...)
	return b
}

// JoinFunc adds a JOIN clause using a callback function
func (b *Builder) JoinFunc(table string, fn QueryFunc) *Builder {
	subBuilder := New(b.db)
	fn(subBuilder)

	// Convert WHERE conditions to JOIN conditions
	var conditions []string
	for _, where := range subBuilder.wheres {
		if where.isColumn {
			conditions = append(conditions, fmt.Sprintf("%v %v %v",
				where.column, where.operator, where.value))
		} else {
			conditions = append(conditions, fmt.Sprintf("%v %v ?",
				where.column, where.operator))
			b.bindings = append(b.bindings, where.value)
		}
	}

	joinCondition := strings.Join(conditions, " AND ")
	b.joins = append(b.joins, join{
		table:     table,
		condition: joinCondition,
		joinType:  "INNER",
	})

	return b
}

// HavingFunc adds a HAVING clause using a callback function
func (b *Builder) HavingFunc(fn QueryFunc) *Builder {
	subBuilder := New(b.db)
	fn(subBuilder)

	for _, where := range subBuilder.wheres {
		b.havings = append(b.havings, having{
			column:   where.column,
			operator: where.operator,
			value:    where.value,
			boolean:  where.boolean,
		})
	}
	b.bindings = append(b.bindings, subBuilder.bindings...)
	return b
}

// WhereNested adds a nested WHERE clause
func (b *Builder) WhereNested(callback func(*Builder)) *Builder {
	subBuilder := New(b.db)
	callback(subBuilder)

	if len(subBuilder.wheres) > 0 {
		b.wheres = append(b.wheres, where{
			column:   "(" + subBuilder.whereSQL() + ")",
			operator: "",
			value:    "",
			boolean:  "AND",
		})
		b.bindings = append(b.bindings, subBuilder.bindings...)
	}

	return b
}

// UnionType represents the type of UNION operation
type UnionType int

const (
	UnionNormal UnionType = iota
	UnionAll
)

// union represents a UNION query
type union struct {
	query *Builder
	typ   UnionType
}

// Union adds a UNION clause
func (b *Builder) Union(query *Builder) *Builder {
	b.unions = append(b.unions, union{
		query: query,
		typ:   UnionNormal,
	})
	return b
}

// UnionAll adds a UNION ALL clause
func (b *Builder) UnionAll(query *Builder) *Builder {
	b.unions = append(b.unions, union{
		query: query,
		typ:   UnionAll,
	})
	return b
}

// When adds conditions based on a boolean value
func (b *Builder) When(condition bool, callback func(*Builder)) *Builder {
	if condition {
		callback(b)
	}
	return b
}

// WhenNot adds conditions when boolean is false
func (b *Builder) WhenNot(condition bool, callback func(*Builder)) *Builder {
	if !condition {
		callback(b)
	}
	return b
}

// Unless is an alias for WhenNot
func (b *Builder) Unless(condition bool, callback func(*Builder)) *Builder {
	return b.WhenNot(condition, callback)
}

// Debug returns the query with interpolated values
func (b *Builder) Debug() string {
	sql := b.ToSQL()
	for _, binding := range b.bindings {
		sql = strings.Replace(sql, "?", fmt.Sprintf("%v", binding), 1)
	}
	return sql
}

// Explain returns the query execution plan
func (b *Builder) Explain() (string, error) {
	ctx := context.Background()
	rows, err := b.db.QueryContext(ctx, "EXPLAIN "+b.ToSQL(), b.bindings...)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var explanation strings.Builder
	cols, err := rows.Columns()
	if err != nil {
		return "", err
	}

	vals := make([]interface{}, len(cols))
	for i := range vals {
		vals[i] = new(sql.RawBytes)
	}

	for rows.Next() {
		if err := rows.Scan(vals...); err != nil {
			return "", err
		}
		for i, col := range cols {
			explanation.WriteString(fmt.Sprintf("%s: %s\n", col, vals[i].(*sql.RawBytes)))
		}
	}

	return explanation.String(), nil
}

// GetBindings returns the current query bindings
func (b *Builder) GetBindings() []interface{} {
	return b.bindings
}

// Schema operations
type SchemaBuilder struct {
	columns map[string]string
	indexes map[string][]string
}

func NewSchemaBuilder() *SchemaBuilder {
	return &SchemaBuilder{
		columns: make(map[string]string),
		indexes: make(map[string][]string),
	}
}

// CreateTable creates a new table
func (b *Builder) CreateTable(name string, callback func(*SchemaBuilder)) error {
	schema := NewSchemaBuilder()
	callback(schema)

	var cols []string
	for col, typ := range schema.columns {
		cols = append(cols, fmt.Sprintf("%s %s", col, typ))
	}

	query := fmt.Sprintf("CREATE TABLE %s (\n%s\n)", name, strings.Join(cols, ",\n"))
	_, err := b.db.ExecContext(context.Background(), query)
	return err
}

// Query events
type QueryEvent struct {
	SQL      string
	Bindings []interface{}
	Duration time.Duration
}

type QueryEventHandler func(*QueryEvent)

// BeforeQuery adds a before query event handler
func (b *Builder) BeforeQuery(handler QueryEventHandler) *Builder {
	b.beforeQueryHandlers = append(b.beforeQueryHandlers, handler)
	return b
}

// AfterQuery adds an after query event handler
func (b *Builder) AfterQuery(handler QueryEventHandler) *Builder {
	b.afterQueryHandlers = append(b.afterQueryHandlers, handler)
	return b
}

// Batch processing
type Paginator struct {
	Items       []map[string]interface{}
	Total       int64
	PerPage     int
	CurrentPage int
	LastPage    int
}

// Paginate returns paginated results
func (b *Builder) Paginate(page, perPage int) (*Paginator, error) {
	ctx := context.Background()

	// Get total count
	countBuilder := *b
	count, err := countBuilder.Count("*").Get(ctx)
	if err != nil {
		return nil, err
	}

	var total int64

	// count total
	if count.Next() {
		count.Scan(&total)
	}

	// Get paginated results
	offset := (page - 1) * perPage
	rows, err := b.Limit(perPage).Offset(offset).Get(ctx)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []map[string]interface{}
	cols, _ := rows.Columns()
	for rows.Next() {
		item := make(map[string]interface{})
		vals := make([]interface{}, len(cols))
		for i := range vals {
			vals[i] = new(interface{})
		}
		if err := rows.Scan(vals...); err != nil {
			return nil, err
		}
		for i, col := range cols {
			item[col] = *vals[i].(*interface{})
		}
		items = append(items, item)
	}

	return &Paginator{
		Items:       items,
		Total:       total,
		PerPage:     perPage,
		CurrentPage: page,
		LastPage:    int(math.Ceil(float64(total) / float64(perPage))),
	}, nil
}
