package qix

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

// MockDB implements DB interface for testing
type MockDB struct {
	queryFunc func(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	execFunc  func(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

func (m *MockDB) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	if m.queryFunc != nil {
		return m.queryFunc(ctx, query, args...)
	}
	return nil, nil
}

func (m *MockDB) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if m.execFunc != nil {
		return m.execFunc(ctx, query, args...)
	}
	return nil, nil
}

// MockResult implements sql.Result for testing
type MockResult struct {
	lastID       int64
	rowsAffected int64
}

func (m MockResult) LastInsertId() (int64, error) {
	return m.lastID, nil
}

func (m MockResult) RowsAffected() (int64, error) {
	return m.rowsAffected, nil
}

// MockTx implements sql.Tx interface for testing
type MockTx struct {
	MockDB
	committed  bool
	rolledBack bool
}

func (m *MockTx) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return m.MockDB.QueryContext(ctx, query, args...)
}

func (m *MockTx) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return m.MockDB.ExecContext(ctx, query, args...)
}

func (m *MockTx) Commit() error {
	m.committed = true
	return nil
}

func (m *MockTx) Rollback() error {
	m.rolledBack = true
	return nil
}

// MockTxDB implements TxDB interface for testing
type MockTxDB struct {
	MockDB
	tx *MockTx
}

func (m *MockTxDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (DB, error) {
	m.tx = &MockTx{
		MockDB: m.MockDB,
	}
	return m.tx, nil
}

func TestNew(t *testing.T) {
	db := &MockDB{}
	builder := New(db)
	if builder == nil {
		t.Error("Expected builder to not be nil")
	}
}

func TestTable(t *testing.T) {
	db := &MockDB{}
	builder := New(db)
	builder.Table("users")
	if builder.table != "users" {
		t.Errorf("Expected table to be 'users', got '%s'", builder.table)
	}
}

func TestJoin(t *testing.T) {
	db := &MockDB{}
	builder := New(db)
	builder.Table("users").
		Join("orders", "users.id = orders.user_id")

	if len(builder.joins) != 1 {
		t.Error("Expected joins to have 1 item")
	}

	join := builder.joins[0]
	if join.joinType != "INNER" {
		t.Errorf("Expected join type to be 'INNER', got '%s'", join.joinType)
	}
}

func TestGroupByAndHaving(t *testing.T) {
	db := &MockDB{}
	builder := New(db)
	builder.Table("orders").
		GroupBy("status").
		Having("count", ">", 100)

	if len(builder.groups) != 1 {
		t.Error("Expected groups to have 1 item")
	}

	if len(builder.havings) != 1 {
		t.Error("Expected havings to have 1 item")
	}
}

func TestOrderByAndLimit(t *testing.T) {
	db := &MockDB{}
	builder := New(db)
	limit := 10
	builder.Table("users").
		OrderBy("name", "ASC").
		Limit(limit).
		Offset(20)

	if len(builder.orders) != 1 {
		t.Error("Expected orders to have 1 item")
	}

	if *builder.limit != limit {
		t.Errorf("Expected limit to be %d", limit)
	}
}

func TestAggregateFunctions(t *testing.T) {
	db := &MockDB{}
	builder := New(db)
	builder.Table("orders").Count("id")

	if len(builder.columns) != 1 {
		t.Error("Expected columns to have 1 item")
	}

	if builder.columns[0] != "COUNT(id)" {
		t.Errorf("Expected COUNT(id), got %s", builder.columns[0])
	}
}

func TestToSQL(t *testing.T) {
	db := &MockDB{}
	tests := []struct {
		name     string
		build    func() *Builder
		expected string
	}{
		{
			name: "Simple Select",
			build: func() *Builder {
				return New(db).Table("users").Select("id", "name")
			},
			expected: "SELECT id, name FROM users",
		},
		{
			name: "Select with Where",
			build: func() *Builder {
				return New(db).Table("users").
					Select("id", "name").
					Where("age", ">", 18)
			},
			expected: "SELECT id, name FROM users WHERE age > ?",
		},
		{
			name: "Complex Query",
			build: func() *Builder {
				return New(db).Table("users").
					Select("users.id", "users.name", "COUNT(orders.id) as order_count").
					LeftJoin("orders", "users.id = orders.user_id").
					Where("users.age", ">", 18).
					GroupBy("users.id").
					Having("order_count", ">", 5).
					OrderBy("users.name", "ASC").
					Limit(10)
			},
			expected: "SELECT users.id, users.name, COUNT(orders.id) as order_count FROM users LEFT JOIN orders ON users.id = orders.user_id WHERE users.age > ? GROUP BY users.id HAVING order_count > ? ORDER BY users.name ASC LIMIT ?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := tt.build()
			sql := builder.ToSQL()
			if sql != tt.expected {
				t.Errorf("Expected SQL: %s\nGot: %s", tt.expected, sql)
			}
		})
	}
}

