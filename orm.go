package qix

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Model represents a database model with ORM capabilities
type Model struct {
	builder    *Builder
	value      interface{}
	table      string
	pk         string
	fields     []Field
	eagerLoad  map[string]func(*Builder) *Builder // Eager loading callbacks
	preloaded  map[string]interface{}             // Preloaded relations
	isPreload  bool                               // Whether the model is being used for preloading
	relManager *relationManager                   // For handling relationships
}

// relationManager manages model relationships
type relationManager struct {
	db         DB
	registry   map[reflect.Type]*Model // Map of registered models
	modelCache map[string]*Model       // Cache of models by table name
}

// field represents a struct field mapped to a database column
type Field struct {
	name     string    // Go field name
	column   string    // DB column name
	isPK     bool      // Is primary key
	isAuto   bool      // Is auto-increment
	omitZero bool      // Omit zero values
	omit     bool      // Omit from operations
	relation *relation // Relation information if field is a relation
}

// relation defines a relationship between models
type relation struct {
	relType     relationshipType // Type of relationship (hasOne, hasMany, belongsTo, etc)
	foreignKey  string           // Foreign key column name
	localKey    string           // Local key column name
	modelType   reflect.Type     // Type of the related model
	targetTable string           // Table name of the related model
	pivot       string           // Pivot table for many-to-many
	pivotFk     string           // Pivot foreign key
	pivotRfk    string           // Pivot related foreign key
}

// relationshipType defines types of relationships
type relationshipType int

const (
	relationHasOne relationshipType = iota
	relationHasMany
	relationBelongsTo
	relationManyToMany
)

// Global relation manager
var globalRelManager = &relationManager{
	registry:   make(map[reflect.Type]*Model),
	modelCache: make(map[string]*Model),
}

// NewModel creates a new ORM model
func NewModel(db DB, value interface{}) (*Model, error) {
	// Set the DB for the relation manager if not already set
	if globalRelManager.db == nil {
		globalRelManager.db = db
	}

	m := &Model{
		builder:    New(db),
		value:      value,
		pk:         "id", // Default primary key
		eagerLoad:  make(map[string]func(*Builder) *Builder),
		preloaded:  make(map[string]interface{}),
		relManager: globalRelManager,
		isPreload:  false,
	}

	err := m.parseStruct()
	if err != nil {
		return nil, err
	}

	// Register model with relation manager
	valueType := reflect.TypeOf(value)
	if valueType.Kind() == reflect.Ptr {
		valueType = valueType.Elem()
	}
	globalRelManager.registry[valueType] = m
	globalRelManager.modelCache[m.table] = m

	return m, nil
}

// parseStruct analyzes the struct and extracts field mapping information
func (m *Model) parseStruct() error {
	v := reflect.ValueOf(m.value)

	// Handle pointer types
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	// Ensure we're working with a struct
	if v.Kind() != reflect.Struct {
		return errors.New("model must be a struct or pointer to struct")
	}

	t := v.Type()

	// Get table name from struct type if not set
	if m.table == "" {
		m.table = toSnakeCase(t.Name())
	}

	// Parse fields
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Get field tag
		tag := field.Tag.Get("db")
		if tag == "-" {
			continue
		}

		// Parse tag options
		options := strings.Split(tag, ",")
		column := options[0]

		// If no column name specified, use field name
		if column == "" {
			column = toSnakeCase(field.Name)
		}

		f := Field{
			name:   field.Name,
			column: column,
		}

		// Parse options
		for _, opt := range options[1:] {
			switch opt {
			case "pk":
				f.isPK = true
				m.pk = column
			case "auto":
				f.isAuto = true
			case "omitempty":
				f.omitZero = true
			case "omit":
				f.omit = true
			}
		}

		// Check for relationship tag
		relTag := field.Tag.Get("rel")
		if relTag != "" {
			rel, err := m.parseRelationTag(relTag, field)
			if err != nil {
				return fmt.Errorf("invalid relation tag for field %s: %w", field.Name, err)
			}
			f.relation = rel
		} else {
			// Check if field is a struct or slice of structs (potential relation)
			fieldType := field.Type
			if fieldType.Kind() == reflect.Ptr {
				fieldType = fieldType.Elem()
			}

			// Auto-detect relationships based on type
			if fieldType.Kind() == reflect.Struct && fieldType != reflect.TypeOf(time.Time{}) {
				// Potential belongsTo or hasOne relationship
				rel := &relation{
					modelType: fieldType,
				}

				// Try to determine relationship type and keys
				fieldTypeName := fieldType.Name()
				if strings.HasSuffix(field.Name, fieldTypeName) {
					// Field name ends with type name, likely a belongsTo
					rel.relType = relationBelongsTo
					rel.foreignKey = toSnakeCase(field.Name) + "_id"
					rel.localKey = "id"
					rel.targetTable = toSnakeCase(fieldTypeName)
				} else {
					// Otherwise, assume hasOne
					rel.relType = relationHasOne
					rel.foreignKey = toSnakeCase(t.Name()) + "_id"
					rel.localKey = "id"
					rel.targetTable = toSnakeCase(fieldTypeName)
				}

				f.relation = rel
			} else if fieldType.Kind() == reflect.Slice || fieldType.Kind() == reflect.Array {
				// Potential hasMany or manyToMany relationship
				elemType := fieldType.Elem()
				if elemType.Kind() == reflect.Ptr {
					elemType = elemType.Elem()
				}

				if elemType.Kind() == reflect.Struct {
					rel := &relation{
						modelType: elemType,
						relType:   relationHasMany,
					}

					// Try to determine keys
					rel.foreignKey = toSnakeCase(t.Name()) + "_id"
					rel.localKey = "id"
					rel.targetTable = toSnakeCase(elemType.Name())

					// Check for potential many-to-many
					singularName := getSingular(field.Name)
					pivotTable := toSnakeCase(singularName) + "_" + toSnakeCase(t.Name())
					rel.pivot = pivotTable
					rel.pivotFk = singularName + "_id"
					// rel.pivotRfk = toSnakeCase(t.Name()) + "_id"
					// This is a heuristic - many-to-many relations should be explicitly defined with tags

					f.relation = rel
				}
			}
		}

		m.fields = append(m.fields, f)
	}

	return nil
}

