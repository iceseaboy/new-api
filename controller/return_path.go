package controller

import (
	"strings"

	"github.com/QuantumNous/opclink/setting/system_setting"
)

func paymentReturnPath(suffix string) string {
	base := strings.TrimRight(system_setting.ServerAddress, "/")
	return base + suffix
}
