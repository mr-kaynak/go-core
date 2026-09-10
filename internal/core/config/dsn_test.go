package config

import (
	"strings"
	"testing"
)

// An unquoted empty value does not terminate its keyword in a keyword/value
// connection string: "password= dbname=orders" parses as a password of
// "dbname=orders", the database name is lost, and the connection silently
// falls back to the default database — which is the user's name. Any
// deployment authenticating without a password (trust, peer, IAM) would then
// read and write the wrong database with no error anywhere.
func TestGetDSNKeepsTheDatabaseNameWhenThePasswordIsEmpty(t *testing.T) {
	cfg := &Config{}
	cfg.Database.Host = "db.internal"
	cfg.Database.Port = 5432
	cfg.Database.User = "app"
	cfg.Database.Password = ""
	cfg.Database.Name = "orders"
	cfg.Database.SSLMode = "require"

	dsn := cfg.GetDSN()

	if !strings.Contains(dsn, "dbname='orders'") {
		t.Fatalf("the database name must survive an empty password, got: %s", dsn)
	}
	if strings.Contains(dsn, "password= ") {
		t.Fatalf("an empty password must be quoted so it cannot swallow the next keyword, got: %s", dsn)
	}
}

func TestGetDSNQuotesValuesThatWouldOtherwiseSplit(t *testing.T) {
	cfg := &Config{}
	cfg.Database.Host = "db.internal"
	cfg.Database.Port = 5432
	cfg.Database.User = "app"
	cfg.Database.Password = `p ss'w\rd`
	cfg.Database.Name = "orders"
	cfg.Database.SSLMode = "disable"

	dsn := cfg.GetDSN()

	// A space would end the value, a quote would end the quoting, and a
	// backslash would escape whatever followed it.
	if !strings.Contains(dsn, `password='p ss\'w\\rd'`) {
		t.Fatalf("spaces, quotes and backslashes must all be escaped, got: %s", dsn)
	}
	if !strings.Contains(dsn, "dbname='orders'") {
		t.Fatalf("the database name must survive an awkward password, got: %s", dsn)
	}
}
