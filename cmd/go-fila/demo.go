// demo.go
//
// Implements `go-fila init --demo`: scaffolds a full-featured sqlite demo
// project built around an order-management domain. Besides the go-fila.yaml
// config (resources, pages, widgets, navigation, auth, RBAC policies, card and
// kanban views, custom and bulk actions, dark-mode brand theme) it writes a
// sqlite schema with related tables
// (roles, users, customers, products, orders, orderlines) and seeds the
// database with realistic demo data (~50-100 rows per table) including an
// admin user (admin@demo.test / admin) and bcrypt-hashed passwords.
package main

import (
	"database/sql"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

// cmdInitDemo scaffolds the demo project into the current directory: it writes
// go-fila.yaml (full-featured order-management config), sql/migrations/schema.sql
// and sql/queries/*.sql, then creates and seeds the sqlite database at
// {outDir}/data/admin.db. It refuses to overwrite existing files unless force
// is given.
// Params: configPath (path of the go-fila.yaml file to write), outDir (output
// directory where sql/ and data/ live), force (overwrite existing files).
// Returns: an error describing the first failure, or nil.
func cmdInitDemo(configPath, outDir string, force bool) error {
	if !force {
		if _, err := os.Stat(configPath); err == nil {
			return fmt.Errorf("%s already exists. Use --force to overwrite.", configPath)
		}
		if _, err := os.Stat(outDir); err == nil {
			return fmt.Errorf("%s already exists. Use --force to overwrite.", outDir)
		}
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(outDir, "sql", "queries"), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(outDir, "sql", "migrations"), 0755); err != nil {
		return err
	}

	if err := os.WriteFile(configPath, []byte(demoYAML()), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "sql", "migrations", "schema.sql"), []byte(demoSchema()), 0644); err != nil {
		return err
	}
	for name, content := range demoQueries() {
		if err := os.WriteFile(filepath.Join(outDir, "sql", "queries", name), []byte(content), 0644); err != nil {
			return err
		}
	}

	dbPath := filepath.Join(outDir, "data", "admin.db")
	if err := seedDemoDB(dbPath); err != nil {
		return fmt.Errorf("seeding demo database: %w", err)
	}

	fmt.Println("Scaffolded demo admin panel in", outDir)
	fmt.Println("Database seeded at", dbPath)
	fmt.Println("")
	fmt.Println("Next steps:")
	fmt.Println("  1. go-fila generate --config", configPath, "--out", outDir)
	fmt.Println("  2. cd", outDir)
	fmt.Println("  3. make run")
	fmt.Println("")
	fmt.Println("Demo login:")
	fmt.Println("  URL:      http://localhost:8080/admin/login")
	fmt.Println("  Email:    admin@demo.test")
	fmt.Println("  Password: admin")
	return nil
}

