package qix

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// Test models for relationship tests

// Post model for testing relationships
type Post struct {
	ID        int       `db:"id,pk,auto"`
	UserID    int       `db:"user_id"`
	Title     string    `db:"title"`
	Content   string    `db:"content"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
	User      Gamer     `db:"user_id" rel:"belongsTo"` // Relation to User
	Comments  []Comment `rel:"hasMany,foreignKey:post_id"`
	Tags      []Tag     `rel:"manyToMany,pivot:post_tags,pivotFk:post_id,pivotRfk:tag_id"`
}

// User model for testing relationships
type Gamer struct {
	ID        int       `db:"id,pk,auto"`
	Name      string    `db:"name"`
	Email     string    `db:"email"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
	Posts     []Post    `rel:"hasMany,foreignKey:user_id"`
	Profile   Avatar    `rel:"hasOne,foreignKey:user_id"`
}

// Comment model for testing relationships
type Comment struct {
	ID        int       `db:"id,pk,auto"`
	PostID    int       `db:"post_id"`
	UserID    int       `db:"user_id"`
	Content   string    `db:"content"`
	CreatedAt time.Time `db:"created_at"`
	Post      Post      `rel:"belongsTo,foreignKey:post_id"`
	User      Gamer     `rel:"belongsTo,foreignKey:user_id"`
}

// Profile model for testing relationships
type Avatar struct {
	ID        int       `db:"id,pk,auto"`
	UserID    int       `db:"user_id"`
	Bio       string    `db:"bio"`
	Image     string    `db:"image"`
	CreatedAt time.Time `db:"created_at"`
	User      *Gamer    `rel:"belongsTo,foreignKey:user_id"`
}

// Tag model for testing relationships
type Tag struct {
	ID    int     `db:"id,pk,auto"`
	Name  string  `db:"name"`
	Posts []*Post `rel:"manyToMany,pivot:post_tags,pivotFk:tag_id,pivotRfk:post_id"`
}

// Test relation parsing and detection
func TestModelRelationParsing(t *testing.T) {
	db := &MockDB{}

	// Test Post model relations
	post := Post{}
	postModel, err := NewModel(db, &post)
	if err != nil {
		t.Fatalf("Failed to create post model: %v", err)
	}

	// Find relation fields
	userRelation := findRelationField(postModel.fields, "User")
	if userRelation == nil {
		t.Fatal("User relation not found in Post model")
	}

	if userRelation.relation.relType != relationBelongsTo {
		t.Errorf("Expected User relation to be belongsTo, got %v", userRelation.relation.relType)
	}

	// Check Comments relation
	commentsRelation := findRelationField(postModel.fields, "Comments")
	if commentsRelation == nil {
		t.Fatal("Comments relation not found in Post model")
	}

	if commentsRelation.relation.relType != relationHasMany {
		t.Errorf("Expected Comments relation to be hasMany, got %v", commentsRelation.relation.relType)
	}

	if commentsRelation.relation.foreignKey != "post_id" {
		t.Errorf("Expected Comments foreignKey to be post_id, got %s", commentsRelation.relation.foreignKey)
	}

	// Check Tags relation (many-to-many)
	tagsRelation := findRelationField(postModel.fields, "Tags")
	if tagsRelation == nil {
		t.Fatal("Tags relation not found in Post model")
	}

	if tagsRelation.relation.relType != relationManyToMany {
		t.Errorf("Expected Tags relation to be manyToMany, got %v", tagsRelation.relation.relType)
	}

	if tagsRelation.relation.pivot != "post_tags" {
		t.Errorf("Expected Tags pivot table to be post_tags, got %s", tagsRelation.relation.pivot)
	}
}

