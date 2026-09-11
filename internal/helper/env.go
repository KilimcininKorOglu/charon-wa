package helper

import (
	"os"
	"strconv"
)

// GetEnvAsInt gets environment variable as integer with default fallback
func GetEnvAsInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultVal
}

// GetEnvAsPositiveInt gets environment variable as integer, falling back to
// defaultVal when the variable is unset, unparsable, or not greater than zero.
func GetEnvAsPositiveInt(key string, defaultVal int) int {
	if intVal := GetEnvAsInt(key, defaultVal); intVal > 0 {
		return intVal
	}
	return defaultVal
}

// GetEnvAsFloat gets environment variable as float64 with default fallback
func GetEnvAsFloat(key string, defaultVal float64) float64 {
	if val := os.Getenv(key); val != "" {
		if floatVal, err := strconv.ParseFloat(val, 64); err == nil {
			return floatVal
		}
	}
	return defaultVal
}

// GetEnvAsFloatInRange gets environment variable as float64, falling back to
// defaultVal when the variable is unset, unparsable, or outside [minVal, maxVal].
func GetEnvAsFloatInRange(key string, defaultVal, minVal, maxVal float64) float64 {
	floatVal := GetEnvAsFloat(key, defaultVal)
	if floatVal < minVal || floatVal > maxVal {
		return defaultVal
	}
	return floatVal
}
