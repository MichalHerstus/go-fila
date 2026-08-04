package editor

import (
	"github.com/charmbracelet/huh"
	"github.com/go-fila/go-fila/internal/types"
)

func buildConnectionForm(cfg *types.Config) (*huh.Form, []func()) {
	if len(cfg.Connections) == 0 {
		cfg.Connections = map[string]types.Connection{"default": {Driver: "postgres"}}
	}

	oldName := ""
	for k := range cfg.Connections {
		oldName = k
		break
	}

	conn := cfg.Connections[oldName]
	if conn.Driver == "" {
		conn.Driver = "postgres"
	}
	name := oldName

	var applies []func()

	mo, moApply := intField("Pool Max Open", &conn.Pool.MaxOpen)
	applies = append(applies, moApply)
	mi, miApply := intField("Pool Max Idle", &conn.Pool.MaxIdle)
	applies = append(applies, miApply)
	applies = append(applies, func() {
		key := oldName
		if name != "" {
			key = name
		}
		if key != oldName {
			delete(cfg.Connections, oldName)
		}
		cfg.Connections[key] = conn
	})

	return huh.NewForm(
		huh.NewGroup(
			inputField("Connection Name", "default", &name),
			selectField("Driver", driverOptions, &conn.Driver),
			inputField("DSN", "postgres://user:pass@localhost:5432/db", &conn.DSN),
		).Title("Connection"),
		huh.NewGroup(
			mo,
			mi,
			inputField("Pool Lifetime", "5m", &conn.Pool.Lifetime),
		).Title("Connection > Pool"),
	), applies
}

func buildSQLCForm(cfg *types.Config) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			inputField("Config File", "sqlc.yaml", &cfg.SQLC.Config),
			inputField("Queries Dir", "./sql/queries", &cfg.SQLC.QueriesDir),
			inputField("Schema Dir", "./sql/migrations", &cfg.SQLC.SchemaDir),
			inputField("Output Package", "internal/data", &cfg.SQLC.OutputPkg),
		).Title("SQLC"),
	)
}