// Test eager loading with a simple query
func TestModelEagerLoading(t *testing.T) {
	// ctx := context.Background()

	// Set up mock for Find with eager loading
	mockDB := &MockDB{
		queryFunc: func(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
			// This is a simplified test to check query structure
			// We're not returning actual rows since that would require more complex mocking

			// If query contains JOIN, it's likely an eager loading query
			// Just check that the query looks reasonable
			// For real tests, we'd inspect the query structure more carefully

			return nil, nil
		},
	}

	post := Post{}
	postModel, _ := NewModel(mockDB, &post)

	// Try With() to eager load
	postModelWithUser := postModel.With("User")

	// Verify the With method properly set up eager loading
	if _, exists := postModelWithUser.eagerLoad["User"]; !exists {
		t.Error("Expected User to be in eagerLoad map after With() call")
	}

	// Test cascading With calls
	postModelWithMultiple := postModel.With("User").With("Comments")

	if len(postModelWithMultiple.eagerLoad) != 2 {
		t.Errorf("Expected 2 relations in eagerLoad map, got %d", len(postModelWithMultiple.eagerLoad))
	}

	if _, exists := postModelWithMultiple.eagerLoad["User"]; !exists {
		t.Error("Expected User to be in eagerLoad map after cascading With() calls")
	}

	if _, exists := postModelWithMultiple.eagerLoad["Comments"]; !exists {
		t.Error("Expected Comments to be in eagerLoad map after cascading With() calls")
	}
}

// Test BelongsTo relationship
func TestModelBelongsToRelationship(t *testing.T) {
	ctx := context.Background()
	mockDB := &MockDB{}

	comment := Comment{PostID: 1}
	commentModel, _ := NewModel(mockDB, comment)

	// Test BelongsTo relationship
	postQuery, err := commentModel.BelongsTo(ctx, &Post{}, "post_id", "id")
	if err != nil {
		t.Fatalf("BelongsTo failed: %v", err)
	}

	// Check the generated query
	sql := postQuery.ToSQL()
	expectedSQL := "SELECT * FROM post WHERE id = ?"
	if sql != expectedSQL {
		t.Errorf("Expected SQL: %s, got: %s", expectedSQL, sql)
	}
}

// Test HasMany relationship
func TestModelHasManyRelationship(t *testing.T) {
	ctx := context.Background()
	mockDB := &MockDB{}

	post := Post{ID: 1}
	postModel, _ := NewModel(mockDB, &post)

	// Test HasMany relationship
	commentsQuery, err := postModel.HasMany(ctx, &Comment{}, "post_id", "id")
	if err != nil {
		t.Fatalf("HasMany failed: %v", err)
	}

	// Check the generated query
	sql := commentsQuery.ToSQL()
	expectedSQL := "SELECT * FROM comment WHERE post_id = ?"
	if sql != expectedSQL {
		t.Errorf("Expected SQL: %s, got: %s", expectedSQL, sql)
	}
}

// Test BelongsToMany relationship
func TestModelBelongsToManyRelationship(t *testing.T) {
	ctx := context.Background()
	mockDB := &MockDB{}

	post := Post{ID: 1}
	postModel, _ := NewModel(mockDB, &post)

	// Test BelongsToMany relationship
	tagsQuery, err := postModel.BelongsToMany(ctx, &Tag{}, "post_tags", "post_id", "tag_id")
	if err != nil {
		t.Fatalf("BelongsToMany failed: %v", err)
	}

	// Check the generated query
	sql := tagsQuery.ToSQL()
	// Should generate a join query
	if !strings.Contains(sql, "JOIN post_tags") {
		t.Errorf("Expected SQL to contain JOIN post_tags, got: %s", sql)
	}

	t.Log(sql)
}

// Test nested transactions
func TestModelNestedTransactions(t *testing.T) {
	ctx := context.Background()

	// This test would require a more sophisticated mock to fully test savepoints
	// For now, just verify the code doesn't panic

	mockTx := &MockTx{
		MockDB: MockDB{
			execFunc: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
				// For nested transactions, we should see SAVEPOINT commands
				if strings.Contains(query, "SAVEPOINT") {
					return MockResult{}, nil
				}
				return MockResult{lastID: 1, rowsAffected: 1}, nil
			},
		},
	}

	// Create a model that's already in a transaction
	post := Post{}
	postModel, _ := NewModel(mockTx, &post)

	// Run a nested transaction
	err := postModel.Transaction(ctx, func(model *Model) error {
		// Do something in the nested transaction
		return nil
	})

	if err != nil {
		t.Fatalf("Nested transaction failed: %v", err)
	}
}

// Helper function to find a relation field by name
func findRelationField(fields []Field, name string) *Field {
	for _, f := range fields {
		if f.name == name && f.relation != nil {
			return &f
		}
	}
	return nil
}