// demoYAML returns the go-fila.yaml config for the demo project. It exercises
// the full feature set: multiple related resources with list/detail/form/card
// views, kanban boards, custom actions, RBAC policies, dashboard pages with
// stat/stats_grid/chart/table widgets and a grouped navigation sidebar.
// Returns: the YAML config as a string.
func demoYAML() string {
	return `version: "1.0"

panel:
  id: admin
  path: /admin
  name: "Acme Shop Admin"
  brand:
    colors:
      primary: "#6366f1"
      secondary: "#64748b"
  layout:
    sidebar:
      collapsible: true
      width: 280
      collapsed_width: 72
    topbar:
      sticky: true
    max_content_width: 7xl
  theme:
    dark_mode: true
    font:
      family: "Inter, sans-serif"
      mono: "JetBrains Mono, monospace"

connections:
  default:
    driver: sqlite
    dsn: "file:./data/admin.db"

sqlc:
  config: sqlc.yaml
  queries_dir: ./sql/queries
  schema_dir: ./sql/migrations
  output_pkg: internal/data

auth:
  guard: web
  provider: session
  table: users
  login:
    fields: [email, password]
    redirect: /admin/Dashboard
  registration: false
  password_reset: true
  remember_me: true

resources:
  - name: Role
    label: Roles
    icon: users
    group: "Administration"
    list:
      query: ListRoles
      count_query: CountRoles
      columns:
        - name: id
          type: integer
          sortable: true
        - name: name
          type: string
          sortable: true
          searchable: true
        - name: description
          type: text
        - name: created_at
          type: datetime
          sortable: true
      default_sort: name
    detail:
      query: GetRole
      params:
        id: "{record.id}"
      fields:
        - name: id
          type: integer
        - name: name
          type: string
        - name: description
          type: text
        - name: created_at
          type: datetime
    form:
      create:
        query: CreateRole
        fields:
          - name: name
            type: text
            required: true
          - name: description
            type: text
      update:
        query: UpdateRole
        populate_query: GetRole
        fields:
          - name: name
            type: text
          - name: description
            type: text
    policies:
      view_any: "admin"
      view: "admin"
      create: "admin"
      update: "admin"
      delete: "admin"

  - name: User
    label: Users
    icon: users
    group: "Administration"
    list:
      query: ListUsers
      count_query: CountUsers
      columns:
        - name: id
          type: integer
          sortable: true
        - name: name
          type: string
          sortable: true
          searchable: true
        - name: email
          type: email
          sortable: true
          searchable: true
        - name: role_name
          label: Role
          type: text
        - name: status
          type: badge
          options:
            active: Active
            inactive: Inactive
        - name: created_at
          type: datetime
          sortable: true
      default_sort: -created_at
    detail:
      query: GetUser
      params:
        id: "{record.id}"
      fields:
        - name: id
          type: integer
        - name: name
          type: string
        - name: email
          type: email
        - name: role_name
          label: Role
          type: text
        - name: status
          type: badge
          options:
            active: Active
            inactive: Inactive
        - name: created_at
          type: datetime
    form:
      create:
        query: CreateUser
        fields:
          - name: name
            type: text
            required: true
            validation:
              min: 2
              max: 100
          - name: email
            type: email
            required: true
          - name: password
            type: password
            required: true
          - name: role_id
            type: select
            options_query: ListRoles
            options_value: id
            options_label: name
          - name: status
            type: select
            options:
              active: Active
              inactive: Inactive
      update:
        query: UpdateUser
        populate_query: GetUser
        fields:
          - name: name
            type: text
          - name: email
            type: email
          - name: role_id
            type: select
            options_query: ListRoles
            options_value: id
            options_label: name
          - name: status
            type: select
            options:
              active: Active
              inactive: Inactive
    policies:
      view_any: "admin|manager"
      view: "admin|manager"
      create: "admin"
      update: "admin"
      delete: "admin"

  - name: Customer
    label: Customers
    icon: users
    group: "Sales"
    list:
      query: ListCustomers
      count_query: CountCustomers
      columns:
        - name: id
          type: integer
          sortable: true
        - name: name
          type: string
          sortable: true
          searchable: true
        - name: email
          type: email
          searchable: true
        - name: company
          type: string
          sortable: true
        - name: city
          type: string
        - name: status
          type: badge
          options:
            active: Active
            inactive: Inactive
        - name: created_at
          type: datetime
          sortable: true
      default_sort: -created_at
    card:
      fields:
        - name: name
          type: string
        - name: email
          type: email
        - name: company
          type: string
        - name: city
          type: string
        - name: status
          type: select
          options:
            active: Active
            inactive: Inactive
      columns: 3
      rows: 4
      kanban_field: status
      searchable:
        - name
        - email
      default_sort: -created_at
    detail:
      query: GetCustomer
      params:
        id: "{record.id}"
      fields:
        - name: id
          type: integer
        - name: name
          type: string
        - name: email
          type: email
        - name: phone
          type: string
        - name: company
          type: string
        - name: city
          type: string
        - name: country
          type: string
        - name: status
          type: badge
          options:
            active: Active
            inactive: Inactive
        - name: created_at
          type: datetime
    form:
      create:
        query: CreateCustomer
        fields:
          - name: name
            type: text
            required: true
          - name: email
            type: email
            required: true
          - name: phone
            type: text
          - name: company
            type: text
          - name: city
            type: text
          - name: country
            type: text
          - name: status
            type: select
            options:
              active: Active
              inactive: Inactive
      update:
        query: UpdateCustomer
        populate_query: GetCustomer
        fields:
          - name: name
            type: text
          - name: email
            type: email
          - name: phone
            type: text
          - name: company
            type: text
          - name: city
            type: text
          - name: country
            type: text
          - name: status
            type: select
            options:
              active: Active
              inactive: Inactive
    policies:
      view_any: "admin|manager"
      view: "admin|manager"
      create: "admin|manager"
      update: "admin|manager"
      delete: "admin"

  - name: Product
    label: Products
    icon: home
    group: "Catalog"
    list:
      query: ListProducts
      count_query: CountProducts
      columns:
        - name: id
          type: integer
          sortable: true
        - name: name
          type: string
          sortable: true
          searchable: true
        - name: sku
          type: string
          searchable: true
        - name: category
          type: string
          sortable: true
        - name: price
          type: float
          sortable: true
        - name: stock
          type: integer
          sortable: true
        - name: status
          type: badge
          options:
            active: Active
            inactive: Inactive
        - name: created_at
          type: datetime
          sortable: true
      default_sort: -created_at
    card:
      fields:
        - name: name
          type: string
        - name: category
          type: string
        - name: price
          type: float
        - name: stock
          type: integer
        - name: status
          type: select
          options:
            active: Active
            inactive: Inactive
      columns: 4
      rows: 4
      searchable:
        - name
      default_sort: -created_at
    detail:
      query: GetProduct
      params:
        id: "{record.id}"
      fields:
        - name: id
          type: integer
        - name: name
          type: string
        - name: sku
          type: string
        - name: category
          type: string
        - name: price
          type: float
        - name: stock
          type: integer
        - name: status
          type: badge
          options:
            active: Active
            inactive: Inactive
        - name: created_at
          type: datetime
    form:
      create:
        query: CreateProduct
        fields:
          - name: name
            type: text
            required: true
          - name: sku
            type: text
            required: true
          - name: category
            type: select
            options:
              Electronics: Electronics
              Home: Home
              Office: Office
              Apparel: Apparel
              Sports: Sports
          - name: price
            type: float
            required: true
          - name: stock
            type: integer
          - name: status
            type: select
            options:
              active: Active
              inactive: Inactive
      update:
        query: UpdateProduct
        populate_query: GetProduct
        fields:
          - name: name
            type: text
          - name: sku
            type: text
          - name: category
            type: select
            options:
              Electronics: Electronics
              Home: Home
              Office: Office
              Apparel: Apparel
              Sports: Sports
          - name: price
            type: float
          - name: stock
            type: integer
          - name: status
            type: select
            options:
              active: Active
              inactive: Inactive
    actions:
      - name: restock
        label: "Restock +10"
        icon: check
        color: success
        requires_confirmation: false
        query: "UPDATE products SET stock = stock + 10 WHERE id = $1"
    policies:
      view_any: "admin|manager"
      view: "admin|manager"
      create: "admin|manager"
      update: "admin|manager"
      delete: "admin"

  - name: Order
    label: Orders
    icon: home
    group: "Sales"
    list:
      query: ListOrders
      count_query: CountOrders
      columns:
        - name: id
          type: integer
          sortable: true
        - name: customer_id
          label: Customer ID
          type: integer
        - name: customer_name
          label: Customer
          type: string
          sortable: true
          searchable: true
        - name: status
          type: badge
          options:
            pending: Pending
            processing: Processing
            completed: Completed
            cancelled: Cancelled
        - name: total
          type: float
          sortable: true
        - name: created_at
          type: datetime
          sortable: true
      default_sort: -created_at
    card:
      fields:
        - name: customer_name
          label: Customer
          type: string
        - name: status
          type: select
          options:
            pending: Pending
            processing: Processing
            completed: Completed
            cancelled: Cancelled
        - name: total
          type: float
        - name: created_at
          type: datetime
      columns: 3
      rows: 4
      kanban_field: status
      searchable:
        - customer_name
      default_sort: -created_at
    detail:
      query: GetOrder
      params:
        id: "{record.id}"
      fields:
        - name: id
          type: integer
        - name: customer_id
          label: Customer ID
          type: integer
        - name: customer_name
          label: Customer
          type: string
        - name: status
          type: badge
          options:
            pending: Pending
            processing: Processing
            completed: Completed
            cancelled: Cancelled
        - name: total
          type: float
        - name: created_at
          type: datetime
    form:
      create:
        query: CreateOrder
        fields:
          - name: customer_id
            label: Customer
            type: select
            options_query: ListCustomerOptions
            options_value: id
            options_label: name
          - name: status
            type: select
            options:
              pending: Pending
              processing: Processing
              completed: Completed
              cancelled: Cancelled
          - name: total
            type: float
      update:
        query: UpdateOrder
        populate_query: GetOrder
        fields:
          - name: customer_id
            label: Customer
            type: select
            options_query: ListCustomerOptions
            options_value: id
            options_label: name
          - name: status
            type: select
            options:
              pending: Pending
              processing: Processing
              completed: Completed
              cancelled: Cancelled
          - name: total
            type: float
    actions:
      - name: complete
        label: "Complete"
        icon: check
        color: success
        requires_confirmation: true
        query: "UPDATE orders SET status = 'completed' WHERE id = $1"
      - name: cancel
        label: "Cancel"
        icon: check
        color: danger
        requires_confirmation: true
        query: "UPDATE orders SET status = 'cancelled' WHERE id = $1"
      - name: complete_selected
        label: "Complete Selected"
        icon: check
        color: success
        requires_confirmation: true
        bulk: true
        query: "UPDATE orders SET status = 'completed' WHERE id = $1"
    policies:
      view_any: "admin|manager"
      view: "admin|manager"
      create: "admin|manager"
      update: "admin|manager"
      delete: "admin"

  - name: OrderLine
    label: Order Lines
    icon: home
    group: "Sales"
    list:
      query: ListOrderLines
      count_query: CountOrderLines
      columns:
        - name: id
          type: integer
          sortable: true
        - name: order_id
          label: Order ID
          type: integer
          sortable: true
        - name: product_id
          label: Product ID
          type: integer
        - name: product_name
          label: Product
          type: string
          searchable: true
        - name: quantity
          type: integer
          sortable: true
        - name: unit_price
          label: Unit Price
          type: float
        - name: line_total
          label: Total
          type: float
        - name: created_at
          type: datetime
          sortable: true
      default_sort: -created_at
    detail:
      query: GetOrderLine
      params:
        id: "{record.id}"
      fields:
        - name: id
          type: integer
        - name: order_id
          label: Order ID
          type: integer
        - name: product_id
          label: Product ID
          type: integer
        - name: product_name
          label: Product
          type: string
        - name: quantity
          type: integer
        - name: unit_price
          label: Unit Price
          type: float
        - name: line_total
          label: Total
          type: float
        - name: created_at
          type: datetime
    form:
      create:
        query: CreateOrderLine
        fields:
          - name: order_id
            label: Order
            type: select
            options_query: ListOrderOptions
            options_value: id
            options_label: customer_name
          - name: product_id
            label: Product
            type: select
            options_query: ListProductOptions
            options_value: id
            options_label: name
          - name: quantity
            type: integer
          - name: unit_price
            label: Unit Price
            type: float
      update:
        query: UpdateOrderLine
        populate_query: GetOrderLine
        fields:
          - name: order_id
            label: Order
            type: select
            options_query: ListOrderOptions
            options_value: id
            options_label: customer_name
          - name: product_id
            label: Product
            type: select
            options_query: ListProductOptions
            options_value: id
            options_label: name
          - name: quantity
            type: integer
          - name: unit_price
            label: Unit Price
            type: float
    policies:
      view_any: "admin|manager"
      view: "admin|manager"
      create: "admin|manager"
      update: "admin|manager"
      delete: "admin"

pages:
  - name: Dashboard
    path: /Dashboard
    default: true
    widgets:
      - type: stats_grid
        columns: 4
        widgets:
          - type: stat
            label: "Customers"
            query: SELECT COUNT(*) FROM customers
            icon: users
            color: primary
          - type: stat
            label: "Open Orders"
            query: SELECT COUNT(*) FROM orders WHERE status IN ('pending', 'processing')
            icon: home
            color: warning
          - type: stat
            label: "Products"
            query: SELECT COUNT(*) FROM products
            icon: cog
            color: success
          - type: stat
            label: "Total Revenue"
            query: SELECT CAST(ROUND(SUM(total)) AS INTEGER) FROM orders
            icon: dollar
            color: primary
      - type: chart
        label: "Orders per Month"
        query: SELECT strftime('%Y-%m', created_at) AS label, COUNT(*) AS value FROM orders GROUP BY strftime('%Y-%m', created_at) ORDER BY label
        chart:
          type: line
      - type: chart
        label: "Orders by Status"
        query: SELECT status AS label, COUNT(*) AS value FROM orders GROUP BY status
        chart:
          type: pie
      - type: chart
        label: "Products by Category"
        query: SELECT category AS label, COUNT(*) AS value FROM products GROUP BY category ORDER BY value DESC
        chart:
          type: bar
      - type: table
        label: "Recent Orders"
        query: SELECT id, customer_name, total, status, created_at FROM orders ORDER BY created_at DESC LIMIT 5
        data_columns: [id, customer_name, total, status, created_at]
      - type: list
        label: "Store Snapshot"
        query: SELECT 'Customers' AS label, COUNT(*) AS value FROM customers UNION ALL SELECT 'Products', COUNT(*) FROM products UNION ALL SELECT 'Open Orders', COUNT(*) FROM orders WHERE status IN ('pending', 'processing') UNION ALL SELECT 'Orders Completed', COUNT(*) FROM orders WHERE status = 'completed'
      - type: html
        label: "Announcement"
        query: SELECT '<p class="text-sm leading-relaxed">Welcome to the <strong class="font-semibold">Acme Shop</strong> admin panel. Use the sidebar to manage orders, customers, products and staff. The <strong class="font-semibold">Orders</strong> list supports bulk actions — select rows and press <em>Complete Selected</em>.</p>'
  - name: Reports
    path: /Reports
    widgets:
      - type: stat
        label: "Pending Orders"
        query: SELECT COUNT(*) FROM orders WHERE status = 'pending'
        icon: home
        color: warning
      - type: table
        label: "Low Stock Products"
        query: SELECT name, sku, category, stock FROM products WHERE stock < 30 ORDER BY stock LIMIT 10
        data_columns: [name, sku, category, stock]
      - type: table
        label: "Top Customers by Spend"
        query: SELECT c.name AS customer_name, CAST(ROUND(SUM(o.total)) AS INTEGER) AS total_spent FROM customers c LEFT JOIN orders o ON o.customer_id = c.id GROUP BY c.name ORDER BY total_spent DESC LIMIT 5
        data_columns: [customer_name, total_spent]

navigation:
  - group: "Overview"
    icon: home
    sort: 1
    items:
      - page: Dashboard
      - page: Reports
  - group: "Sales"
    icon: chart
    sort: 2
    items:
      - resource: Order
      - resource: OrderLine
      - resource: Customer
  - group: "Catalog"
    icon: cog
    sort: 3
    items:
      - resource: Product
  - group: "Administration"
    icon: users
    sort: 4
    items:
      - resource: User
      - resource: Role
`
}