func TestWhereHelpers(t *testing.T) {
	db := &MockDB{}
	tests := []struct {
		name     string
		build    func() *Builder
		expected string
	}{
		{
			name: "WhereIn",
			build: func() *Builder {
				return New(db).Table("users").WhereIn("id", 1, 2, 3)
			},
			expected: "SELECT * FROM users WHERE id IN (?, ?, ?)",
		},
		{
			name: "WhereNotIn",
			build: func() *Builder {
				return New(db).Table("users").WhereNotIn("status", "pending", "failed")
			},
			expected: "SELECT * FROM users WHERE status NOT IN (?, ?)",
		},
		{
			name: "WhereNull",
			build: func() *Builder {
				return New(db).Table("users").WhereNull("deleted_at")
			},
			expected: "SELECT * FROM users WHERE deleted_at IS NULL",
		},
		{
			name: "WhereNotNull",
			build: func() *Builder {
				return New(db).Table("users").WhereNotNull("email_verified_at")
			},
			expected: "SELECT * FROM users WHERE email_verified_at IS NOT NULL",
		},
		{
			name: "WhereBetween",
			build: func() *Builder {
				return New(db).Table("orders").WhereBetween("created_at", "2023-01-01", "2023-12-31")
			},
			expected: "SELECT * FROM orders WHERE created_at BETWEEN ? AND ?",
		},
		{
			name: "Complex Where Conditions",
			build: func() *Builder {
				return New(db).Table("users").
					Where("age", ">", 18).
					WhereNull("deleted_at").
					OrWhere("role", "=", "admin")
			},
			expected: "SELECT * FROM users WHERE age > ? AND deleted_at IS NULL OR role = ?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := tt.build()
			sql := builder.ToSQL()
			if sql != tt.expected {
				t.Errorf("Expected SQL: %s\nGot: %s", tt.expected, sql)
			}
		})
	}
}

func TestDateWhereHelpers(t *testing.T) {
	db := &MockDB{}
	tests := []struct {
		name     string
		build    func() *Builder
		expected string
	}{
		{
			name: "WhereDate",
			build: func() *Builder {
				return New(db).Table("orders").WhereDate("created_at", "=", "2023-01-01")
			},
			expected: "SELECT * FROM orders WHERE DATE(created_at) = ?",
		},
		{
			name: "WhereYear",
			build: func() *Builder {
				return New(db).Table("orders").WhereYear("created_at", "=", 2023)
			},
			expected: "SELECT * FROM orders WHERE YEAR(created_at) = ?",
		},
		{
			name: "WhereMonth",
			build: func() *Builder {
				return New(db).Table("orders").WhereMonth("created_at", "=", 1)
			},
			expected: "SELECT * FROM orders WHERE MONTH(created_at) = ?",
		},
		{
			name: "WhereDay",
			build: func() *Builder {
				return New(db).Table("orders").WhereDay("created_at", "=", 15)
			},
			expected: "SELECT * FROM orders WHERE DAY(created_at) = ?",
		},
		{
			name: "WhereColumn",
			build: func() *Builder {
				return New(db).Table("users").WhereColumn("updated_at", ">", "created_at")
			},
			expected: "SELECT * FROM users WHERE updated_at > created_at",
		},
		{
			name: "Complex Date Conditions",
			build: func() *Builder {
				return New(db).Table("orders").
					WhereYear("created_at", "=", 2023).
					WhereMonth("created_at", "IN", []int{1, 2, 3}).
					OrWhereColumn("updated_at", ">", "created_at")
			},
			expected: "SELECT * FROM orders WHERE YEAR(created_at) = ? AND MONTH(created_at) IN (?, ?, ?) OR updated_at > created_at",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := tt.build()
			sql := builder.ToSQL()
			if sql != tt.expected {
				t.Errorf("Expected SQL: %s\nGot: %s", tt.expected, sql)
			}
		})
	}
}

