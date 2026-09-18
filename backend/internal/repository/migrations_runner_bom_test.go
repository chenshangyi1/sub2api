package repository

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestNormalizeMigrationSQL_StripsUTF8BOM(t *testing.T) {
	raw := append([]byte{0xEF, 0xBB, 0xBF}, []byte("CREATE TABLE t(id int);\n")...)
	got := normalizeMigrationSQL(raw)
	require.Equal(t, "CREATE TABLE t(id int);", got)
	require.False(t, strings.HasPrefix(got, "\uFEFF"))
}

func TestNormalizeMigrationSQL_PlainSQLUnchanged(t *testing.T) {
	got := normalizeMigrationSQL([]byte("  CREATE TABLE t(id int);  \n"))
	require.Equal(t, "CREATE TABLE t(id int);", got)
}

func TestMigration253HasNoUTF8BOM(t *testing.T) {
	raw, err := fs.ReadFile(migrations.FS, "253_user_cf_ip_allowlist.sql")
	require.NoError(t, err)
	require.False(t, bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}), "253 must not start with UTF-8 BOM")
	require.True(t, bytes.HasPrefix(bytes.TrimSpace(raw), []byte("CREATE")))
}