// demoSchema returns the sqlite DDL for the demo domain: roles, users,
// customers, products, orders and orderlines (the order-lines table is named
// "orderlines" so it matches the generator's plural table-name convention for
// the OrderLine resource). Foreign keys express the relations between tables.
// Returns: the schema SQL as a string.
func demoSchema() string {
	return `CREATE TABLE roles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    password TEXT NOT NULL,
    role_id INTEGER NOT NULL DEFAULT 3 REFERENCES roles(id),
    role_name TEXT NOT NULL DEFAULT 'user',
    status TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE customers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    phone TEXT NOT NULL DEFAULT '',
    company TEXT NOT NULL DEFAULT '',
    city TEXT NOT NULL DEFAULT '',
    country TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE products (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    sku TEXT NOT NULL UNIQUE,
    category TEXT NOT NULL DEFAULT '',
    price REAL NOT NULL DEFAULT 0,
    stock INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE orders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    customer_id INTEGER NOT NULL REFERENCES customers(id),
    customer_name TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    total REAL NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE orderlines (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id INTEGER NOT NULL REFERENCES orders(id),
    product_id INTEGER NOT NULL REFERENCES products(id),
    product_name TEXT NOT NULL DEFAULT '',
    quantity INTEGER NOT NULL DEFAULT 1,
    unit_price REAL NOT NULL DEFAULT 0,
    line_total REAL NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
`
}