func TestQueryContext(t *testing.T) {
	ctx := context.Background()
	mockDB := &MockDB{
		execFunc: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return MockResult{lastID: 1, rowsAffected: 1}, nil
		},
	}

	tests := []struct {
		name string
		test func(t *testing.T)
	}{
		{
			name: "Insert with Context",
			test: func(t *testing.T) {
				builder := New(mockDB)
				data := map[string]interface{}{
					"name": "John",
					"age":  25,
				}
				id, err := builder.Table("users").InsertGetId(ctx, data)
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if id != 1 {
					t.Errorf("Expected id 1, got %d", id)
				}
			},
		},
		{
			name: "Update with Context",
			test: func(t *testing.T) {
				builder := New(mockDB)
				data := map[string]interface{}{
					"age": 26,
				}
				affected, err := builder.Table("users").
					Where("id", "=", 1).
					UpdateWithContext(ctx, data)
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if affected != 1 {
					t.Errorf("Expected 1 row affected, got %d", affected)
				}
			},
		},
		{
			name: "Delete with Context",
			test: func(t *testing.T) {
				builder := New(mockDB)
				affected, err := builder.Table("users").
					Where("id", "=", 1).
					DeleteWithContext(ctx)
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if affected != 1 {
					t.Errorf("Expected 1 row affected, got %d", affected)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.test)
	}
}

func TestTransaction(t *testing.T) {
	ctx := context.Background()
	mockDB := &MockTxDB{}
	builder := New(mockDB)

	t.Run("Successful Transaction", func(t *testing.T) {
		err := builder.Transaction(ctx, func(tx *Builder) error {
			data := map[string]interface{}{
				"name": "John",
			}
			_, err := tx.Table("users").InsertGetId(ctx, data)
			return err
		})

		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if !mockDB.tx.committed {
			t.Error("Expected transaction to be committed")
		}
	})

	t.Run("Failed Transaction", func(t *testing.T) {
		mockDB := &MockTxDB{}
		builder := New(mockDB)

		err := builder.Transaction(ctx, func(tx *Builder) error {
			return fmt.Errorf("some error")
		})

		if err == nil {
			t.Error("Expected error, got nil")
		}
		if !mockDB.tx.rolledBack {
			t.Error("Expected transaction to be rolled back")
		}
	})
}

func TestBatchOperations(t *testing.T) {
	ctx := context.Background()
	mockDB := &MockDB{
		execFunc: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return MockResult{rowsAffected: int64(len(args))}, nil
		},
	}

	t.Run("BatchInsert", func(t *testing.T) {
		builder := New(mockDB)
		data := []map[string]interface{}{
			{"name": "John", "age": 25},
			{"name": "Jane", "age": 30},
		}
		err := builder.Table("users").BatchInsert(ctx, data)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
	})

	t.Run("BulkUpdate", func(t *testing.T) {
		builder := New(mockDB)
		data := []map[string]interface{}{
			{"id": 1, "status": "active"},
			{"id": 2, "status": "inactive"},
		}
		err := builder.Table("users").BulkUpdate(ctx, data, "id")
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
	})
}

func TestAdvancedJoins(t *testing.T) {
	db := &MockDB{}
	tests := []struct {
		name     string
		build    func() *Builder
		expected string
	}{
		{
			name: "RightJoin",
			build: func() *Builder {
				return New(db).Table("users").
					RightJoin("orders", "users.id = orders.user_id")
			},
			expected: "SELECT * FROM users RIGHT JOIN orders ON users.id = orders.user_id",
		},
		{
			name: "CrossJoin",
			build: func() *Builder {
				return New(db).Table("users").
					CrossJoin("permissions")
			},
			expected: "SELECT * FROM users CROSS JOIN permissions",
		},
		{
			name: "JoinSub",
			build: func() *Builder {
				sub := New(db).Table("orders").Select("user_id", "COUNT(*) as order_count").GroupBy("user_id")
				return New(db).Table("users").
					JoinSub(sub, "user_orders", "users.id = user_orders.user_id")
			},
			expected: "SELECT * FROM users INNER JOIN (SELECT user_id, COUNT(*) as order_count FROM orders GROUP BY user_id) AS user_orders ON users.id = user_orders.user_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql := tt.build().ToSQL()
			if sql != tt.expected {
				t.Errorf("Expected SQL: %s\nGot: %s", tt.expected, sql)
			}
		})
	}
}

func TestAdvancedWhere(t *testing.T) {
	db := &MockDB{}
	tests := []struct {
		name     string
		build    func() *Builder
		expected string
	}{
		{
			name: "WhereFunc",
			build: func() *Builder {
				return New(db).Table("users").WhereFunc(func(q *Builder) {
					q.Where("age", ">", 18).Where("status", "=", "active")
				})
			},
			expected: "SELECT * FROM users WHERE age > ? AND status = ?",
		},
		{
			name: "OrWhereFunc",
			build: func() *Builder {
				return New(db).Table("users").
					Where("role", "=", "admin").
					OrWhereFunc(func(q *Builder) {
						q.Where("age", ">", 18).Where("status", "=", "active")
					})
			},
			expected: "SELECT * FROM users WHERE role = ? OR age > ? AND status = ?",
		},
		{
			name: "WhereNested",
			build: func() *Builder {
				return New(db).Table("users").
					Where("role", "=", "user").
					WhereNested(func(q *Builder) {
						q.Where("age", ">", 18).OrWhere("vip", "=", true)
					})
			},
			expected: "SELECT * FROM users WHERE role = ? AND (age > ? OR vip = ?)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql := tt.build().ToSQL()
			t.Logf("Query: %s", sql)
			if sql != tt.expected {
				t.Errorf("Expected SQL: %s\nGot: %s", tt.expected, sql)
			}
		})
	}
}

