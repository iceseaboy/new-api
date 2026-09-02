package plugins

import (
	"embed"
	"fmt"
	"io/fs"

	"github.com/QuantumNous/new-api/pkg/jsplugin"
)

//go:embed tasks/*/plugin.js
var taskPlugins embed.FS

func init() {
	entries, err := fs.ReadDir(taskPlugins, "tasks")
	if err != nil {
		panic(fmt.Sprintf("read embedded task plugins: %v", err))
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		key := entry.Name()
		// 这些平台由本仓库的内置 Go 任务适配器承载（秒级计费、中继信封、
		// 素材库等增强），跳过对应工厂插件以免其模型声明抢占路由。
		// 见 relay/relay_adaptor.go builtinTaskAdaptor。
		if key == "alibaba" || key == "doubao" || key == "jimeng" || key == "kling" {
			continue
		}
		source, sourceErr := Source(key)
		if sourceErr != nil {
			panic(fmt.Sprintf("read embedded task plugin %s: %v", key, sourceErr))
		}
		if _, registerErr := jsplugin.DefaultRegistry.RegisterFactory(source, jsplugin.Options{Key: key}); registerErr != nil {
			panic(fmt.Sprintf("register embedded task plugin %s: %v", key, registerErr))
		}
	}
}

// Source returns the embedded factory source for a task plugin key.
func Source(key string) (string, error) {
	source, err := taskPlugins.ReadFile("tasks/" + key + "/plugin.js")
	if err != nil {
		return "", err
	}
	return string(source), nil
}
