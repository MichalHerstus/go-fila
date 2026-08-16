package schema

import (
	"strings"
	"testing"
)

func TestGenerateResourceYAML(t *testing.T) {
	tables := ParseSchemaBytes([]byte(`CREATE TABLE orders (
    id SERIAL PRIMARY KEY,
    customer_id INT REFERENCES customers(id),
    total DECIMAL(10,2),
    created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE TABLE customers (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL
);`))
	yaml := GenerateResourceYAML(tables[0], tables, "postgres")
	for _, want := range []string{"name: Order", "list:", "form:", "options_query: ListCustomers"} {
		if !strings.Contains(yaml, want) {
			t.Errorf("yaml missing %q", want)
		}
	}
}