// getPkFieldName gets the field name corresponding to a given column name
func getPkFieldName(fields []Field, colName string) string {
	for _, field := range fields {
		if field.column == colName && field.isPK {
			return field.name
		}
	}
	return "ID" // Default assumption for primary key field name
}

// All retrieves all records
func (m *Model) All(ctx context.Context) (interface{}, error) {
	// Create a slice of the model type
	sliceType := reflect.SliceOf(reflect.TypeOf(m.value))
	results := reflect.MakeSlice(sliceType, 0, 0)

	// Build query
	rows, err := m.builder.Table(m.table).Get(ctx)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Iterate through results
	for rows.Next() {
		// Create a new instance of the model
		result := reflect.New(reflect.TypeOf(m.value)).Elem()

		// Map columns to struct fields
		if err := m.scanRow(rows, result); err != nil {
			return nil, err
		}

		// Append to results slice
		results = reflect.Append(results, result)
	}

	return results.Interface(), nil
}

// Find finds a record by primary key
func (m *Model) Find(ctx context.Context, id interface{}) (interface{}, error) {
	result := reflect.New(reflect.TypeOf(m.value)).Interface()

	// Build query
	rows, err := m.builder.Table(m.table).
		Where(m.pk, "=", id).
		Limit(1).
		Get(ctx)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Check if record exists
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}

	// Map columns to struct fields
	if err := m.scanInto(rows, result); err != nil {
		return nil, err
	}

	// Load eager relations if any
	if len(m.eagerLoad) > 0 {
		for relation, customQuery := range m.eagerLoad {
			if err := m.loadRelation(ctx, result, relation, customQuery); err != nil {
				return nil, fmt.Errorf("error loading relation '%s': %w", relation, err)
			}
		}
	}

	return result, nil
}

// Where adds a where clause and returns records
func (m *Model) Where(ctx context.Context, column string, operator string, value interface{}) (interface{}, error) {
	// Create a slice of the model type
	sliceType := reflect.SliceOf(reflect.TypeOf(m.value))
	results := reflect.MakeSlice(sliceType, 0, 0)

	// Build query
	rows, err := m.builder.Table(m.table).
		Where(column, operator, value).
		Get(ctx)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Iterate through results
	for rows.Next() {
		// Create a new instance of the model
		result := reflect.New(reflect.TypeOf(m.value)).Elem()

		// Map columns to struct fields
		if err := m.scanRow(rows, result); err != nil {
			return nil, err
		}

		// Append to results slice
		results = reflect.Append(results, result)
	}

	// Load eager relations if any
	if len(m.eagerLoad) > 0 && results.Len() > 0 {
		for relation, customQuery := range m.eagerLoad {
			// Create pointer to slice for loadRelation
			resultsPtr := reflect.New(results.Type())
			resultsPtr.Elem().Set(results)

			if err := m.loadRelation(ctx, resultsPtr.Interface(), relation, customQuery); err != nil {
				return nil, fmt.Errorf("error loading relation '%s': %w", relation, err)
			}

			// Update results with potentially modified values
			results = resultsPtr.Elem()
		}
	}

	return results.Interface(), nil
}

