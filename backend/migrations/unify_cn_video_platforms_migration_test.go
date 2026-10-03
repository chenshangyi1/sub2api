package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnifyCNVideoPlatformsMigration(t *testing.T) {
	content, err := FS.ReadFile("254_unify_cn_video_platforms.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "platform = 'cn'")
	require.Contains(t, sql, "WHERE platform IN ('kimi', 'zhipu', 'deepseek')")
	require.Contains(t, sql, "{cn_vendor}")
	require.NotContains(t, sql, "platform = 'openai'")
	require.Contains(t, sql, "'cn', 'video', 'kimi', 'zhipu', 'deepseek'")
	require.Contains(t, sql, `"platform":"cn"`)
	require.Contains(t, sql, `"platform":"video"`)
}
