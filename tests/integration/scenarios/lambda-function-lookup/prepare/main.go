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
	if err := prepare(filepath.Join(os.Getenv("SCENARIO_DIR"), "_output")); err != nil {
		log.Fatal(err)
	}
}

func prepare(directory string) error {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	return writeArchive(filepath.Join(directory, "function.zip"))
}

func writeArchive(path string) (err error) {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	archive := zip.NewWriter(file)
	defer func() { err = errors.Join(err, archive.Close()) }()
	entry, err := archive.Create("index.py")
	if err != nil {
		return err
	}
	_, err = io.WriteString(entry, "def handler(event, context):\n    return event\n")
	return err
}