// demoQueries returns the SQLC-annotated queries for the demo domain, one file
// per resource. The list/count queries are what the generated list views
// reference; detail and populate queries drive the detail/edit views; the
// List*Options queries back the form select fields.
// Returns: a map of filename -> SQL content.
func demoQueries() map[string]string {
	return map[string]string{
		"roles.sql": `-- name: ListRoles :many
SELECT id, name, description, created_at FROM roles ORDER BY name;

-- name: CountRoles :one
SELECT COUNT(*) FROM roles;

-- name: GetRole :one
SELECT id, name, description, created_at FROM roles WHERE id = ?;

-- name: CreateRole :exec
INSERT INTO roles (name, description) VALUES (?, ?);

-- name: UpdateRole :exec
UPDATE roles SET name = ?, description = ? WHERE id = ?;

-- name: DeleteRole :exec
DELETE FROM roles WHERE id = ?;
`,
		"users.sql": `-- name: ListUsers :many
SELECT id, name, email, role_id, role_name, status, created_at FROM users ORDER BY created_at DESC;

-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: GetUser :one
SELECT id, name, email, role_id, role_name, status, created_at FROM users WHERE id = ?;

-- name: GetUserByEmail :one
SELECT id, password, COALESCE(role_name, '') AS role_name FROM users WHERE email = ?;

-- name: CreateUser :exec
INSERT INTO users (name, email, password, role_id, role_name, status) VALUES (?, ?, ?, ?, ?, ?);

-- name: UpdateUser :exec
UPDATE users SET name = ?, email = ?, role_id = ?, status = ? WHERE id = ?;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = ?;
`,
		"customers.sql": `-- name: ListCustomers :many
SELECT id, name, email, phone, company, city, country, status, created_at FROM customers ORDER BY created_at DESC;

-- name: CountCustomers :one
SELECT COUNT(*) FROM customers;

-- name: GetCustomer :one
SELECT id, name, email, phone, company, city, country, status, created_at FROM customers WHERE id = ?;

-- name: ListCustomerOptions :many
SELECT id, name FROM customers ORDER BY name;

-- name: CreateCustomer :exec
INSERT INTO customers (name, email, phone, company, city, country, status) VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: UpdateCustomer :exec
UPDATE customers SET name = ?, email = ?, phone = ?, company = ?, city = ?, country = ?, status = ? WHERE id = ?;

-- name: DeleteCustomer :exec
DELETE FROM customers WHERE id = ?;
`,
		"products.sql": `-- name: ListProducts :many
SELECT id, name, sku, category, price, stock, status, created_at FROM products ORDER BY created_at DESC;

-- name: CountProducts :one
SELECT COUNT(*) FROM products;

-- name: GetProduct :one
SELECT id, name, sku, category, price, stock, status, created_at FROM products WHERE id = ?;

-- name: ListProductOptions :many
SELECT id, name FROM products ORDER BY name;

-- name: CreateProduct :exec
INSERT INTO products (name, sku, category, price, stock, status) VALUES (?, ?, ?, ?, ?, ?);

-- name: UpdateProduct :exec
UPDATE products SET name = ?, sku = ?, category = ?, price = ?, stock = ?, status = ? WHERE id = ?;

-- name: DeleteProduct :exec
DELETE FROM products WHERE id = ?;
`,
		"orders.sql": `-- name: ListOrders :many
SELECT id, customer_id, customer_name, status, total, created_at FROM orders ORDER BY created_at DESC;

-- name: CountOrders :one
SELECT COUNT(*) FROM orders;

-- name: GetOrder :one
SELECT id, customer_id, customer_name, status, total, created_at FROM orders WHERE id = ?;

-- name: ListOrderOptions :many
SELECT id, customer_name FROM orders ORDER BY created_at DESC;

-- name: CreateOrder :exec
INSERT INTO orders (customer_id, customer_name, status, total) VALUES (?, ?, ?, ?);

-- name: UpdateOrder :exec
UPDATE orders SET customer_id = ?, customer_name = ?, status = ?, total = ? WHERE id = ?;

-- name: DeleteOrder :exec
DELETE FROM orders WHERE id = ?;
`,
		"orderlines.sql": `-- name: ListOrderLines :many
SELECT id, order_id, product_id, product_name, quantity, unit_price, line_total, created_at FROM orderlines ORDER BY created_at DESC;

-- name: CountOrderLines :one
SELECT COUNT(*) FROM orderlines;

-- name: GetOrderLine :one
SELECT id, order_id, product_id, product_name, quantity, unit_price, line_total, created_at FROM orderlines WHERE id = ?;

-- name: CreateOrderLine :exec
INSERT INTO orderlines (order_id, product_id, product_name, quantity, unit_price, line_total) VALUES (?, ?, ?, ?, ?, ?);

-- name: UpdateOrderLine :exec
UPDATE orderlines SET order_id = ?, product_id = ?, quantity = ?, unit_price = ? WHERE id = ?;

-- name: DeleteOrderLine :exec
DELETE FROM orderlines WHERE id = ?;
`,
	}
}

