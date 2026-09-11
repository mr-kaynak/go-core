package config

import (
	"fmt"
	"strings"
)

// MigrationSettings is everything a migration run needs, and nothing else.
//
// A migration job connects to PostgreSQL and runs SQL. It does not sign
// tokens, send mail, or serve a port. Loading the full [Config] for it means
// a container that exists to run DDL cannot start without a 32-character JWT
// secret, an SMTP host, and an encryption key — so those secrets get handed
// to it anyway, and the job that most deserves a narrow database role ends up
// holding the application's entire credential set.
//
// The App section is here for one reason: [Config.IsProduction] governs
// whether an unencrypted connection is allowed, and that question does not
// stop applying because it is a migration opening the connection.
type MigrationSettings struct {
	App      AppConfig      `mapstructure:"app" validate:"required"`
	Database DatabaseConfig `mapstructure:"database" validate:"required"`
	Log      LogConfig      `mapstructure:"log"`
}

// LoadMigration reads the migration-relevant configuration from the same
// environment variables, defaults and config file that [Load] reads.
//
// It does not publish to the process-global slot. [Get] must never return a
// configuration whose absent sections were never populated: a caller reaching
// for cfg.JWT.Secret would find an empty string rather than a failure, and an
// empty secret is a working HMAC key.
func LoadMigration(configPath ...string) (*MigrationSettings, error) {
	v, err := newViper(configPath...)
	if err != nil {
		return nil, err
	}

	loaded := &MigrationSettings{}
	if err := v.Unmarshal(loaded); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := parseDatabaseDurations(v, &loaded.Database); err != nil {
		return nil, fmt.Errorf("failed to parse duration config: %w", err)
	}

	if err := validate.Struct(loaded); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	if err := checkDatabaseTransport(loaded.App.Env, loaded.Database); err != nil {
		return nil, err
	}

	return loaded, nil
}

// GetDSN renders the keyword/value connection string, identically to
// [Config.GetDSN] — the migration job and the application must address the
// same database the same way.
func (s *MigrationSettings) GetDSN() string {
	return databaseDSN(s.Database)
}

// IsProduction reports whether this is a production environment.
func (s *MigrationSettings) IsProduction() bool {
	return strings.EqualFold(s.App.Env, "production")
}

// IsStaging reports whether this is a staging environment.
func (s *MigrationSettings) IsStaging() bool {
	return strings.EqualFold(s.App.Env, "staging")
}
