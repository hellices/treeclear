package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

type Duration struct {
	time.Duration
}

func (duration *Duration) UnmarshalText(text []byte) error {
	value := string(text)
	if strings.HasSuffix(value, "d") {
		const dayDuration = 24 * time.Hour
		days, err := strconv.ParseInt(strings.TrimSuffix(value, "d"), 10, 64)
		if err != nil || days <= 0 || days > math.MaxInt64/int64(dayDuration) {
			return fmt.Errorf("invalid day duration %q", value)
		}
		duration.Duration = time.Duration(days) * dayDuration
		return nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", value, err)
	}
	duration.Duration = parsed
	return nil
}
