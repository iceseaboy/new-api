package relay

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/stretchr/testify/assert"
)

// TaskPlatformUnavailableError feeds the client-facing rejection when no
// adaptor serves a platform. The three shapes are user-actionable and must
// stay distinguishable: system switched off, plugin disabled, unknown platform.
func TestTaskPlatformUnavailableError(t *testing.T) {
	t.Run("master switch off", func(t *testing.T) {
		pluginruntime.DefaultRegistry.SetEnabled(false)
		t.Cleanup(func() { pluginruntime.DefaultRegistry.SetEnabled(true) })
		code, message := TaskPlatformUnavailableError(constant.TaskPlatform("17"))
		assert.Equal(t, "task_plugin_system_disabled", code)
		assert.Contains(t, message, "task plugin system is disabled")
	})

	t.Run("factory plugin disabled resolves legacy channel type to plugin key", func(t *testing.T) {
		// 平台 17/50/45/54 由内置 Go 适配器承载，不再依赖工厂插件；
		// 用仍走插件的 vidu 验证同一错误形状。
		pluginruntime.DefaultRegistry.SetDisabledFactoryKeys([]string{"vidu"})
		t.Cleanup(func() { pluginruntime.DefaultRegistry.SetDisabledFactoryKeys(nil) })
		code, message := TaskPlatformUnavailableError(constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeVidu)))
		assert.Equal(t, "task_plugin_disabled", code)
		assert.Contains(t, message, `task plugin "vidu" is disabled`)
	})

	t.Run("unknown platform keeps the legacy message", func(t *testing.T) {
		code, message := TaskPlatformUnavailableError(constant.TaskPlatform("no-such-platform"))
		assert.Equal(t, "invalid_api_platform", code)
		assert.Contains(t, message, "invalid api platform: no-such-platform")
	})
}
