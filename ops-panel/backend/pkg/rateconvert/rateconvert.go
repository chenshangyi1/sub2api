// Package rateconvert 统一上游分组倍率换算逻辑。
// 网关调度与上游同步账号共用，避免两套算法漂移。
package rateconvert

import "strings"

// Convert 按 mode 换算分组倍率。
//
// mode:
//   - raw / "" : 原值
//   - multiply / divide : 按 customValue 乘除
//   - multiply_100 / divide_100 : 百倍换算兼容模式
//   - custom / fixed : 使用 customValue
func Convert(v float64, mode string, customValue float64) float64 {
	switch strings.TrimSpace(mode) {
	case "multiply":
		return v * conversionFactor(customValue)
	case "divide":
		return v / conversionFactor(customValue)
	case "multiply_100":
		return v * 100
	case "divide_100":
		return v / 100
	case "custom", "fixed":
		return customValue
	default:
		return v
	}
}

func conversionFactor(value float64) float64 {
	if value == 0 {
		return 1
	}
	return value
}

// NormalizeMode 归一化 mode，空串视为 raw。
func NormalizeMode(mode string) string {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return "raw"
	}
	return mode
}