func TestQueryFunctions(t *testing.T) {
	db := &MockDB{}
	tests := []struct {
		name     string
		build    func() *Builder
		expected string
	}{
		{
			name: "WhereFunc Complex Condition",
			build: func() *Builder {
				return New(db).Table("users").WhereFunc(func(q *Builder) {
					q.Where("age", ">", 18).
						Where("status", "=", "active").
						OrWhere("role", "=", "admin")
				})
			},
			expected: "SELECT * FROM users WHERE age > ? AND status = ? OR role = ?",
		},
		{
			name: "OrWhereFunc Group",
			build: func() *Builder {
				return New(db).Table("users").
					Where("department", "=", "IT").
					OrWhereFunc(func(q *Builder) {
						q.Where("age", ">", 25).
							Where("experience", ">", 5)
					})
			},
			expected: "SELECT * FROM users WHERE department = ? OR age > ? AND experience > ?",
		},
		{
			name: "JoinFunc Complex Condition",
			build: func() *Builder {
				return New(db).Table("users").
					JoinFunc("orders", func(q *Builder) {
						q.WhereColumn("users.id", "=", "orders.user_id").
							Where("orders.status", "=", "completed")
					})
			},
			expected: "SELECT * FROM users INNER JOIN orders ON users.id = orders.user_id AND orders.status = ?",
		},
		{
			name: "HavingFunc Complex Condition",
			build: func() *Builder {
				return New(db).Table("users").
					Select("department", "AVG(salary) as avg_salary").
					GroupBy("department").
					HavingFunc(func(q *Builder) {
						q.Where("avg_salary", ">", 50000).
							Where("COUNT(*)", ">", 5)
					})
			},
			expected: "SELECT department, AVG(salary) as avg_salary FROM users GROUP BY department HAVING avg_salary > ? AND COUNT(*) > ?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql := tt.build().ToSQL()
			if sql != tt.expected {
				t.Errorf("Expected SQL: %s\nGot: %s", tt.expected, sql)
			}
		})
	}
}

func TestUnionQueries(t *testing.T) {
	db := &MockDB{}
	tests := []struct {
		name     string
		build    func() *Builder
		expected string
	}{
		{
			name: "Simple Union",
			build: func() *Builder {
				users := New(db).Table("users").Where("role", "=", "admin")
				staff := New(db).Table("staff").Where("department", "=", "IT")
				return users.Union(staff)
			},
			expected: "SELECT * FROM users WHERE role = ? UNION SELECT * FROM staff WHERE department = ?",
		},
		{
			name: "Union All",
			build: func() *Builder {
				q1 := New(db).Table("orders").Where("status", "=", "pending")
				q2 := New(db).Table("archived_orders").Where("status", "=", "pending")
				return q1.UnionAll(q2)
			},
			expected: "SELECT * FROM orders WHERE status = ? UNION ALL SELECT * FROM archived_orders WHERE status = ?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql := tt.build().ToSQL()
			if sql != tt.expected {
				t.Errorf("Expected SQL: %s\nGot: %s", tt.expected, sql)
			}
		})
	}
}

func TestConditionalQueries(t *testing.T) {
	db := &MockDB{}
	hasRole := true
	hasDept := false

	builder := New(db).Table("users").
		When(hasRole, func(q *Builder) {
			q.Where("role", "=", "admin")
		}).
		When(hasDept, func(q *Builder) {
			q.Where("department", "=", "IT")
		})

	expected := "SELECT * FROM users WHERE role = ?"
	sql := builder.ToSQL()
	t.Logf("Query: %s", sql)
	if sql != expected {
		t.Errorf("Expected SQL: %s\nGot: %s", expected, sql)
	}
}

func TestPagination(t *testing.T) {
	db := &MockDB{
		queryFunc: func(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
			// Mock implementation
			return nil, nil
		},
	}

	builder := New(db).Table("users").Where("active", "=", true)
	paginator, err := builder.Paginate(1, 20)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if paginator.PerPage != 20 {
		t.Errorf("Expected per page to be 20, got %d", paginator.PerPage)
	}
}