// demoSeededProduct is a catalog entry used to seed the products table.
type demoSeededProduct struct {
	name     string
	category string
	price    float64
}

// demoProducts is the deterministic demo catalog; its category values match the
// select options declared in demoYAML so the forms and the seeded rows agree.
var demoProducts = []demoSeededProduct{
	{"Wireless Mouse", "Electronics", 24.99},
	{"Mechanical Keyboard", "Electronics", 89.99},
	{"USB-C Hub", "Electronics", 39.99},
	{"Noise Cancelling Headphones", "Electronics", 199.99},
	{"4K Monitor 27\"", "Electronics", 329.99},
	{"Webcam 1080p", "Electronics", 59.99},
	{"Bluetooth Speaker", "Electronics", 49.99},
	{"Smart Watch", "Electronics", 149.99},
	{"External SSD 1TB", "Electronics", 109.99},
	{"Wireless Charger", "Electronics", 29.99},
	{"Standing Desk", "Home", 399.99},
	{"Ergonomic Office Chair", "Home", 249.99},
	{"LED Desk Lamp", "Home", 34.99},
	{"Espresso Machine", "Home", 179.99},
	{"Air Fryer", "Home", 89.99},
	{"Robot Vacuum", "Home", 299.99},
	{"Coffee Mug Set", "Home", 24.99},
	{"Cotton Bed Sheets", "Home", 59.99},
	{"Duvet Insert", "Home", 79.99},
	{"Throw Pillow", "Home", 19.99},
	{"Notebook A5", "Office", 4.99},
	{"Ballpoint Pens (12)", "Office", 8.99},
	{"Printer Paper (500)", "Office", 12.99},
	{"Stapler", "Office", 6.99},
	{"Desk Organizer", "Office", 18.99},
	{"Whiteboard", "Office", 42.99},
	{"Document Tray", "Office", 14.99},
	{"Correction Tape", "Office", 3.49},
	{"Paper Clips (1000)", "Office", 5.99},
	{"Hole Puncher", "Office", 9.99},
	{"Cotton T-Shirt", "Apparel", 19.99},
	{"Denim Jacket", "Apparel", 79.99},
	{"Running Shoes", "Apparel", 89.99},
	{"Wool Sweater", "Apparel", 64.99},
	{"Leather Belt", "Apparel", 34.99},
	{"Baseball Cap", "Apparel", 14.99},
	{"Winter Beanie", "Apparel", 12.99},
	{"Sunglasses", "Apparel", 29.99},
	{"Backpack", "Apparel", 54.99},
	{"Sneakers", "Apparel", 74.99},
	{"Yoga Mat", "Sports", 24.99},
	{"Dumbbell Set", "Sports", 99.99},
	{"Tennis Racket", "Sports", 69.99},
	{"Basketball", "Sports", 29.99},
	{"Running Belt", "Sports", 17.99},
	{"Resistance Bands", "Sports", 21.99},
	{"Skipping Rope", "Sports", 9.99},
	{"Water Bottle 1L", "Sports", 12.99},
	{"Fitness Tracker", "Sports", 59.99},
	{"Camping Tent", "Sports", 129.99},
}