// Create inserts a new record
func (m *Model) Create(ctx context.Context, data interface{}) (int64, error) {
	// Extract values from struct
	values, err := m.extractValues(data, true)
	if err != nil {
		return 0, err
	}

	// Insert into database
	return m.builder.Table(m.table).InsertGetId(ctx, values)
}

// Update updates a record by primary key
func (m *Model) Update(ctx context.Context, data interface{}) (int64, error) {
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return 0, errors.New("data must be a struct or pointer to struct")
	}

	// Get primary key value
	var pkValue interface{}
	for _, f := range m.fields {
		if f.isPK {
			pkValue = v.FieldByName(f.name).Interface()
			break
		}
	}

	if pkValue == nil {
		return 0, errors.New("primary key value not found")
	}

	// Extract values from struct
	values, err := m.extractValues(data, false)
	if err != nil {
		return 0, err
	}

	// Update in database
	return m.builder.Table(m.table).
		Where(m.pk, "=", pkValue).
		UpdateWithContext(ctx, values)
}

// Delete deletes a record by primary key
func (m *Model) Delete(ctx context.Context, id interface{}) (int64, error) {
	return m.builder.Table(m.table).
		Where(m.pk, "=", id).
		DeleteWithContext(ctx)
}

// extractValues extracts field values from a struct into a map
func (m *Model) extractValues(data interface{}, isCreate bool) (map[string]interface{}, error) {
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return nil, errors.New("data must be a struct or pointer to struct")
	}

	values := make(map[string]interface{})

	for _, f := range m.fields {
		// Skip auto-increment primary key on create
		if isCreate && f.isPK && f.isAuto {
			continue
		}

		// Skip omitted fields
		if f.omit {
			continue
		}

		fieldVal := v.FieldByName(f.name)

		// Skip zero values if omitempty
		if f.omitZero && isZeroValue(fieldVal) {
			continue
		}

		// Add to values map
		values[f.column] = fieldVal.Interface()
	}

	return values, nil
}

// scanInto scans a row into a struct
func (m *Model) scanInto(rows *sql.Rows, dest interface{}) error {
	v := reflect.ValueOf(dest)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct && !(v.Kind() == reflect.Ptr && v.Elem().Kind() == reflect.Struct) {
		return errors.New("destination must be a struct or pointer to struct")
	}

	// If it's a pointer to a pointer to a struct, get the pointer to struct
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	return m.scanRow(rows, v)
}

// scanRow scans a row into a struct value
func (m *Model) scanRow(rows *sql.Rows, v reflect.Value) error {
	columns, err := rows.Columns()
	if err != nil {
		return err
	}

	// Create a map of column names to field indices
	colToField := make(map[string]int)
	for i, f := range m.fields {
		colToField[f.column] = i
	}

	// Create a list of pointers to scan into
	values := make([]interface{}, len(columns))
	for i, col := range columns {
		fieldIdx, ok := colToField[col]
		if !ok {
			// If column doesn't map to a field, use a throwaway variable
			values[i] = new(interface{})
			continue
		}

		field := m.fields[fieldIdx]
		fieldVal := v.FieldByName(field.name)

		if !fieldVal.CanSet() {
			return fmt.Errorf("cannot set field %s", field.name)
		}

		// Create appropriate pointer type for the field
		values[i] = reflect.New(fieldVal.Type()).Interface()
	}

	// Scan into values
	if err := rows.Scan(values...); err != nil {
		return err
	}

	// Set struct fields from values
	for i, col := range columns {
		fieldIdx, ok := colToField[col]
		if !ok {
			continue
		}

		field := m.fields[fieldIdx]
		fieldVal := v.FieldByName(field.name)

		// Get value and set field
		scanVal := reflect.ValueOf(values[i]).Elem()

		// Handle special types like time.Time
		if fieldVal.Type() == reflect.TypeOf(time.Time{}) && scanVal.Type() != reflect.TypeOf(time.Time{}) {
			// Handle time conversions
			continue
		}

		// Set field value
		fieldVal.Set(scanVal)
	}

	return nil
}

// Helper functions

