package db

import (
	"fmt"
	"os"
)

// holderID uniquely identifies this process as a lock holder (hostname:pid).
// It is shared by both SQLiteDB and PostgresDB lock implementations.
var holderID = func() string {
	host, _ := os.Hostname()
	return fmt.Sprintf("%s:%d", host, os.Getpid())
}()
