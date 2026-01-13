package main

type Config struct {
	Port   int
	LogDir string
}

func DefaultConfig() Config {
	return Config{
		Port:   8080,
		LogDir: "./logs",
	}
}