// toSnakeCase converts a CamelCase string to snake_case
func toSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && 'A' <= r && r <= 'Z' {
			result.WriteByte('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

// isZeroValue checks if a value is the zero value for its type
func isZeroValue(v reflect.Value) bool {
	return reflect.DeepEqual(v.Interface(), reflect.Zero(v.Type()).Interface())
}

// SetTable explicitly sets the table name for the model
func (m *Model) SetTable(table string) *Model {
	m.table = table
	return m
}

// SetPrimaryKey explicitly sets the primary key field
func (m *Model) SetPrimaryKey(pk string) *Model {
	m.pk = pk
	return m
}

// Query returns the underlying query builder
func (m *Model) Query() *Builder {
	return m.builder.Table(m.table)
}

// First retrieves the first record matching the current query
func (m *Model) First(ctx context.Context) (interface{}, error) {
	result := reflect.New(reflect.TypeOf(m.value)).Interface()

	// Build query
	rows, err := m.builder.Table(m.table).
		Limit(1).
		Get(ctx)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Check if record exists
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}

	// Map columns to struct fields
	if err := m.scanInto(rows, result); err != nil {
		return nil, err
	}

	// Load eager relations if any
	if len(m.eagerLoad) > 0 {
		for relation, customQuery := range m.eagerLoad {
			if err := m.loadRelation(ctx, result, relation, customQuery); err != nil {
				return nil, fmt.Errorf("error loading relation '%s': %w", relation, err)
			}
		}
	}

	return result, nil
}

// HasOne defines a one-to-one relationship
// Returns a query builder for the related model
func (m *Model) HasOne(ctx context.Context, model interface{}, foreignKey, localKey string) (*Builder, error) {
	relatedModel, err := NewModel(m.relManager.db, model)
	if err != nil {
		return nil, err
	}

	if localKey == "" {
		localKey = "id"
	}

	if foreignKey == "" {
		foreignKey = toSnakeCase(reflect.TypeOf(m.value).Name()) + "_id"
	}

	// Get local key value
	localKeyValue, err := m.getKeyValue(m.value, localKey)
	if err != nil {
		return nil, err
	}

	// Build query for related model
	return relatedModel.Query().Where(foreignKey, "=", localKeyValue), nil
}

// HasMany defines a one-to-many relationship
// Returns a query builder for the related models
func (m *Model) HasMany(ctx context.Context, model interface{}, foreignKey, localKey string) (*Builder, error) {
	// Similar to HasOne but for collections
	return m.HasOne(ctx, model, foreignKey, localKey)
}

// BelongsTo defines a belongs-to relationship
// Returns a query builder for the parent model
func (m *Model) BelongsTo(ctx context.Context, model interface{}, foreignKey, ownerKey string) (*Builder, error) {
	relatedModel, err := NewModel(m.relManager.db, model)
	if err != nil {
		return nil, err
	}

	if ownerKey == "" {
		ownerKey = "id"
	}

	if foreignKey == "" {
		foreignKey = toSnakeCase(reflect.TypeOf(model).Name()) + "_id"
	}

	// Get foreign key value from this model
	foreignKeyValue, err := m.getKeyValue(m.value, foreignKey)
	if err != nil {
		return nil, err
	}

	// Build query for parent model
	return relatedModel.Query().Where(ownerKey, "=", foreignKeyValue), nil
}

// BelongsToMany defines a many-to-many relationship
// Returns a query builder for the related models
func (m *Model) BelongsToMany(ctx context.Context, model interface{}, pivotTable, foreignPivotKey, relatedPivotKey string) (*Builder, error) {
	relatedModel, err := NewModel(m.relManager.db, model)
	if err != nil {
		return nil, err
	}

	if pivotTable == "" {
		// Default pivot table name: table1_table2 (alphabetical order)
		table1 := m.table
		table2 := relatedModel.table
		if table1 > table2 {
			table1, table2 = table2, table1
		}
		pivotTable = table1 + "_" + table2
	}

	if foreignPivotKey == "" {
		foreignPivotKey = getSingular(m.table) + "_id"
	}

	if relatedPivotKey == "" {
		relatedPivotKey = getSingular(relatedModel.table) + "_id"
	}

	// Get local key value
	localKeyValue, err := m.getKeyValue(m.value, "id")
	if err != nil {
		return nil, err
	}

	// Build query for related models through pivot table
	return relatedModel.Query().
		Join(pivotTable, fmt.Sprintf("%s.id = %s.%s", relatedModel.table, pivotTable, relatedPivotKey)).
		Where(fmt.Sprintf("%s.%s", pivotTable, foreignPivotKey), "=", localKeyValue), nil
}

// getKeyValue gets a field value by column name
func (m *Model) getKeyValue(model interface{}, columnName string) (interface{}, error) {
	// Find the field name for the given column
	var fieldName string
	for _, f := range m.fields {
		if f.column == columnName {
			fieldName = f.name
			break
		}
	}

	if fieldName == "" {
		return nil, fmt.Errorf("column not found: %s", columnName)
	}

	// Get the field value
	v := reflect.ValueOf(model)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	fieldValue := v.FieldByName(fieldName)
	if !fieldValue.IsValid() {
		return nil, fmt.Errorf("field not found: %s", fieldName)
	}

	return fieldValue.Interface(), nil
}

// Paginate retrieves records with pagination
func (m *Model) Paginate(ctx context.Context, page, perPage int) (*Paginator, error) {
	return m.builder.Table(m.table).Paginate(page, perPage)
}

// WithContext returns a clone of the model with the specified context
func (m *Model) WithContext(ctx context.Context) *Model {
	clone := *m
	// Deep clone the eager load map
	clone.eagerLoad = make(map[string]func(*Builder) *Builder, len(m.eagerLoad))
	for k, v := range m.eagerLoad {
		clone.eagerLoad[k] = v
	}
	return &clone
}

// WithTransaction returns a clone of the model with the transaction
func (m *Model) WithTransaction(tx *Builder) *Model {
	clone := *m
	clone.builder = tx
	// Deep clone the eager load map
	clone.eagerLoad = make(map[string]func(*Builder) *Builder, len(m.eagerLoad))
	for k, v := range m.eagerLoad {
		clone.eagerLoad[k] = v
	}
	return &clone
}

// Preload loads a relation for an already retrieved model or collection
func (m *Model) Preload(ctx context.Context, result interface{}, relation string) error {
	return m.PreloadWithQuery(ctx, result, relation, nil)
}

// PreloadWithQuery loads a relation with a custom query
func (m *Model) PreloadWithQuery(ctx context.Context, result interface{}, relation string, customQuery func(*Builder) *Builder) error {
	return m.loadRelation(ctx, result, relation, customQuery)
}

// WithTransaction returns a clone of the model with the transaction
// func (m *Model) WithTransaction(tx *Builder) *Model {
// 	clone := *m
// 	clone.builder = tx
// 	return &clone
// }

// Transaction executes a function within a transaction
// Supports nested transactions (uses savepoints for nested transactions)
func (m *Model) Transaction(ctx context.Context, fn func(*Model) error) error {
	// Check if we're already in a transaction
	tx, isInTransaction := m.builder.db.(*sql.Tx)

	if isInTransaction {
		// We're already in a transaction, use a savepoint
		savepointID := fmt.Sprintf("sp_%d", time.Now().UnixNano())

		// Start savepoint
		_, err := tx.ExecContext(ctx, fmt.Sprintf("SAVEPOINT %s", savepointID))
		if err != nil {
			return fmt.Errorf("failed to create savepoint: %w", err)
		}

		// Execute the function
		err = fn(m)

		if err != nil {
			// Rollback to savepoint on error
			_, rbErr := tx.ExecContext(ctx, fmt.Sprintf("ROLLBACK TO SAVEPOINT %s", savepointID))
			if rbErr != nil {
				return fmt.Errorf("failed to rollback to savepoint: %v (original error: %w)", rbErr, err)
			}
			return err
		}

		// Release savepoint on success
		_, err = tx.ExecContext(ctx, fmt.Sprintf("RELEASE SAVEPOINT %s", savepointID))
		if err != nil {
			return fmt.Errorf("failed to release savepoint: %w", err)
		}

		return nil
	}

	// Not in transaction yet, start a new one
	return m.builder.Transaction(ctx, func(tx *Builder) error {
		return fn(m.WithTransaction(tx))
	})
}

// parseRelationTag parses a relation tag and returns a relation struct
func (m *Model) parseRelationTag(tag string, field reflect.StructField) (*relation, error) {
	parts := strings.Split(tag, ",")
	if len(parts) < 1 {
		return nil, errors.New("relation type required")
	}

	rel := &relation{}

	// Parse relation type
	relTypeStr := parts[0]
	switch relTypeStr {
	case "hasOne":
		rel.relType = relationHasOne
	case "hasMany":
		rel.relType = relationHasMany
	case "belongsTo":
		rel.relType = relationBelongsTo
	case "manyToMany":
		rel.relType = relationManyToMany
	default:
		return nil, fmt.Errorf("unknown relation type: %s", relTypeStr)
	}

	// Get the target model type
	fieldType := field.Type
	if fieldType.Kind() == reflect.Ptr {
		fieldType = fieldType.Elem()
	}

	// For collection types, get the element type
	if fieldType.Kind() == reflect.Slice || fieldType.Kind() == reflect.Array {
		elemType := fieldType.Elem()
		if elemType.Kind() == reflect.Ptr {
			elemType = elemType.Elem()
		}
		fieldType = elemType
	}

	rel.modelType = fieldType
	rel.targetTable = toSnakeCase(fieldType.Name())

	// Set default keys based on relation type
	switch rel.relType {
	case relationHasOne, relationHasMany:
		rel.localKey = "id"
		rel.foreignKey = toSnakeCase(reflect.TypeOf(m.value).Elem().Name()) + "_id"
	case relationBelongsTo:
		rel.localKey = toSnakeCase(field.Name) + "_id"
		rel.foreignKey = "id"
	case relationManyToMany:
		rel.localKey = "id"
		rel.foreignKey = "id"
		// Default pivot table name: table1_table2 (alphabetical order)
		table1 := m.table
		table2 := rel.targetTable
		if table1 > table2 {
			table1, table2 = table2, table1
		}
		rel.pivot = table1 + "_" + table2
		rel.pivotFk = getSingular(m.table) + "_id"
		rel.pivotRfk = getSingular(rel.targetTable) + "_id"
	}

	// Parse additional options
	for i := 1; i < len(parts); i++ {
		option := parts[i]
		keyValue := strings.SplitN(option, ":", 2)

		if len(keyValue) != 2 {
			continue
		}

		key := keyValue[0]
		value := keyValue[1]

		switch key {
		case "foreignKey":
			rel.foreignKey = value
		case "localKey":
			rel.localKey = value
		case "pivot":
			rel.pivot = value
		case "pivotFk":
			rel.pivotFk = value
		case "pivotRfk":
			rel.pivotRfk = value
		case "table":
			rel.targetTable = value
		}
	}

	return rel, nil
}

// getSingular returns the singular form of a word
// This is a very basic implementation - in a real app, you might want
// to use a proper inflector library
func getSingular(word string) string {
	// Very basic implementation, just for demonstration
	if strings.HasSuffix(word, "ies") {
		return word[:len(word)-3] + "y"
	}
	if strings.HasSuffix(word, "s") {
		return word[:len(word)-1]
	}
	return word
}

// With specifies relations to eager load
func (m *Model) With(relations ...string) *Model {
	clone := *m
	for _, relation := range relations {
		clone.eagerLoad[relation] = nil // Use default query
	}
	return &clone
}

// WithQuery specifies a relation to eager load with a custom query
func (m *Model) WithQuery(relation string, query func(*Builder) *Builder) *Model {
	clone := *m
	clone.eagerLoad[relation] = query
	return &clone
}

// loadRelation loads related models for a specific relation
func (m *Model) loadRelation(ctx context.Context, results interface{}, relationName string, customQuery func(*Builder) *Builder) error {
	// Get the field for the relation
	var relationField *Field
	for _, f := range m.fields {
		if strings.EqualFold(f.name, relationName) {
			relationField = &f
			break
		}
	}

	if relationField == nil || relationField.relation == nil {
		return fmt.Errorf("relation '%s' not found", relationName)
	}

	// Get relation info
	rel := relationField.relation
	targetTable := rel.targetTable

	// Find related model
	var relatedModel *Model
	var exists bool

	// Try to get related model from registry
	if m.relManager != nil {
		relatedModel, exists = m.relManager.registry[rel.modelType]
		if !exists {
			// Try to create a new model instance
			dummy := reflect.New(rel.modelType).Interface()
			var err error
			relatedModel, err = NewModel(m.relManager.db, dummy)
			if err != nil {
				return fmt.Errorf("failed to create related model: %w", err)
			}
		}
	} else {
		return errors.New("relation manager not initialized")
	}

	// Set flag to indicate this model is being used for preloading
	relatedModel.isPreload = true

	// Create query builder for the related model
	query := relatedModel.Query()

	// Apply custom query constraints if provided
	if customQuery != nil {
		query = customQuery(query)
	}

	// Extract primary keys from results
	resultVal := reflect.ValueOf(results)
	if resultVal.Kind() == reflect.Ptr {
		resultVal = resultVal.Elem()
	}

	var primaryKeys []interface{}
	var modelMap = make(map[interface{}]reflect.Value) // Map primary keys to model values

	// Handle different result types (single model or slice of models)
	switch resultVal.Kind() {
	case reflect.Struct:
		// Single model
		pkField := resultVal.FieldByName(getPkFieldName(m.fields, m.pk))
		if pkField.IsValid() {
			pkValue := pkField.Interface()
			primaryKeys = append(primaryKeys, pkValue)
			modelMap[pkValue] = resultVal
		}
	case reflect.Slice, reflect.Array:
		// Slice of models
		for i := 0; i < resultVal.Len(); i++ {
			itemVal := resultVal.Index(i)
			if itemVal.Kind() == reflect.Ptr {
				itemVal = itemVal.Elem()
			}

			if itemVal.Kind() != reflect.Struct {
				continue
			}

			pkField := itemVal.FieldByName(getPkFieldName(m.fields, m.pk))
			if pkField.IsValid() {
				pkValue := pkField.Interface()
				primaryKeys = append(primaryKeys, pkValue)
				modelMap[pkValue] = itemVal
			}
		}
	default:
		return fmt.Errorf("unsupported result type: %s", resultVal.Kind())
	}

	if len(primaryKeys) == 0 {
		return nil // No primary keys to load relations for
	}

	// Modify query based on relationship type
	var foreignKeyField string // Field in related model that references parent

	var columns []string

	switch rel.relType {
	case relationHasOne, relationHasMany:
		foreignKeyField = rel.foreignKey
		query.WhereIn(rel.foreignKey, primaryKeys...)
	case relationBelongsTo:
		// For belongsTo, collect foreign keys from parent models
		foreignKeys := make([]interface{}, 0, len(modelMap))
		for _, modelVal := range modelMap {
			fkField := modelVal.FieldByName(getFieldNameByColumn(m.fields, rel.localKey))
			if fkField.IsValid() && !fkField.IsZero() {
				foreignKeys = append(foreignKeys, fkField.Interface())
			}
		}

		if len(foreignKeys) == 0 {
			return nil // No foreign keys to query
		}

		foreignKeyField = rel.foreignKey
		query.WhereIn(rel.foreignKey, foreignKeys...)
	case relationManyToMany:
		// For many-to-many, we need to query through the pivot table
		query = query.
			Join(rel.pivot, fmt.Sprintf("%s.%s = %s.%s", targetTable, rel.foreignKey, rel.pivot, rel.pivotRfk)).
			WhereIn(fmt.Sprintf("%s.%s", rel.pivot, rel.pivotFk), primaryKeys...).
			Select(fmt.Sprintf("%s.*", targetTable), fmt.Sprintf("%s.%s as pivot_%s", rel.pivot, rel.pivotFk, rel.pivotFk))

		// We'll need to track which related models belong to which parent models
		foreignKeyField = rel.foreignKey
	}

	// Execute query to get related models
	relatedRows, err := query.Get(ctx)
	if err != nil {
		return err
	}
	defer relatedRows.Close()

	// Process related rows based on relationship type
	switch rel.relType {
	case relationHasOne, relationBelongsTo:
		// Map to store related models by key
		relatedMap := make(map[interface{}]interface{})

		// Get columns from query result
		columns, err = relatedRows.Columns()
		if err != nil {
			return fmt.Errorf("failed to get columns: %w", err)
		}

		// For each row, create and scan into a new related model instance
		for relatedRows.Next() {
			// Create a new instance of the related model
			relatedInstance := reflect.New(rel.modelType).Interface()

			// Scan into the related instance
			if err := relatedModel.scanInto(relatedRows, relatedInstance); err != nil {
				return fmt.Errorf("failed to scan related row: %w", err)
			}

			// Extract the key value to map this related instance
			var keyValue interface{}

			if rel.relType == relationHasOne {
				// For hasOne, the related model contains the foreign key
				keyValue = extractFieldValue(relatedInstance, foreignKeyField)
			} else {
				// For belongsTo, we use the primary key of the related model
				keyValue = extractFieldValue(relatedInstance, foreignKeyField)
			}

			if keyValue != nil {
				relatedMap[keyValue] = relatedInstance
			}
		}

		if err := relatedRows.Err(); err != nil {
			return fmt.Errorf("error iterating related rows: %w", err)
		}

		// Assign related models to parent models
		for pk, parentVal := range modelMap {
			var keyToLookup interface{}

			if rel.relType == relationHasOne {
				// For hasOne, the key is the parent primary key
				keyToLookup = pk
			} else {
				// For belongsTo, the key is the foreign key in the parent
				keyToLookup = parentVal.FieldByName(getFieldNameByColumn(m.fields, rel.localKey)).Interface()
			}

			if relatedInstance, ok := relatedMap[keyToLookup]; ok {
				// Get the field on the parent model
				relField := parentVal.FieldByName(relationName)
				if relField.IsValid() && relField.CanSet() {
					// Set the related model
					relFieldType := relField.Type()
					relatedVal := reflect.ValueOf(relatedInstance)

					// Handle pointer vs. non-pointer field types
					if relFieldType.Kind() == reflect.Ptr {
						relField.Set(relatedVal)
					} else if relatedVal.Kind() == reflect.Ptr {
						relField.Set(relatedVal.Elem())
					} else {
						relField.Set(relatedVal)
					}
				}
			}
		}

	case relationHasMany, relationManyToMany:
		// For collections, we need to group related models by parent key
		relatedGroups := make(map[interface{}][]interface{})

		// For many-to-many, we need to track pivot IDs
		pivotFkIndex := -1
		if rel.relType == relationManyToMany {
			columns, _ := relatedRows.Columns()
			for i, col := range columns {
				if col == fmt.Sprintf("pivot_%s", rel.pivotFk) {
					pivotFkIndex = i
					break
				}
			}
		}

		// Process each related record
		for relatedRows.Next() {
			// Create a new instance of the related model
			relatedInstance := reflect.New(rel.modelType).Interface()

			// For many-to-many, we need custom scanning to handle pivot values
			if rel.relType == relationManyToMany && pivotFkIndex >= 0 {
				// Here we would need to handle scanning both the model and pivot data
				// This is a simplified version - in a real implementation, you would
				// need more sophisticated code to handle both model and pivot fields

				// Scan into related instance
				if err := relatedModel.scanInto(relatedRows, relatedInstance); err != nil {
					return fmt.Errorf("failed to scan related row: %w", err)
				}

				// For many-to-many, the parent key comes from the pivot table
				var pivotParentKey interface{}
				var values = make([]interface{}, len(columns))
				for i := range values {
					values[i] = new(interface{})
				}

				// Re-scan to get pivot data
				if err := relatedRows.Scan(values...); err != nil {
					return fmt.Errorf("failed to scan pivot data: %w", err)
				}

				pivotParentKey = *(values[pivotFkIndex].(*interface{}))

				if pivotParentKey != nil {
					if _, ok := relatedGroups[pivotParentKey]; !ok {
						relatedGroups[pivotParentKey] = make([]interface{}, 0)
					}
					relatedGroups[pivotParentKey] = append(relatedGroups[pivotParentKey], relatedInstance)
				}
			} else {
				// For hasMany
				if err := relatedModel.scanInto(relatedRows, relatedInstance); err != nil {
					return fmt.Errorf("failed to scan related row: %w", err)
				}

				// Get the foreign key value that references the parent
				parentKey := extractFieldValue(relatedInstance, foreignKeyField)

				if parentKey != nil {
					if _, ok := relatedGroups[parentKey]; !ok {
						relatedGroups[parentKey] = make([]interface{}, 0)
					}
					relatedGroups[parentKey] = append(relatedGroups[parentKey], relatedInstance)
				}
			}
		}

		if err := relatedRows.Err(); err != nil {
			return fmt.Errorf("error iterating related rows: %w", err)
		}

		// Assign related collections to parent models
		for pk, parentVal := range modelMap {
			relatedSlice, ok := relatedGroups[pk]
			if !ok {
				relatedSlice = make([]interface{}, 0) // Empty slice for models with no relations
			}

			// Get the field on the parent model
			relField := parentVal.FieldByName(relationName)
			if relField.IsValid() && relField.CanSet() {
				// Create a new slice of the right type
				sliceType := relField.Type()
				newSlice := reflect.MakeSlice(sliceType, 0, len(relatedSlice))

				// Check if the slice elements are pointers
				elemType := sliceType.Elem()
				isPtrElem := elemType.Kind() == reflect.Ptr

				// Add each related instance to the slice
				for _, item := range relatedSlice {
					itemVal := reflect.ValueOf(item)

					// Handle pointer vs. non-pointer slice elements
					if isPtrElem && itemVal.Kind() != reflect.Ptr {
						// Need to convert to pointer
						newItem := reflect.New(itemVal.Type())
						newItem.Elem().Set(itemVal)
						newSlice = reflect.Append(newSlice, newItem)
					} else if !isPtrElem && itemVal.Kind() == reflect.Ptr {
						// Need to dereference pointer
						newSlice = reflect.Append(newSlice, itemVal.Elem())
					} else {
						// Types match
						newSlice = reflect.Append(newSlice, itemVal)
					}
				}

				// Set the slice field
				relField.Set(newSlice)
			}
		}
	}

	return nil
}

// extractFieldValue extracts a field value from a model instance by column name
func extractFieldValue(model interface{}, columnName string) interface{} {
	val := reflect.ValueOf(model)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil
	}

	// Try direct field access first (assuming field name matches column name)
	field := val.FieldByName(columnName)
	if field.IsValid() {
		return field.Interface()
	}

	// If that fails, try to find a field with matching column tag
	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("db")
		tagParts := strings.Split(tag, ",")
		if len(tagParts) > 0 && tagParts[0] == columnName {
			return val.Field(i).Interface()
		}
	}

	return nil
}

// getFieldNameByColumn gets the field name corresponding to a given column name
func getFieldNameByColumn(fields []Field, colName string) string {
	for _, field := range fields {
		if field.column == colName {
			return field.name
		}
	}

	// If not found, try a case-insensitive match or return the column name itself
	// as a last resort (assuming it might be a field name already)
	for _, field := range fields {
		if strings.EqualFold(field.column, colName) {
			return field.name
		}
	}

	// Capitalize first letter to match Go's exported field convention
	if len(colName) > 0 {
		return strings.ToUpper(colName[:1]) + colName[1:]
	}

	return colName
}

// Count returns the count of records
func (m *Model) Count(ctx context.Context) (int64, error) {
	var count int64
	rows, err := m.builder.Table(m.table).Count("*").Get(ctx)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	if rows.Next() {
		err = rows.Scan(&count)
	}

	return count, err
}
