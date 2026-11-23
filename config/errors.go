package config

import "fmt"

func errf(format string, args ...interface{}) error {
	return fmt.Errorf(format, args...)
}
