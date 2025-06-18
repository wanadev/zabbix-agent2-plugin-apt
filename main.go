package main

import (
	"errors"
	"os"

	"git.zabbix.com/ap/apt/plugin"
	"golang.zabbix.com/sdk/plugin/flag"
	"golang.zabbix.com/sdk/zbxerr"
)

const pluginVersion = 0

func main() {
	err := flag.HandleFlags(
		plugin.PluginName,
		os.Args[0],
		copyrightMessage(),
		"",
		1,
		2,
		pluginVersion,
	)
	if err != nil {
		if errors.Is(err, zbxerr.ErrorOSExitZero) {
			return
		}

		panic(err)
	}

	err = plugin.Launch()
	if err != nil {
		panic(err)
	}
}

func copyrightMessage() string {
	return ""
}
