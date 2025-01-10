package config

import "os"

type OSInterface interface {
	Getenv(key string) string
	Stat(name string) (os.FileInfo, error)
	ReadFile(filename string) ([]byte, error)
}

var defaultOS = OSInterface(osAdapter{})

type osAdapter struct{}

func (osAdapter) Getenv(key string) string                 { return os.Getenv(key) }
func (osAdapter) Stat(name string) (os.FileInfo, error)    { return os.Stat(name) }
func (osAdapter) ReadFile(filename string) ([]byte, error) { return os.ReadFile(filename) }
