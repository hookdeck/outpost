package config

import "os"

type OSInterface interface {
	Getenv(key string) string
	Stat(name string) (os.FileInfo, error)
	ReadFile(name string) ([]byte, error)
	Environ() []string
}

var defaultOS OSInterface = &defaultOSImpl{}

type defaultOSImpl struct{}

func (d *defaultOSImpl) Getenv(key string) string {
	return os.Getenv(key)
}

func (d *defaultOSImpl) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

func (d *defaultOSImpl) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

func (d *defaultOSImpl) Environ() []string {
	return os.Environ()
}