// demoFirstNames and demoLastNames feed the generated customer records.
var demoFirstNames = []string{
	"John", "Jane", "Michael", "Sarah", "David", "Emma", "James", "Olivia",
	"Robert", "Sophia", "William", "Ava", "Thomas", "Isabella", "Charles", "Mia",
	"Daniel", "Charlotte", "Matthew", "Amelia", "Anthony", "Harper", "Andrew",
	"Evelyn", "Joshua", "Abigail", "Kevin", "Emily", "Brian", "Elizabeth",
	"Jason", "Sofia", "Ethan", "Grace", "Noah", "Chloe", "Ryan", "Lily",
	"Eric", "Nora",
}

var demoLastNames = []string{
	"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller",
	"Davis", "Rodriguez", "Martinez", "Hernandez", "Lopez", "Gonzalez",
	"Wilson", "Anderson", "Thomas", "Taylor", "Moore", "Jackson", "Martin",
	"Lee", "Perez", "Thompson", "White", "Harris", "Sanchez", "Clark",
	"Ramirez", "Lewis", "Robinson", "Walker", "Young", "Allen", "King",
	"Wright", "Scott", "Torres", "Nguyen", "Hill", "Flores",
}

var demoCompanies = []string{
	"Acme Corp", "Globex", "Initech", "Umbrella Corp", "Stark Industries",
	"Wayne Enterprises", "Hooli", "Pied Piper", "Aperture Science",
	"Massive Dynamic", "Vandelay Industries", "Bluth Company",
	"Dunder Mifflin", "Wonka Industries", "Gringotts", "Soylent Corp",
	"Tyrell Corp", "Cyberdyne", "Oscorp", "Daily Planet",
}

