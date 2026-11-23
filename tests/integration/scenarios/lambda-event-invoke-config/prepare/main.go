package main

import (
	"archive/zip"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
)

func main() {
	if err := run(filepath.Join(os.Getenv("SCENARIO_DIR"), "_output")); err != nil {
		log.Fatal(err)
	}
}

func run(dir string) (err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	file, err := os.Create(filepath.Join(dir, "function.zip"))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	archive := zip.NewWriter(file)
	defer func() { err = errors.Join(err, archive.Close()) }()
	handler, err := archive.Create("index.py")
	if err != nil {
		return err
	}
	_, err = io.WriteString(handler, "def handler(event, context):\n    return event\n")
	return err
}
