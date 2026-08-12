package ratio_setting

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/opclink/common"
	"github.com/QuantumNous/opclink/types"
)

// UserModelRatio grants a specific user a per-model price multiplier without
// moving the user out of their group or touching channel-group bindings.
// Outer key is the user ID (as a string, since JSON object keys are strings),
// inner key is a model name or a prefix pattern ending with '*'.
// Matching precedence: exact model name first, then the longest matching
// '*' prefix. Absent entries mean no adjustment (multiplier 1).
var userModelRatioMap = types.NewRWMap[string, map[string]float64]()

// userModelRatioMax bounds multipliers so a fat-fingered value cannot inflate
// a charge by more than one order of magnitude; discounts stay in (0, 1].
const userModelRatioMax = 10

func GetUserModelRatio(userId int, modelName string) (float64, bool) {
	if userId <= 0 || modelName == "" {
		return 1, false
	}
	rules, ok := userModelRatioMap.Get(strconv.Itoa(userId))
	if !ok || len(rules) == 0 {
		return 1, false
	}
	if ratio, ok := rules[modelName]; ok {
		return ratio, true
	}
	bestLen := -1
	best := 1.0
	for pattern, ratio := range rules {
		if !strings.HasSuffix(pattern, "*") {
			continue
		}
		prefix := strings.TrimSuffix(pattern, "*")
		if strings.HasPrefix(modelName, prefix) && len(prefix) > bestLen {
			bestLen = len(prefix)
			best = ratio
		}
	}
	if bestLen >= 0 {
		return best, true
	}
	return 1, false
}

func UserModelRatio2JSONString() string {
	return userModelRatioMap.MarshalJSONString()
}

func ValidateUserModelRatioJSON(jsonStr string) error {
	var parsed map[string]map[string]float64
	if err := common.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return err
	}
	for userKey, rules := range parsed {
		userId, err := strconv.Atoi(userKey)
		if err != nil || userId <= 0 {
			return fmt.Errorf("user model ratio key must be a positive user ID: %q", userKey)
		}
		for pattern, ratio := range rules {
			if pattern == "" || pattern == "*" {
				return fmt.Errorf("user %s has an empty model pattern", userKey)
			}
			if idx := strings.Index(pattern, "*"); idx >= 0 && idx != len(pattern)-1 {
				return fmt.Errorf("user %s pattern %q may only use '*' as a trailing wildcard", userKey, pattern)
			}
			if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio <= 0 || ratio > userModelRatioMax {
				return fmt.Errorf("user %s model %q ratio must be in (0, %d]: %v", userKey, pattern, userModelRatioMax, ratio)
			}
		}
	}
	return nil
}

func UpdateUserModelRatioByJSONString(jsonStr string) error {
	if err := ValidateUserModelRatioJSON(jsonStr); err != nil {
		return err
	}
	return types.LoadFromJsonString(userModelRatioMap, jsonStr)
}