var demoCities = []string{
	"New York", "Los Angeles", "Chicago", "Houston", "Phoenix", "Philadelphia",
	"San Antonio", "San Diego", "Dallas", "San Jose", "Austin", "Seattle",
	"Denver", "Boston", "Atlanta", "Miami", "Portland", "Las Vegas",
	"Detroit", "Nashville",
}

var demoCountries = []string{
	"USA", "Canada", "Germany", "France", "UK", "Japan", "Australia",
	"Brazil", "India", "Netherlands",
}

// seedDemoDB creates the sqlite database at dbPath, applies demoSchema and
// fills it with deterministic demo data: roles, bcrypt-hashed users (including
// the admin@demo.test / admin account), customers, products, orders and
// order lines. Returns an error on the first failure.
// Params: dbPath (filesystem path of the sqlite database file).
// Returns: an error if the database cannot be created or seeded, or nil.
func seedDemoDB(dbPath string) error {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return err
	}
	if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return err
	}
	if _, err := db.Exec(demoSchema()); err != nil {
		return err
	}

	rng := rand.New(rand.NewSource(42))
	now := time.Now()
	stamp := func(daysBack int) string {
		return now.AddDate(0, 0, -daysBack).Format("2006-01-02 15:04:05")
	}

	roles := []struct{ name, desc string }{
		{"admin", "Full system access"},
		{"manager", "Manage sales and catalog"},
		{"staff", "Limited read access"},
	}
	for i, r := range roles {
		if _, err := db.Exec(
			"INSERT INTO roles (id, name, description, created_at) VALUES (?, ?, ?, ?)",
			i+1, r.name, r.desc, stamp(365)); err != nil {
			return err
		}
	}
	roleID := map[string]int{"admin": 1, "manager": 2, "staff": 3}

	users := []struct{ name, email, role, password string }{
		{"Admin User", "admin@demo.test", "admin", "admin"},
		{"Manager User", "manager@demo.test", "manager", "password"},
	}
	for i := 1; i <= 8; i++ {
		users = append(users, struct{ name, email, role, password string }{
			fmt.Sprintf("Staff User %d", i),
			fmt.Sprintf("staff%d@demo.test", i),
			"staff",
			"password",
		})
	}
	for _, u := range users {
		hash, err := bcrypt.GenerateFromPassword([]byte(u.password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		if _, err := db.Exec(
			"INSERT INTO users (name, email, password, role_id, role_name, status, created_at) VALUES (?, ?, ?, ?, ?, 'active', ?)",
			u.name, u.email, string(hash), roleID[u.role], u.role, stamp(30+rng.Intn(335))); err != nil {
			return err
		}
	}

	type customer struct {
		id   int64
		name string
	}
	var customers []customer
	usedEmails := map[string]bool{}
	for i := 0; i < 60; i++ {
		first := demoFirstNames[rng.Intn(len(demoFirstNames))]
		last := demoLastNames[rng.Intn(len(demoLastNames))]
		name := first + " " + last
		email := strings.ToLower(first + "." + last + "@example.com")
		if usedEmails[email] {
			email = strings.ToLower(first+"."+last) + fmt.Sprintf("%d", i) + "@example.com"
		}
		usedEmails[email] = true
		status := "active"
		if rng.Intn(5) == 0 {
			status = "inactive"
		}
		res, err := db.Exec(
			"INSERT INTO customers (name, email, phone, company, city, country, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			name, email,
			fmt.Sprintf("555-01%02d-%04d", rng.Intn(90), rng.Intn(10000)),
			demoCompanies[rng.Intn(len(demoCompanies))],
			demoCities[rng.Intn(len(demoCities))],
			demoCountries[rng.Intn(len(demoCountries))],
			status, stamp(30+rng.Intn(335)))
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		customers = append(customers, customer{id: id, name: name})
	}

	type product struct {
		id    int64
		name  string
		price float64
	}
	var products []product
	for i, p := range demoProducts {
		status := "active"
		if rng.Intn(10) == 0 {
			status = "inactive"
		}
		res, err := db.Exec(
			"INSERT INTO products (name, sku, category, price, stock, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
			p.name, fmt.Sprintf("SKU-%04d", i+1), p.category, p.price, rng.Intn(250), status, stamp(30+rng.Intn(335)))
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		products = append(products, product{id: id, name: p.name, price: p.price})
	}

	orderStatuses := []string{"pending", "processing", "completed", "completed", "completed", "completed", "cancelled"}
	for i := 0; i < 50; i++ {
		cu := customers[rng.Intn(len(customers))]
		status := orderStatuses[rng.Intn(len(orderStatuses))]
		orderStamp := stamp(rng.Intn(180))
		res, err := db.Exec(
			"INSERT INTO orders (customer_id, customer_name, status, total, created_at) VALUES (?, ?, ?, 0, ?)",
			cu.id, cu.name, status, orderStamp)
		if err != nil {
			return err
		}
		orderID, _ := res.LastInsertId()

		var total float64
		numLines := 1 + rng.Intn(3)
		for j := 0; j < numLines; j++ {
			pr := products[rng.Intn(len(products))]
			qty := 1 + rng.Intn(5)
			lineTotal := round2(pr.price * float64(qty))
			total += lineTotal
			if _, err := db.Exec(
				"INSERT INTO orderlines (order_id, product_id, product_name, quantity, unit_price, line_total, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
				orderID, pr.id, pr.name, qty, pr.price, lineTotal, orderStamp); err != nil {
				return err
			}
		}
		if _, err := db.Exec(
			"UPDATE orders SET total = ? WHERE id = ?", round2(total), orderID); err != nil {
			return err
		}
	}

	return nil
}

// round2 rounds a float to two decimal places, matching money arithmetic.
// Params: v (the value to round).
// Returns: the value rounded to 2 decimal places.
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
